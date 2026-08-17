package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	PublicCatalogBaseURL       = "https://www.skills.sh"
	DefaultLeaderboardLimit    = 30
	MaxLeaderboardLimit        = 30
	MaxLeaderboardBodyBytes    = 2 << 20
	MaxLeaderboardRSCBytes     = 1 << 20
	MaxUpstreamLeaderboardRows = 600
	DefaultLeaderboardTimeout  = 8 * time.Second
	maxLeaderboardTotalSkills  = int64(10_000_000)
)

var catalogHostLabelPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
var leaderboardSkillIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._:-]{0,126}[a-z0-9])?$`)

type LeaderboardReader struct {
	Client  *http.Client
	BaseURL string
	Timeout time.Duration
}

type leaderboardDefinition struct {
	view CatalogView
	path string
}

var leaderboardDefinitions = [...]leaderboardDefinition{
	{view: CatalogViewAllTime, path: "/"},
	{view: CatalogViewTrending, path: "/trending"},
	{view: CatalogViewHot, path: "/hot"},
}

type upstreamLeaderboard struct {
	InitialSkills []json.RawMessage `json:"initialSkills"`
	TotalSkills   int64             `json:"totalSkills"`
	AllTimeTotal  int64             `json:"allTimeTotal"`
	View          CatalogView       `json:"view"`
}

type upstreamAllTimeSkill struct {
	Source         string  `json:"source"`
	SkillID        string  `json:"skillId"`
	Name           string  `json:"name"`
	Installs       int64   `json:"installs"`
	WeeklyInstalls []int64 `json:"weeklyInstalls"`
	IsOfficial     *bool   `json:"isOfficial,omitempty"`
}

type upstreamTrendingSkill struct {
	Source     string `json:"source"`
	SkillID    string `json:"skillId"`
	Name       string `json:"name"`
	Installs   int64  `json:"installs"`
	IsOfficial *bool  `json:"isOfficial,omitempty"`
}

type upstreamHotSkill struct {
	Source            string `json:"source"`
	SkillID           string `json:"skillId"`
	Name              string `json:"name"`
	Installs          int64  `json:"installs"`
	InstallsYesterday int64  `json:"installsYesterday"`
	Change            int64  `json:"change"`
	IsOfficial        *bool  `json:"isOfficial,omitempty"`
}

func NewLeaderboardReader() *LeaderboardReader {
	return &LeaderboardReader{
		Client: &http.Client{
			Timeout:       DefaultLeaderboardTimeout,
			CheckRedirect: rejectCatalogRedirect,
		},
		BaseURL: PublicCatalogBaseURL,
		Timeout: DefaultLeaderboardTimeout,
	}
}

func (reader *LeaderboardReader) Read(ctx context.Context, limit int) (CatalogLeaderboards, error) {
	if limit <= 0 {
		limit = DefaultLeaderboardLimit
	}
	if limit > MaxLeaderboardLimit {
		return CatalogLeaderboards{}, fmt.Errorf("leaderboard limit exceeds %d", MaxLeaderboardLimit)
	}

	timeout := reader.Timeout
	if timeout <= 0 || timeout > DefaultLeaderboardTimeout {
		timeout = DefaultLeaderboardTimeout
	}
	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		index        int
		leaderboard  CatalogLeaderboard
		allTimeTotal int64
		err          error
	}
	results := make(chan result, len(leaderboardDefinitions))
	for index, definition := range leaderboardDefinitions {
		go func() {
			leaderboard, allTimeTotal, err := reader.readView(requestContext, definition, limit)
			results <- result{index: index, leaderboard: leaderboard, allTimeTotal: allTimeTotal, err: err}
		}()
	}

	ordered := make([]result, len(leaderboardDefinitions))
	for range leaderboardDefinitions {
		current := <-results
		ordered[current.index] = current
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return CatalogLeaderboards{}, context.Canceled
	}
	successes := 0
	var firstErr error
	var expectedTotal int64
	totalsDiffer := false
	for index := range ordered {
		if ordered[index].err != nil {
			if firstErr == nil {
				firstErr = ordered[index].err
			}
			ordered[index].leaderboard = CatalogLeaderboard{
				View:    leaderboardDefinitions[index].view,
				Skills:  []RankedCatalogSkill{},
				Warning: "This catalog ranking is temporarily unavailable.",
			}
			continue
		}
		successes++
		if expectedTotal == 0 {
			expectedTotal = ordered[index].allTimeTotal
		} else if ordered[index].allTimeTotal != expectedTotal {
			totalsDiffer = true
		}
	}
	if successes == 0 {
		if errors.Is(requestContext.Err(), context.DeadlineExceeded) {
			return CatalogLeaderboards{}, errors.New("skills.sh leaderboard request timed out")
		}
		return CatalogLeaderboards{}, firstErr
	}
	if totalsDiffer {
		for index := range ordered {
			if ordered[index].err == nil {
				ordered[index].leaderboard.Warning = "Catalog totals are inconsistent; rankings use the valid entries returned for this view."
			}
		}
	}

	return CatalogLeaderboards{
		AllTime:  ordered[0].leaderboard,
		Trending: ordered[1].leaderboard,
		Hot:      ordered[2].leaderboard,
	}, nil
}

func (reader *LeaderboardReader) readView(ctx context.Context, definition leaderboardDefinition, limit int) (CatalogLeaderboard, int64, error) {
	endpoint, err := reader.endpoint(definition.path)
	if err != nil {
		return CatalogLeaderboard{}, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return CatalogLeaderboard{}, 0, errors.New("could not build skills.sh leaderboard request")
	}
	request.Header.Set("Accept", "text/html")

	client := reader.Client
	if client == nil {
		client = NewLeaderboardReader().Client
	}
	boundedClient := *client
	boundedClient.CheckRedirect = rejectCatalogRedirect
	response, err := boundedClient.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
			return CatalogLeaderboard{}, 0, context.Canceled
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return CatalogLeaderboard{}, 0, errors.New("skills.sh leaderboard request timed out")
		}
		return CatalogLeaderboard{}, 0, errors.New("skills.sh leaderboard request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return CatalogLeaderboard{}, 0, fmt.Errorf("skills.sh %s leaderboard returned HTTP %d", definition.view, response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/html" {
		return CatalogLeaderboard{}, 0, fmt.Errorf("skills.sh %s leaderboard returned unexpected content", definition.view)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxLeaderboardBodyBytes+1))
	if err != nil {
		return CatalogLeaderboard{}, 0, fmt.Errorf("could not read skills.sh %s leaderboard", definition.view)
	}
	if len(body) > MaxLeaderboardBodyBytes {
		return CatalogLeaderboard{}, 0, fmt.Errorf("skills.sh %s leaderboard exceeded the body limit", definition.view)
	}
	leaderboard, allTimeTotal, err := parseLeaderboardDocument(body, definition.view, limit)
	if err != nil {
		return CatalogLeaderboard{}, 0, fmt.Errorf("skills.sh %s leaderboard unavailable: %w", definition.view, err)
	}
	return leaderboard, allTimeTotal, nil
}

func (reader *LeaderboardReader) endpoint(path string) (string, error) {
	base := reader.BaseURL
	if base == "" {
		base = PublicCatalogBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("invalid skills.sh leaderboard endpoint")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	return parsed.String(), nil
}

func parseLeaderboardDocument(body []byte, expectedView CatalogView, limit int) (CatalogLeaderboard, int64, error) {
	rsc, err := extractRSCStream(body)
	if err != nil {
		return CatalogLeaderboard{}, 0, err
	}
	marker := []byte(`{"initialSkills":`)
	if bytes.Count(rsc, marker) != 1 {
		return CatalogLeaderboard{}, 0, errors.New("structured initialSkills payload changed shape")
	}
	start := bytes.Index(rsc, marker)
	rawEnvelope, err := extractJSONObject(rsc[start:])
	if err != nil {
		return CatalogLeaderboard{}, 0, errors.New("structured initialSkills payload is malformed")
	}
	var upstream upstreamLeaderboard
	if err := json.Unmarshal(rawEnvelope, &upstream); err != nil {
		return CatalogLeaderboard{}, 0, errors.New("structured leaderboard payload is malformed")
	}
	if upstream.View != expectedView {
		return CatalogLeaderboard{}, 0, errors.New("structured leaderboard view does not match the requested page")
	}
	if len(upstream.InitialSkills) < 1 || len(upstream.InitialSkills) > MaxUpstreamLeaderboardRows {
		return CatalogLeaderboard{}, 0, errors.New("structured leaderboard row count is invalid")
	}
	if upstream.TotalSkills < 0 || upstream.TotalSkills > maxLeaderboardTotalSkills || upstream.AllTimeTotal <= 0 || upstream.AllTimeTotal > maxCatalogInstalls {
		return CatalogLeaderboard{}, 0, errors.New("structured leaderboard totals are invalid")
	}

	type rankedCandidate struct {
		skill  RankedCatalogSkill
		metric int64
	}
	byIdentity := make(map[string]rankedCandidate, len(upstream.InitialSkills))
	for _, raw := range upstream.InitialSkills {
		skill, metric, err := decodeRankedSkill(raw, expectedView, 0)
		if err != nil {
			continue
		}
		identity := skill.Source + "\x00" + skill.SkillID
		current, exists := byIdentity[identity]
		if !exists || metric > current.metric || (metric == current.metric && skill.Name < current.skill.Name) {
			byIdentity[identity] = rankedCandidate{skill: skill, metric: metric}
		}
	}
	validated := make([]rankedCandidate, 0, len(byIdentity))
	for _, candidate := range byIdentity {
		validated = append(validated, candidate)
	}
	sort.Slice(validated, func(left, right int) bool {
		if validated[left].metric != validated[right].metric {
			return validated[left].metric > validated[right].metric
		}
		if validated[left].skill.Source != validated[right].skill.Source {
			return validated[left].skill.Source < validated[right].skill.Source
		}
		return validated[left].skill.SkillID < validated[right].skill.SkillID
	})
	if len(validated) > limit {
		validated = validated[:limit]
	}
	skills := make([]RankedCatalogSkill, len(validated))
	for index := range validated {
		skills[index] = validated[index].skill
		skills[index].Rank = index + 1
	}
	totalSkills := upstream.TotalSkills
	if totalSkills < int64(len(skills)) {
		totalSkills = int64(len(skills))
	}
	return CatalogLeaderboard{
		View:        expectedView,
		TotalSkills: totalSkills,
		Skills:      skills,
	}, upstream.AllTimeTotal, nil
}

func decodeRankedSkill(raw json.RawMessage, view CatalogView, rank int) (RankedCatalogSkill, int64, error) {
	var source, skillID, name string
	var metric int64
	result := RankedCatalogSkill{Rank: rank}
	switch view {
	case CatalogViewAllTime:
		var candidate upstreamAllTimeSkill
		if err := json.Unmarshal(raw, &candidate); err != nil {
			return RankedCatalogSkill{}, 0, errors.New("all-time Skill payload is malformed")
		}
		if len(candidate.WeeklyInstalls) != 8 {
			return RankedCatalogSkill{}, 0, errors.New("all-time weekly metrics changed shape")
		}
		for _, value := range candidate.WeeklyInstalls {
			if !validLeaderboardMetric(value) {
				return RankedCatalogSkill{}, 0, errors.New("all-time weekly metric is invalid")
			}
		}
		source, skillID, name, metric = candidate.Source, candidate.SkillID, candidate.Name, candidate.Installs
		result.TotalInstalls = int64Pointer(candidate.Installs)
	case CatalogViewTrending:
		var candidate upstreamTrendingSkill
		if err := json.Unmarshal(raw, &candidate); err != nil {
			return RankedCatalogSkill{}, 0, errors.New("trending Skill payload is malformed")
		}
		source, skillID, name, metric = candidate.Source, candidate.SkillID, candidate.Name, candidate.Installs
		result.Installs24h = int64Pointer(candidate.Installs)
	case CatalogViewHot:
		var candidate upstreamHotSkill
		if err := json.Unmarshal(raw, &candidate); err != nil {
			return RankedCatalogSkill{}, 0, errors.New("hot Skill payload is malformed")
		}
		if !validLeaderboardMetric(candidate.InstallsYesterday) || candidate.Change < -maxCatalogInstalls || candidate.Change > maxCatalogInstalls || candidate.Change != candidate.Installs-candidate.InstallsYesterday {
			return RankedCatalogSkill{}, 0, errors.New("hot Skill metrics are invalid")
		}
		source, skillID, name, metric = candidate.Source, candidate.SkillID, candidate.Name, candidate.Installs
		result.CurrentInstalls = int64Pointer(candidate.Installs)
		result.YesterdayInstalls = int64Pointer(candidate.InstallsYesterday)
		result.Change = int64Pointer(candidate.Change)
	default:
		return RankedCatalogSkill{}, 0, errors.New("unsupported leaderboard view")
	}
	if !validLeaderboardMetric(metric) {
		return RankedCatalogSkill{}, 0, errors.New("leaderboard install metric is invalid")
	}
	if err := validateLeaderboardIdentity(source, skillID, name); err != nil {
		return RankedCatalogSkill{}, 0, err
	}
	result.ID = source + "/" + skillID
	result.SkillID = skillID
	result.Name = name
	result.Source = source
	result.Installable = ValidateCatalogIdentity(result.ID, source, skillID) == nil
	return result, metric, nil
}

func extractRSCStream(body []byte) ([]byte, error) {
	const (
		openScript  = "<script"
		closeScript = "</script>"
		pushPrefix  = "self.__next_f.push("
	)
	var stream bytes.Buffer
	found := false
	for offset := 0; offset < len(body); {
		opening := bytes.Index(body[offset:], []byte(openScript))
		if opening < 0 {
			break
		}
		opening += offset
		tagEndRelative := bytes.IndexByte(body[opening:], '>')
		if tagEndRelative < 0 || tagEndRelative > 1024 {
			return nil, errors.New("structured RSC script tag is malformed")
		}
		tagEnd := opening + tagEndRelative + 1
		closingRelative := bytes.Index(body[tagEnd:], []byte(closeScript))
		if closingRelative < 0 {
			return nil, errors.New("structured RSC script tag is unterminated")
		}
		closing := tagEnd + closingRelative
		content := bytes.TrimSpace(body[tagEnd:closing])
		if bytes.HasPrefix(content, []byte(pushPrefix)) {
			found = true
			if len(content) <= len(pushPrefix) || content[len(content)-1] != ')' {
				return nil, errors.New("structured RSC frame is malformed")
			}
			var frame []json.RawMessage
			if err := decodeExactJSON(content[len(pushPrefix):len(content)-1], &frame); err != nil || len(frame) < 1 {
				return nil, errors.New("structured RSC frame is malformed")
			}
			var kind int
			if err := json.Unmarshal(frame[0], &kind); err != nil {
				return nil, errors.New("structured RSC frame kind is malformed")
			}
			switch kind {
			case 0:
				if len(frame) != 1 {
					return nil, errors.New("structured RSC bootstrap frame changed shape")
				}
			case 1:
				if len(frame) != 2 {
					return nil, errors.New("structured RSC data frame changed shape")
				}
				var chunk string
				if err := json.Unmarshal(frame[1], &chunk); err != nil || !utf8.ValidString(chunk) {
					return nil, errors.New("structured RSC data frame is malformed")
				}
				if stream.Len()+len(chunk) > MaxLeaderboardRSCBytes {
					return nil, errors.New("structured RSC payload exceeded the size limit")
				}
				stream.WriteString(chunk)
			default:
				return nil, errors.New("structured RSC frame kind changed")
			}
		}
		offset = closing + len(closeScript)
	}
	if !found || stream.Len() == 0 {
		return nil, errors.New("structured RSC payload was not found")
	}
	return stream.Bytes(), nil
}

func extractJSONObject(value []byte) ([]byte, error) {
	if len(value) == 0 || value[0] != '{' {
		return nil, errors.New("JSON object did not start")
	}
	depth := 0
	inString := false
	escaped := false
	for index, current := range value {
		if inString {
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '"' {
				inString = false
			}
			continue
		}
		switch current {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return value[:index+1], nil
			}
			if depth < 0 {
				return nil, errors.New("JSON object ended early")
			}
		}
	}
	return nil, errors.New("JSON object was unterminated")
}

func validateObjectKeys(raw []byte, required, optional []string) error {
	var object map[string]json.RawMessage
	if err := decodeExactJSON(raw, &object); err != nil {
		return err
	}
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = struct{}{}
		if _, ok := object[key]; !ok {
			return fmt.Errorf("required field %q is missing", key)
		}
	}
	for _, key := range optional {
		allowed[key] = struct{}{}
	}
	for key := range object {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unexpected field %q", key)
		}
	}
	return nil
}

func decodeExactJSON(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON value has trailing data")
	}
	return nil
}

func validateLeaderboardIdentity(source, skillID, name string) error {
	if err := validateLeaderboardSource(source); err != nil {
		return errors.New("structured leaderboard source is invalid")
	}
	if len(skillID) > maxSkillNameLength || !leaderboardSkillIDPattern.MatchString(skillID) || skillID == "." || skillID == ".." {
		return errors.New("structured leaderboard Skill ID is invalid")
	}
	if name == "" || len(name) > maxSkillNameLength || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return errors.New("structured leaderboard Skill name is invalid")
	}
	for _, current := range name {
		if unicode.IsControl(current) {
			return errors.New("structured leaderboard Skill name is invalid")
		}
	}
	return nil
}

func validateLeaderboardSource(value string) error {
	if ValidateRepository(value) == nil {
		return nil
	}
	if value == "" || len(value) > maxSourceLength || !utf8.ValidString(value) || strings.ToLower(value) != value || strings.Contains(value, "/") {
		return errors.New("invalid leaderboard source")
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return errors.New("leaderboard source must be a repository or hostname")
	}
	for _, label := range labels {
		if !catalogHostLabelPattern.MatchString(label) {
			return errors.New("invalid leaderboard hostname")
		}
	}
	return nil
}

func validLeaderboardMetric(value int64) bool {
	return value >= 0 && value <= maxCatalogInstalls
}

func int64Pointer(value int64) *int64 {
	return &value
}
