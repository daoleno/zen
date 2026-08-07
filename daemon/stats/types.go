package stats

// StatsResponse is sent to the app in response to a "get_stats" request.
type StatsResponse struct {
	Type                   string                       `json:"type"`
	Ranges                 map[string]*RangeData        `json:"ranges"`
	CodexSubscription      *CodexSubscriptionUsage      `json:"codexSubscription,omitempty"`
	OpenCodeGoSubscription *OpenCodeGoSubscriptionUsage `json:"opencodeGoSubscription,omitempty"`
}

// CodexSubscriptionUsage describes official ChatGPT-backed Codex quota. It
// intentionally contains no credential or account identifiers.
type CodexSubscriptionUsage struct {
	AuthKind  string             `json:"authKind"`
	State     string             `json:"state"`
	Plan      string             `json:"plan,omitempty"`
	Windows   []CodexUsageWindow `json:"windows,omitempty"`
	FetchedAt string             `json:"fetchedAt,omitempty"`
	Stale     bool               `json:"stale,omitempty"`
}

// CodexUsageWindow is one rolling quota window. UsedPercent uses the Codex
// backend's consumed-percentage semantics (0 means unused, 100 exhausted).
type CodexUsageWindow struct {
	Name          string  `json:"name"`
	UsedPercent   float64 `json:"usedPercent"`
	WindowMinutes int64   `json:"windowMinutes,omitempty"`
	ResetsAt      string  `json:"resetsAt,omitempty"`
}

// OpenCodeGoSubscriptionUsage describes an OpenCode Go subscription confirmed
// against the official OpenCode Go services. UsageAvailable is true only when
// the authenticated subscription server-function response yielded at least
// one usage window in the same refresh; it is never guessed or reused from an
// older refresh. Window limits are the plan facts published in the OpenCode
// Go documentation ($12/5h, $30/week, $60/month). The projection contains no
// credentials, account identifiers, or cookies.
type OpenCodeGoSubscriptionUsage struct {
	AuthKind       string                  `json:"authKind"`
	State          string                  `json:"state"`
	Plan           string                  `json:"plan,omitempty"`
	FetchedAt      string                  `json:"fetchedAt,omitempty"`
	UsageAvailable bool                    `json:"usageAvailable"`
	Windows        []OpenCodeGoUsageWindow `json:"windows,omitempty"`
}

// OpenCodeGoUsageWindow is one dashboard usage window. UsedPercent is the
// dashboard's consumed-percentage (0 unused, 100 exhausted); LimitUSD is the
// documented plan limit for the window.
type OpenCodeGoUsageWindow struct {
	Name           string  `json:"name"`
	UsedPercent    float64 `json:"usedPercent"`
	LimitUSD       float64 `json:"limitUsd"`
	ResetInSeconds int64   `json:"resetInSeconds,omitempty"`
	ResetsAt       string  `json:"resetsAt,omitempty"`
}

// RangeData holds aggregated stats for a single time range.
type RangeData struct {
	Cost                float64       `json:"cost"`
	CostKnown           bool          `json:"costKnown"`
	TotalTokens         int64         `json:"totalTokens"`
	TotalTokensKnown    bool          `json:"totalTokensKnown"`
	InputTokens         int64         `json:"inputTokens"`
	OutputTokens        int64         `json:"outputTokens"`
	ReasoningTokens     int64         `json:"reasoningTokens"`
	CacheRead           int64         `json:"cacheRead"`
	CacheCreate         int64         `json:"cacheCreate"`
	TokenBreakdownKnown bool          `json:"tokenBreakdownKnown"`
	Sessions            int           `json:"sessions"`
	Models              []ModelStat   `json:"models"`
	Projects            []ProjectStat `json:"projects"`
	Skills              []SkillStat   `json:"skills"`
	Tools               []ToolStat    `json:"tools"`
	Days                []DayCell     `json:"days"` // Per-day activity, sorted by date ascending
}

// DayCell represents a single day's aggregated activity.
type DayCell struct {
	Date                string  `json:"date"` // "2006-01-02"
	TotalTokens         int64   `json:"totalTokens"`
	TotalTokensKnown    bool    `json:"totalTokensKnown"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	ReasoningTokens     int64   `json:"reasoningTokens"`
	CacheRead           int64   `json:"cacheRead"`
	CacheCreate         int64   `json:"cacheCreate"`
	TokenBreakdownKnown bool    `json:"tokenBreakdownKnown"`
	Cost                float64 `json:"cost"`
	CostKnown           bool    `json:"costKnown"`
	Sessions            int     `json:"sessions"`
}

// ModelStat tracks usage for a single LLM model.
type ModelStat struct {
	Name                string  `json:"name"`
	TotalTokens         int64   `json:"totalTokens"`
	TotalTokensKnown    bool    `json:"totalTokensKnown"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	ReasoningTokens     int64   `json:"reasoningTokens"`
	CacheRead           int64   `json:"cacheRead"`
	CacheCreate         int64   `json:"cacheCreate"`
	TokenBreakdownKnown bool    `json:"tokenBreakdownKnown"`
	Cost                float64 `json:"cost"`
	CostKnown           bool    `json:"costKnown"`
	Sessions            int     `json:"sessions"`
}

// ProjectStat tracks usage for a single project directory.
type ProjectStat struct {
	Name                string  `json:"name"`
	TotalTokens         int64   `json:"totalTokens"`
	TotalTokensKnown    bool    `json:"totalTokensKnown"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	ReasoningTokens     int64   `json:"reasoningTokens"`
	CacheRead           int64   `json:"cacheRead"`
	CacheCreate         int64   `json:"cacheCreate"`
	TokenBreakdownKnown bool    `json:"tokenBreakdownKnown"`
	Cost                float64 `json:"cost"`
	CostKnown           bool    `json:"costKnown"`
	Sessions            int     `json:"sessions"`
}

// SkillStat tracks invocation counts for a Claude Code skill (slash command).
type SkillStat struct {
	Name     string   `json:"name"`
	Calls    int      `json:"calls"`
	Projects []string `json:"projects"`
}

// ToolStat tracks invocation counts for a low-level tool (Read, Edit, Bash, etc.).
type ToolStat struct {
	Name  string `json:"name"`
	Calls int    `json:"calls"`
}
