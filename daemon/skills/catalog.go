package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	PublicSearchEndpoint = "https://skills.sh/api/search"
	DefaultCatalogLimit  = 20
	MaxCatalogLimit      = 30
	MaxCatalogBodyBytes  = 512 << 10
	DefaultSearchTimeout = 7 * time.Second
	maxCatalogQueryRunes = 80
	maxCatalogInstalls   = int64(1_000_000_000_000)
)

type Searcher struct {
	Client   *http.Client
	Endpoint string
	Timeout  time.Duration
}

type catalogResponse struct {
	Skills []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Installs int64  `json:"installs"`
		Source   string `json:"source"`
	} `json:"skills"`
}

func NewSearcher() *Searcher {
	return &Searcher{
		Client: &http.Client{
			Timeout:       DefaultSearchTimeout,
			CheckRedirect: rejectCatalogRedirect,
		},
		Endpoint: PublicSearchEndpoint,
		Timeout:  DefaultSearchTimeout,
	}
}

func (searcher *Searcher) Search(ctx context.Context, query string, limit int) (CatalogResult, error) {
	query, err := validateCatalogQuery(query)
	if err != nil {
		return CatalogResult{}, err
	}
	if limit <= 0 {
		limit = DefaultCatalogLimit
	}
	if limit > MaxCatalogLimit {
		return CatalogResult{}, fmt.Errorf("catalog limit exceeds %d", MaxCatalogLimit)
	}
	endpoint := searcher.Endpoint
	if endpoint == "" {
		endpoint = PublicSearchEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return CatalogResult{}, errors.New("invalid catalog endpoint")
	}
	values := parsed.Query()
	values.Set("q", query)
	values.Set("limit", fmt.Sprintf("%d", limit))
	parsed.RawQuery = values.Encode()

	timeout := searcher.Timeout
	if timeout <= 0 || timeout > DefaultSearchTimeout {
		timeout = DefaultSearchTimeout
	}
	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return CatalogResult{}, errors.New("could not build catalog request")
	}
	request.Header.Set("Accept", "application/json")
	client := searcher.Client
	if client == nil {
		client = NewSearcher().Client
	}
	boundedClient := *client
	boundedClient.CheckRedirect = rejectCatalogRedirect
	response, err := boundedClient.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestContext.Err(), context.DeadlineExceeded) {
			return CatalogResult{}, errors.New("skills.sh search timed out")
		}
		if errors.Is(err, context.Canceled) || errors.Is(requestContext.Err(), context.Canceled) {
			return CatalogResult{}, context.Canceled
		}
		return CatalogResult{}, errors.New("skills.sh search failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return CatalogResult{}, fmt.Errorf("skills.sh search returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxCatalogBodyBytes+1))
	if err != nil {
		return CatalogResult{}, errors.New("could not read skills.sh response")
	}
	if len(body) > MaxCatalogBodyBytes {
		return CatalogResult{}, errors.New("skills.sh response exceeded the body limit")
	}
	var decoded catalogResponse
	if json.Unmarshal(body, &decoded) != nil {
		return CatalogResult{}, errors.New("skills.sh returned malformed JSON")
	}
	if len(decoded.Skills) > MaxCatalogLimit*4 {
		return CatalogResult{}, errors.New("skills.sh returned too many results")
	}

	result := CatalogResult{Query: query, Skills: make([]CatalogSkill, 0, min(limit, len(decoded.Skills)))}
	seen := make(map[string]CatalogSkill, len(decoded.Skills))
	invalid := 0
	for _, candidate := range decoded.Skills {
		if ValidateCatalogIdentity(candidate.ID, candidate.Source, candidate.Name) != nil || candidate.Installs < 0 || candidate.Installs > maxCatalogInstalls {
			invalid++
			continue
		}
		skill := CatalogSkill{
			ID:       candidate.ID,
			Name:     candidate.Name,
			Installs: candidate.Installs,
			Source:   candidate.Source,
		}
		if previous, ok := seen[skill.ID]; ok {
			if previous != skill {
				return CatalogResult{}, errors.New("skills.sh returned an ambiguous duplicate identity")
			}
			continue
		}
		seen[skill.ID] = skill
		if len(result.Skills) < limit {
			result.Skills = append(result.Skills, skill)
		}
	}
	if len(decoded.Skills) > 0 && len(result.Skills) == 0 && invalid > 0 {
		return CatalogResult{}, errors.New("skills.sh returned no valid Skill identities")
	}
	return result, nil
}

func rejectCatalogRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func validateCatalogQuery(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		return "", errors.New("invalid catalog query")
	}
	runes := []rune(value)
	if len(runes) < 2 {
		return "", errors.New("catalog query must contain at least two characters")
	}
	if len(runes) > maxCatalogQueryRunes {
		return "", errors.New("catalog query is too long")
	}
	for _, r := range runes {
		if unicode.IsControl(r) {
			return "", errors.New("catalog query contains control characters")
		}
	}
	return value, nil
}
