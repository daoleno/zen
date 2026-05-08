package work

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

const (
	autoBlockStart = "<!-- zen:auto:start -->"
	autoBlockEnd   = "<!-- zen:auto:end -->"
	pendingTitle   = "Analyzing Work Session"
)

var autoTitleSuffixRe = regexp.MustCompile(`\s+\([^)]+\)\s*$`)

// SessionLogger turns watcher session snapshots into durable work log files.
type SessionLogger struct {
	store       *Store
	digester    AgentDigestProvider
	now         func() time.Time
	minInterval time.Duration
	digestEvery time.Duration

	mu             sync.Mutex
	lastWrite      map[string]time.Time
	lastHash       map[string]string
	lastDigest     map[string]time.Time
	lastDigestHash map[string]string
	pendingDigest  map[string]bool
	digestSem      chan struct{}
	syncDigest     bool
}

func NewSessionLogger(store *Store, digester AgentDigestProvider) *SessionLogger {
	return &SessionLogger{
		store:          store,
		digester:       digester,
		now:            func() time.Time { return time.Now().UTC() },
		minInterval:    2 * time.Second,
		digestEvery:    90 * time.Second,
		lastWrite:      map[string]time.Time{},
		lastHash:       map[string]string{},
		lastDigest:     map[string]time.Time{},
		lastDigestHash: map[string]string{},
		pendingDigest:  map[string]bool{},
		digestSem:      make(chan struct{}, 1),
	}
}

// RecordAgent upserts the work log for an agent session. Non-forced updates are
// throttled so active output does not rewrite the Markdown file on every poll.
func (l *SessionLogger) RecordAgent(agent *classifier.Agent, final, force bool) (*Item, error) {
	if l == nil || l.store == nil || agent == nil || strings.TrimSpace(agent.ID) == "" {
		return nil, nil
	}

	now := l.now().UTC()
	status := sessionStatus(agent, final)
	hash := sessionSnapshotHash(agent, status)
	if !force && l.shouldSkip(agent.ID, hash, now) {
		return nil, nil
	}

	existing, ok := l.store.GetByAgentSession(agent.ID)
	if !ok {
		existing, ok = l.store.GetByID(autoSessionID(agent.ID))
	}

	item := buildSessionItem(l.store.Root, existing, agent, now, status, digestFromExisting(existing), "")
	written, err := l.store.Write(item, time.Time{})
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	l.lastWrite[agent.ID] = now
	l.lastHash[agent.ID] = hash
	l.mu.Unlock()

	l.scheduleDigest(agent, status, hash, final, force, now)
	return written, nil
}

func (l *SessionLogger) shouldSkip(sessionID, hash string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastHash[sessionID] == hash {
		return true
	}
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
	if l.pendingDigest[sessionID] || l.lastDigestHash[sessionID] == hash {
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

func (l *SessionLogger) digestAndWrite(agent *classifier.Agent, status, hash string) {
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

	existing, _ := l.store.GetByAgentSession(sessionID)
	now := l.now().UTC()
	input := AgentDigestInput{
		Agent:           *agent,
		Status:          status,
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
	finishedAt := l.now().UTC()

	l.mu.Lock()
	currentHash := l.lastHash[sessionID]
	if currentHash != hash {
		l.mu.Unlock()
		return
	}
	l.lastDigest[sessionID] = finishedAt
	if err == nil {
		l.lastDigestHash[sessionID] = hash
	}
	l.mu.Unlock()

	if err != nil {
		l.writeDigestError(agent, status, hash, finishedAt, err)
		return
	}

	latest, _ := l.store.GetByAgentSession(sessionID)
	item := buildSessionItem(l.store.Root, latest, agent, finishedAt, status, digest, hash)
	if _, err := l.store.Write(item, time.Time{}); err != nil {
		l.mu.Lock()
		delete(l.lastDigestHash, sessionID)
		l.mu.Unlock()
	}
}

func (l *SessionLogger) writeDigestError(agent *classifier.Agent, status, hash string, now time.Time, digestErr error) {
	latest, _ := l.store.GetByAgentSession(agent.ID)
	digest := digestFromExisting(latest)
	item := buildSessionItem(l.store.Root, latest, agent, now, status, digest, "")
	item.Frontmatter.AIError = truncateText(collapseSpaces(digestErr.Error()), 180)
	item.Frontmatter.AIHash = hash
	title := digestTitle(digestFromFrontmatter(item.Frontmatter), agent)
	item.Body = mergeAutoBlock(item.Body, title, renderAutoBlock(agent, item.Frontmatter, now))
	if _, err := l.store.Write(item, time.Time{}); err != nil {
		return
	}
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

func buildSessionItem(root string, existing *Item, agent *classifier.Agent, now time.Time, status string, digest AgentDigest, digestHash string) *Item {
	id := autoSessionID(agent.ID)
	project := safeProjectName(agent.Project, agent.Cwd)
	title := digestTitle(digest, agent)
	path := filepath.Join(root, project, buildSessionFilename(now, title, id))
	frontmatter := Frontmatter{
		ID:           id,
		Created:      now,
		Started:      &now,
		Status:       status,
		Title:        digest.Title,
		Summary:      digest.Summary,
		Progress:     append([]string(nil), digest.Progress...),
		Next:         digest.Next,
		AgentSession: agent.ID,
		Cwd:          strings.TrimSpace(agent.Cwd),
		Command:      strings.TrimSpace(agent.Command),
		AIProvider:   digest.Provider,
		AIHash:       digestHash,
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
		if frontmatter.Created.IsZero() {
			frontmatter.Created = now
		}
		if frontmatter.Started == nil {
			started := frontmatter.Created
			frontmatter.Started = &started
		}
		frontmatter.AgentSession = agent.ID
		frontmatter.Cwd = strings.TrimSpace(agent.Cwd)
		frontmatter.Command = strings.TrimSpace(agent.Command)
		frontmatter.Status = status
		if hasDigestContent(digest) {
			frontmatter.Title = digest.Title
			frontmatter.Summary = digest.Summary
			frontmatter.Progress = append([]string(nil), digest.Progress...)
			frontmatter.Next = digest.Next
			frontmatter.AIProvider = digest.Provider
			frontmatter.AIHash = digestHash
			frontmatter.AIError = ""
			updated := now
			frontmatter.AIUpdated = &updated
		}
		title = digestTitle(digestFromFrontmatter(frontmatter), agent)
	}

	if isTerminalStatus(frontmatter.Status) {
		if frontmatter.Done == nil {
			done := now
			frontmatter.Done = &done
		}
	} else {
		frontmatter.Done = nil
	}

	return &Item{
		ID:          frontmatter.ID,
		Path:        path,
		Project:     project,
		Body:        mergeAutoBlock(existingBody, title, renderAutoBlock(agent, frontmatter, now)),
		Frontmatter: frontmatter,
	}
}

func autoSessionID(sessionID string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(sessionID)))
	return "session-" + hex.EncodeToString(sum[:])[:16]
}

func buildSessionFilename(now time.Time, title, id string) string {
	return now.Format("2006-01-02") + "-" + slugifySessionTitle(title, id) + ".md"
}

func slugifySessionTitle(title, fallback string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(title), "#"))
	if trimmed == "" {
		return strings.ToLower(fallback)
	}

	out := make([]rune, 0, len(trimmed))
	lastDash := false
	for _, r := range strings.ToLower(trimmed) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_' || r == '.':
			if !lastDash {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(string(out), "-")
	if slug == "" {
		slug = strings.ToLower(fallback)
	}
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	if slug == "" {
		return strings.ToLower(fallback)
	}
	return slug
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

func isTerminalStatus(status string) bool {
	return status == string(classifier.StateDone) || status == string(classifier.StateFailed)
}

func sessionTitle(agent *classifier.Agent) string {
	if agent == nil {
		return "Agent Session"
	}
	title := autoTitleSuffixRe.ReplaceAllString(strings.TrimSpace(agent.Name), "")
	if isGenericSessionTitle(title) {
		if summary := strings.TrimSpace(agent.Summary); summary != "" && !strings.HasPrefix(strings.ToLower(summary), "no new output") {
			title = summary
		}
	}
	if title == "" {
		title = strings.TrimSpace(agent.Command)
	}
	if title == "" {
		title = "Agent Session"
	}
	return truncateText(collapseSpaces(title), 80)
}

func isGenericSessionTitle(title string) bool {
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "", "claude", "claude code", "codex", "zsh", "bash", "sh", "fish", "tmux", "terminal":
		return true
	default:
		return false
	}
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
		Summary:  fm.Summary,
		Progress: append([]string(nil), fm.Progress...),
		Next:     fm.Next,
		Provider: fm.AIProvider,
	}
}

func digestTitle(digest AgentDigest, agent *classifier.Agent) string {
	if title := strings.TrimSpace(digest.Title); title != "" {
		return title
	}
	return pendingTitle
}

func renderAutoBlock(agent *classifier.Agent, frontmatter Frontmatter, now time.Time) string {
	status := frontmatter.Status
	lines := []string{
		autoBlockStart,
		"## Brief",
		"",
	}

	if summary := strings.TrimSpace(frontmatter.Summary); summary != "" {
		lines = append(lines, summary)
	} else if frontmatter.AIError != "" {
		lines = append(lines, "AI digest unavailable. The daemon will retry when the session changes.")
	} else {
		lines = append(lines, "AI digest pending.")
	}

	if len(frontmatter.Progress) > 0 {
		lines = append(lines, "", "## Progress", "")
		for _, item := range frontmatter.Progress {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				lines = append(lines, "- "+trimmed)
			}
		}
	}

	if next := strings.TrimSpace(frontmatter.Next); next != "" {
		lines = append(lines, "", "## Next", "", next)
	}

	lines = append(lines,
		"",
		"## Context",
		"",
		fmt.Sprintf("- Status: %s", status),
		fmt.Sprintf("- Agent: %s", collapseSpaces(autoTitleSuffixRe.ReplaceAllString(strings.TrimSpace(agent.Name), ""))),
		fmt.Sprintf("- Workspace: %s", safeProjectName(agent.Project, agent.Cwd)),
		fmt.Sprintf("- Updated: %s", now.Format(time.RFC3339)),
	)
	if provider := strings.TrimSpace(frontmatter.AIProvider); provider != "" {
		lines = append(lines, fmt.Sprintf("- Read by: %s", provider))
	}
	if frontmatter.AIError != "" {
		lines = append(lines, fmt.Sprintf("- AI error: %s", frontmatter.AIError))
	}
	lines = append(lines, "", autoBlockEnd)
	return strings.Join(lines, "\n")
}

func mergeAutoBlock(existingBody, title, autoBlock string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Agent Session"
	}
	if strings.TrimSpace(existingBody) == "" {
		return "# " + title + "\n\n" + autoBlock + "\n\n## Notes\n\n"
	}

	start := strings.Index(existingBody, autoBlockStart)
	end := strings.Index(existingBody, autoBlockEnd)
	if start >= 0 && end >= start {
		end += len(autoBlockEnd)
		return strings.TrimRight(existingBody[:start], "\n") + "\n\n" + autoBlock + "\n\n" + strings.TrimLeft(existingBody[end:], "\n")
	}

	lines := strings.Split(existingBody, "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "# ") {
		if shouldReplaceAutoTitle(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), "#")), title) {
			lines[0] = "# " + title
		}
		rest := strings.Join(lines[1:], "\n")
		return strings.TrimRight(lines[0], "\n") + "\n\n" + autoBlock + "\n\n" + strings.TrimLeft(rest, "\n")
	}
	return "# " + title + "\n\n" + autoBlock + "\n\n" + strings.TrimLeft(existingBody, "\n")
}

func shouldReplaceAutoTitle(existingTitle, nextTitle string) bool {
	existingTitle = strings.TrimSpace(existingTitle)
	nextTitle = strings.TrimSpace(nextTitle)
	if existingTitle == "" || nextTitle == "" || existingTitle == nextTitle {
		return false
	}
	return existingTitle == pendingTitle || isGenericSessionTitle(existingTitle)
}

func recentOutput(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	start := 0
	if len(lines) > 20 {
		start = len(lines) - 20
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
