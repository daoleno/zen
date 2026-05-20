package work

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const (
	autoBlockStart    = "<!-- zen:auto:start -->"
	autoBlockEnd      = "<!-- zen:auto:end -->"
	sessionBlockStart = "<!-- zen:session:start "
	sessionBlockEnd   = "<!-- zen:session:end "
	brainLogKind      = "brain_log"
	// Keep this stable during product iteration; change only for incompatible readout semantics.
	digestPromptVersion = "agent-readout"
)

var autoTitleSuffixRe = regexp.MustCompile(`\s+\([^)]+\)\s*$`)

// SessionLogger turns watcher agent snapshots into durable daily work log evidence.
type SessionLogger struct {
	store          *Store
	digester       AgentDigestProvider
	now            func() time.Time
	minInterval    time.Duration
	digestEvery    time.Duration
	failureBackoff time.Duration

	mu             sync.Mutex
	lastWrite      map[string]time.Time
	lastHash       map[string]string
	lastDigest     map[string]time.Time
	lastDigestHash map[string]string
	lastDigestErr  map[string]digestFailure
	pendingDigest  map[string]bool
	digestSem      chan struct{}
	syncDigest     bool
	loadTranscript func(classifier.Agent, time.Time) ToolTranscript
}

type digestFailure struct {
	hash string
	at   time.Time
}

func NewSessionLogger(store *Store, digester AgentDigestProvider) *SessionLogger {
	return &SessionLogger{
		store:          store,
		digester:       digester,
		now:            time.Now,
		minInterval:    2 * time.Second,
		digestEvery:    time.Hour,
		failureBackoff: time.Hour,
		lastWrite:      map[string]time.Time{},
		lastHash:       map[string]string{},
		lastDigest:     map[string]time.Time{},
		lastDigestHash: map[string]string{},
		lastDigestErr:  map[string]digestFailure{},
		pendingDigest:  map[string]bool{},
		digestSem:      make(chan struct{}, 1),
		loadTranscript: loadToolTranscript,
	}
}

type configurableDigestProvider interface {
	PreferredProvider() string
	SetPreferredProvider(string) (string, bool)
}

func (l *SessionLogger) DigestProvider() string {
	if l == nil || l.digester == nil {
		return ""
	}
	configurable, ok := l.digester.(configurableDigestProvider)
	if !ok {
		return ""
	}
	return configurable.PreferredProvider()
}

func (l *SessionLogger) SetDigestProvider(provider string) (string, bool) {
	if l == nil || l.digester == nil {
		return "", false
	}
	configurable, ok := l.digester.(configurableDigestProvider)
	if !ok {
		return "", false
	}
	return configurable.SetPreferredProvider(provider)
}

// RecordAgent upserts the project evidence used by the daily work log. Non-forced updates are
// throttled so active output does not rewrite the Markdown file on every poll.
func (l *SessionLogger) RecordAgent(agent *classifier.Agent, final, force bool) (*Item, error) {
	if l == nil || l.store == nil || agent == nil || strings.TrimSpace(agent.ID) == "" {
		return nil, nil
	}
	if !IsAgentSession(agent) {
		return nil, nil
	}

	now := l.now()
	status := sessionStatus(agent, final)
	hash := sessionSnapshotHash(agent, status)
	if !force && l.shouldSkip(agent.ID, now) {
		return nil, nil
	}

	l.mu.Lock()
	l.lastWrite[agent.ID] = now
	l.lastHash[agent.ID] = hash
	l.mu.Unlock()

	project := safeProjectName(agent.Project, agent.Cwd)
	l.scheduleDigest(agent, status, hash, final, force, now)
	if l.syncDigest {
		if written, ok := l.store.GetByID(brainLogID(project)); ok {
			return written, nil
		}
	}
	if existing, ok := l.store.GetByID(brainLogID(project)); ok {
		return existing, nil
	}
	return nil, nil
}

func (l *SessionLogger) shouldSkip(sessionID string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if last, ok := l.lastWrite[sessionID]; ok && now.Sub(last) < l.minInterval {
		return true
	}
	return false
}

func (l *SessionLogger) scheduleDigest(agent *classifier.Agent, status, hash string, final, force bool, now time.Time) {
	if l == nil || l.digester == nil || agent == nil {
		return
	}

	sessionID := strings.TrimSpace(agent.ID)
	if sessionID == "" {
		return
	}

	l.mu.Lock()
	if l.pendingDigest[sessionID] {
		l.mu.Unlock()
		return
	}
	if !force && !final && !isDigestUrgentStatus(status) {
		if last := l.lastDigest[sessionID]; !last.IsZero() && now.Sub(last) < l.digestEvery {
			l.mu.Unlock()
			return
		}
	}
	l.pendingDigest[sessionID] = true
	l.mu.Unlock()

	agentCopy := cloneDigestAgent(agent)
	if l.syncDigest {
		l.digestAndWrite(agentCopy, status, hash)
		return
	}
	go l.digestAndWrite(agentCopy, status, hash)
}

func (l *SessionLogger) digestAndWrite(agent *classifier.Agent, status, _ string) {
	sessionID := strings.TrimSpace(agent.ID)
	defer func() {
		l.mu.Lock()
		delete(l.pendingDigest, sessionID)
		l.mu.Unlock()
	}()

	if l.digestSem != nil {
		l.digestSem <- struct{}{}
		defer func() { <-l.digestSem }()
	}

	project := safeProjectName(agent.Project, agent.Cwd)
	existing, _ := l.store.GetByID(brainLogID(project))
	now := l.now()
	transcript := l.nativeTranscript(*agent, now)
	if strings.TrimSpace(transcript.Excerpt) == "" || !IsNativeAgentSource(transcript.Source) {
		l.mu.Lock()
		l.lastDigest[sessionID] = now
		l.mu.Unlock()
		return
	}
	digestHash := transcriptEvidenceHash(*agent, transcript)

	l.mu.Lock()
	if l.lastDigestHash[sessionID] == digestHash {
		l.lastDigest[sessionID] = now
		l.mu.Unlock()
		l.writeExistingDigestSnapshot(agent, existing, now, status, digestHash, transcript)
		return
	}
	if lastErr, ok := l.lastDigestErr[sessionID]; ok && lastErr.hash == digestHash && now.Sub(lastErr.at) < l.failureBackoff {
		l.lastDigest[sessionID] = now
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()
	if existing != nil && existing.Frontmatter.AIHash == digestHash {
		l.mu.Lock()
		l.lastDigest[sessionID] = now
		l.lastDigestHash[sessionID] = digestHash
		delete(l.lastDigestErr, sessionID)
		l.mu.Unlock()
		l.writeExistingDigestSnapshot(agent, existing, now, status, digestHash, transcript)
		return
	}

	input := AgentDigestInput{
		Agent:           *agent,
		Status:          status,
		Transcript:      transcript,
		PreviousTitle:   "",
		PreviousSummary: "",
		PreviousNext:    "",
		Now:             now,
	}
	if existing != nil {
		input.PreviousTitle = existing.Frontmatter.Title
		input.PreviousSummary = existing.Frontmatter.Summary
		input.PreviousNext = existing.Frontmatter.Next
	}

	digest, err := l.digester.Digest(context.Background(), input)
	finishedAt := l.now()

	l.mu.Lock()
	l.lastDigest[sessionID] = finishedAt
	if err == nil {
		l.lastDigestHash[sessionID] = digestHash
		delete(l.lastDigestErr, sessionID)
	} else {
		l.lastDigestErr[sessionID] = digestFailure{hash: digestHash, at: finishedAt}
	}
	l.mu.Unlock()

	if err != nil {
		log.Printf("brain digest failed for %s (%s): %v", agent.ID, project, err)
		return
	}

	latest, _ := l.store.GetByID(brainLogID(project))
	item := buildSessionItem(l.store.Root, latest, agent, finishedAt, status, digest, digestHash, transcript)
	if _, err := l.store.Write(item, time.Time{}); err != nil {
		l.mu.Lock()
		delete(l.lastDigestHash, sessionID)
		l.mu.Unlock()
		log.Printf("brain digest write failed for %s (%s): %v", agent.ID, project, err)
	}
}

func (l *SessionLogger) writeExistingDigestSnapshot(agent *classifier.Agent, existing *Item, now time.Time, status, digestHash string, transcript ToolTranscript) {
	if l == nil || l.store == nil || agent == nil || existing == nil {
		return
	}
	current := digestFromExisting(existing)
	item := buildSessionItem(l.store.Root, existing, agent, now, status, current, digestHash, transcript)
	if _, err := l.store.Write(item, time.Time{}); err != nil {
		log.Printf("brain digest write failed for %s (%s): %v", agent.ID, safeProjectName(agent.Project, agent.Cwd), err)
	}
}

func (l *SessionLogger) nativeTranscript(agent classifier.Agent, now time.Time) ToolTranscript {
	if l != nil && l.loadTranscript != nil {
		return l.loadTranscript(agent, now)
	}
	return loadToolTranscript(agent, now)
}

func cloneDigestAgent(agent *classifier.Agent) *classifier.Agent {
	if agent == nil {
		return nil
	}
	cp := *agent
	if agent.LastLines != nil {
		cp.LastLines = append([]string(nil), agent.LastLines...)
	}
	return &cp
}

func isDigestUrgentStatus(status string) bool {
	return status == string(classifier.StateBlocked) ||
		status == string(classifier.StateDone) ||
		status == string(classifier.StateFailed)
}

func buildSessionItem(root string, existing *Item, agent *classifier.Agent, now time.Time, status string, digest AgentDigest, digestHash string, transcript ToolTranscript) *Item {
	project := safeProjectName(agent.Project, agent.Cwd)
	id := brainLogID(project)
	title := project
	path := filepath.Join(root, project, "brain.md")
	frontmatter := Frontmatter{
		ID:          id,
		Kind:        brainLogKind,
		Created:     now,
		Started:     &now,
		Status:      status,
		Title:       title,
		Outcome:     digest.Outcome,
		Summary:     digest.Summary,
		Progress:    append([]string(nil), digest.Progress...),
		Friction:    digest.Friction,
		Cause:       digest.Cause,
		Insight:     digest.Insight,
		Next:        digest.Next,
		AgentSource: strings.TrimSpace(transcript.Source),
		Cwd:         strings.TrimSpace(agent.Cwd),
		Command:     strings.TrimSpace(agent.Command),
		AIProvider:  digest.Provider,
		AIHash:      digestHash,
	}
	if hasDigestContent(digest) {
		updated := now
		frontmatter.AIUpdated = &updated
	}

	existingBody := ""
	if existing != nil {
		existingBody = existing.Body
		path = existing.Path
		project = existing.Project
		frontmatter = existing.Frontmatter
		frontmatter.ID = strings.TrimSpace(frontmatter.ID)
		if frontmatter.ID == "" {
			frontmatter.ID = id
		}
		frontmatter.Kind = brainLogKind
		if frontmatter.Created.IsZero() {
			frontmatter.Created = now
		}
		if frontmatter.Started == nil {
			started := frontmatter.Created
			frontmatter.Started = &started
		}
		frontmatter.Cwd = strings.TrimSpace(agent.Cwd)
		frontmatter.Command = strings.TrimSpace(agent.Command)
		frontmatter.Status = status
		frontmatter.Title = title
		frontmatter.AgentSource = strings.TrimSpace(transcript.Source)
		if hasDigestContent(digest) {
			frontmatter.Outcome = digest.Outcome
			frontmatter.Summary = digest.Summary
			frontmatter.Progress = append([]string(nil), digest.Progress...)
			frontmatter.Friction = digest.Friction
			frontmatter.Cause = digest.Cause
			frontmatter.Insight = digest.Insight
			frontmatter.Next = digest.Next
			frontmatter.AIProvider = digest.Provider
			frontmatter.AIHash = digestHash
			frontmatter.AIError = ""
			updated := now
			frontmatter.AIUpdated = &updated
		}
	}

	frontmatter.Done = nil
	entry := renderProjectSessionEntry(agent, frontmatter, digest, now)

	return &Item{
		ID:          frontmatter.ID,
		Path:        path,
		Project:     project,
		Body:        mergeProjectLog(existingBody, title, entry),
		Frontmatter: frontmatter,
	}
}

func brainLogID(project string) string {
	project = safeProjectName(project, "")
	sum := sha1.Sum([]byte(project))
	return "brain-" + hex.EncodeToString(sum[:])[:16]
}

type projectSessionEntry struct {
	Key     string
	Date    string
	Updated time.Time
	Score   int
	Content string
}

func renderProjectSessionEntry(agent *classifier.Agent, frontmatter Frontmatter, digest AgentDigest, now time.Time) projectSessionEntry {
	key := autoSessionID(agent.ID)
	date := now.Format("2006-01-02")
	score := sessionEntryScore(frontmatter, digest)
	updated := now.UTC()
	title := digestTitle(digest, agent)

	lines := []string{
		fmt.Sprintf("%s%s date=%s score=%d updated=%s -->", sessionBlockStart, key, date, score, updated.Format(time.RFC3339)),
		"### " + title,
		"",
	}

	if outcome := strings.TrimSpace(frontmatter.Outcome); outcome != "" {
		lines = append(lines, "#### Outcome", "", outcome)
	}

	if summary := strings.TrimSpace(frontmatter.Summary); summary != "" {
		if len(lines) > 3 {
			lines = append(lines, "")
		}
		lines = append(lines, "#### Read", "", summary)
	} else if frontmatter.Outcome == "" && frontmatter.AIError != "" {
		lines = append(lines, "AI digest unavailable. The daemon will retry when the evidence changes.")
	}

	if len(frontmatter.Progress) > 0 {
		lines = append(lines, "", "#### Signals", "")
		for _, item := range frontmatter.Progress {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				lines = append(lines, "- "+trimmed)
			}
		}
	}

	if friction := strings.TrimSpace(frontmatter.Friction); friction != "" {
		lines = append(lines, "", "#### Friction", "", friction)
	}

	if cause := strings.TrimSpace(frontmatter.Cause); cause != "" {
		lines = append(lines, "", "#### Cause", "", cause)
	}

	if insight := strings.TrimSpace(frontmatter.Insight); insight != "" {
		lines = append(lines, "", "#### Insight", "", insight)
	}

	if next := strings.TrimSpace(frontmatter.Next); next != "" {
		lines = append(lines, "", "#### Next", "", next)
	}

	if len(lines) == 3 {
		if summary := strings.TrimSpace(frontmatter.Summary); summary != "" {
			lines = append(lines, summary)
		}
	}

	meta := []string{
		fmt.Sprintf("session=%s", strings.TrimSpace(agent.ID)),
		fmt.Sprintf("status=%s", frontmatter.Status),
		fmt.Sprintf("agent=%s", collapseSpaces(autoTitleSuffixRe.ReplaceAllString(strings.TrimSpace(agent.Name), ""))),
		fmt.Sprintf("updated=%s", updated.Format(time.RFC3339)),
	}
	if provider := strings.TrimSpace(frontmatter.AIProvider); provider != "" {
		meta = append(meta, fmt.Sprintf("read_by=%s", provider))
	}
	if frontmatter.AIError != "" {
		meta = append(meta, fmt.Sprintf("ai_error=%s", frontmatter.AIError))
	}
	lines = append(lines,
		"",
		"<!-- zen:meta "+strings.Join(meta, "; ")+" -->",
		fmt.Sprintf("%s%s -->", sessionBlockEnd, key),
	)

	return projectSessionEntry{
		Key:     key,
		Date:    date,
		Updated: updated,
		Score:   score,
		Content: strings.Join(lines, "\n"),
	}
}

func sessionEntryScore(frontmatter Frontmatter, digest AgentDigest) int {
	score := 0
	if hasDigestContent(digest) {
		score += 10
	}
	if strings.TrimSpace(frontmatter.Outcome) != "" {
		score += 2
	}
	if strings.TrimSpace(frontmatter.Summary) != "" {
		score += 3
	}
	if len(frontmatter.Progress) > 0 {
		score += 2
	}
	if strings.TrimSpace(frontmatter.Insight) != "" {
		score += 3
	}
	if strings.TrimSpace(frontmatter.Friction) != "" {
		score++
	}
	if strings.TrimSpace(frontmatter.Next) != "" {
		score += 2
	}
	if frontmatter.Status == string(classifier.StateBlocked) || frontmatter.Status == string(classifier.StateFailed) {
		score++
	}
	return score
}

func mergeProjectLog(existingBody, projectTitle string, entry projectSessionEntry) string {
	projectTitle = strings.TrimSpace(projectTitle)
	if projectTitle == "" {
		projectTitle = "Brain"
	}

	before, auto, after, ok := splitAutoBlock(existingBody)
	entries := parseProjectSessionEntries(auto)
	replaced := false
	for i := range entries {
		if entries[i].Key == entry.Key {
			entries[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, entry)
	}

	autoBlock := renderProjectAutoBlock(entries)
	if strings.TrimSpace(existingBody) == "" || !ok {
		if strings.TrimSpace(existingBody) == "" {
			return "# " + projectTitle + "\n\n" + autoBlock + "\n"
		}
		lines := strings.Split(existingBody, "\n")
		if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "# ") {
			before = strings.TrimRight(lines[0], "\n") + "\n\n"
			after = strings.TrimLeft(strings.Join(lines[1:], "\n"), "\n")
		} else {
			before = "# " + projectTitle + "\n\n"
			after = strings.TrimLeft(existingBody, "\n")
		}
	}

	out := strings.TrimRight(before, "\n") + "\n\n" + autoBlock
	if strings.TrimSpace(after) != "" {
		out += "\n\n" + strings.TrimLeft(after, "\n")
	}
	return strings.TrimRight(out, "\n") + "\n"
}

func splitAutoBlock(body string) (before, auto, after string, ok bool) {
	start := strings.Index(body, autoBlockStart)
	end := strings.Index(body, autoBlockEnd)
	if start < 0 || end < start {
		return "", "", "", false
	}
	end += len(autoBlockEnd)
	return body[:start], body[start:end], body[end:], true
}

func renderProjectAutoBlock(entries []projectSessionEntry) string {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Date != entries[j].Date {
			return entries[i].Date > entries[j].Date
		}
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		return entries[i].Updated.After(entries[j].Updated)
	})

	lines := []string{autoBlockStart}
	currentDate := ""
	for _, entry := range entries {
		date := strings.TrimSpace(entry.Date)
		if date == "" {
			date = "Undated"
		}
		if date != currentDate {
			if currentDate != "" {
				lines = append(lines, "")
			}
			lines = append(lines, "## "+date, "")
			currentDate = date
		}
		lines = append(lines, strings.TrimSpace(entry.Content), "")
	}
	lines = append(lines, autoBlockEnd)
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func parseProjectSessionEntries(autoBlock string) []projectSessionEntry {
	var entries []projectSessionEntry
	rest := autoBlock
	for {
		start := strings.Index(rest, sessionBlockStart)
		if start < 0 {
			break
		}
		afterStart := rest[start+len(sessionBlockStart):]
		startClose := strings.Index(afterStart, "-->")
		if startClose < 0 {
			break
		}
		header := strings.TrimSpace(afterStart[:startClose])
		key, meta := splitSessionHeader(header)
		if key == "" {
			rest = afterStart[startClose+len("-->"):]
			continue
		}
		endMarker := sessionBlockEnd + key + " -->"
		afterHeader := afterStart[startClose+len("-->"):]
		end := strings.Index(afterHeader, endMarker)
		if end < 0 {
			rest = afterHeader
			continue
		}
		content := sessionBlockStart + strings.TrimSpace(header) + " -->" + afterHeader[:end] + endMarker
		entries = append(entries, projectSessionEntry{
			Key:     key,
			Date:    meta["date"],
			Updated: parseEntryUpdated(meta["updated"]),
			Score:   parseEntryScore(meta["score"]),
			Content: strings.TrimSpace(content),
		})
		rest = afterHeader[end+len(endMarker):]
	}
	return entries
}

func splitSessionHeader(header string) (string, map[string]string) {
	fields := strings.Fields(header)
	if len(fields) == 0 {
		return "", nil
	}
	meta := map[string]string{}
	for _, field := range fields[1:] {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		meta[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return strings.TrimSpace(fields[0]), meta
}

func parseEntryUpdated(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func parseEntryScore(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}

func autoSessionID(sessionID string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(sessionID)))
	return "session-" + hex.EncodeToString(sum[:])[:16]
}

func safeProjectName(project, cwd string) string {
	project = strings.TrimSpace(project)
	if project == "" {
		project = filepath.Base(strings.TrimRight(strings.TrimSpace(cwd), string(filepath.Separator)))
	}
	if project == "" || project == "." || project == string(filepath.Separator) {
		project = "workspace"
	}
	project = filepath.Base(project)
	project = strings.Trim(project, ". ")
	if project == "" {
		return "workspace"
	}
	return project
}

func sessionStatus(agent *classifier.Agent, final bool) string {
	if agent == nil {
		if final {
			return string(classifier.StateDone)
		}
		return string(classifier.StateUnknown)
	}
	if final && agent.State != classifier.StateFailed {
		return string(classifier.StateDone)
	}
	if strings.TrimSpace(string(agent.State)) == "" {
		return string(classifier.StateUnknown)
	}
	return string(agent.State)
}

func sessionSnapshotHash(agent *classifier.Agent, status string) string {
	if agent == nil {
		return ""
	}
	parts := []string{
		digestPromptVersion,
		strings.TrimSpace(agent.ID),
		strings.TrimSpace(agent.Name),
		strings.TrimSpace(agent.Project),
		strings.TrimSpace(agent.Cwd),
		strings.TrimSpace(agent.Command),
		status,
		collapseSpaces(agent.Summary),
	}
	parts = append(parts, recentOutput(agent.LastLines))
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func transcriptEvidenceHash(agent classifier.Agent, transcript ToolTranscript) string {
	parts := []string{
		digestPromptVersion,
		strings.TrimSpace(agent.Project),
		strings.TrimSpace(agent.Cwd),
		strings.TrimSpace(transcript.Source),
		strings.TrimSpace(transcript.SessionID),
		strings.TrimSpace(transcript.Path),
		collapseSpaces(transcript.Excerpt),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func digestFromExisting(existing *Item) AgentDigest {
	if existing == nil {
		return AgentDigest{}
	}
	return digestFromFrontmatter(existing.Frontmatter)
}

func digestFromFrontmatter(fm Frontmatter) AgentDigest {
	return AgentDigest{
		Title:    fm.Title,
		Outcome:  fm.Outcome,
		Readout:  fm.Summary,
		Summary:  fm.Summary,
		Signals:  append([]string(nil), fm.Progress...),
		Progress: append([]string(nil), fm.Progress...),
		Friction: fm.Friction,
		Cause:    fm.Cause,
		Insight:  fm.Insight,
		Next:     fm.Next,
		Provider: fm.AIProvider,
	}
}

func digestTitle(digest AgentDigest, agent *classifier.Agent) string {
	if title := strings.TrimSpace(digest.Title); title != "" {
		return title
	}
	if summary := firstNonEmpty(digest.Insight, digest.Summary, digest.Outcome); summary != "" {
		return truncateText(collapseSpaces(summary), 70)
	}
	if agent != nil {
		if summary := strings.TrimSpace(agent.Summary); summary != "" {
			return truncateText(collapseSpaces(summary), 70)
		}
	}
	return "Brain readout"
}

func recentOutput(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	start := 0
	if len(lines) > 120 {
		start = len(lines) - 120
	}
	out := make([]string, 0, len(lines)-start)
	for _, line := range lines[start:] {
		out = append(out, strings.TrimRight(line, "\r\n"))
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

func collapseSpaces(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateText(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return strings.TrimSpace(value[:max-3]) + "..."
}
