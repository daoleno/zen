package stats

// StatsResponse is sent to the app in response to a "get_stats" request.
type StatsResponse struct {
	Type   string                `json:"type"`
	Ranges map[string]*RangeData `json:"ranges"`
}

// RangeData holds aggregated stats for a single time range.
type RangeData struct {
	Cost            float64       `json:"cost"`
	TotalTokens     int64         `json:"totalTokens"`
	InputTokens     int64         `json:"inputTokens"`
	OutputTokens    int64         `json:"outputTokens"`
	ReasoningTokens int64         `json:"reasoningTokens"`
	CacheRead       int64         `json:"cacheRead"`
	CacheCreate     int64         `json:"cacheCreate"`
	Sessions        int           `json:"sessions"`
	Models          []ModelStat   `json:"models"`
	Projects        []ProjectStat `json:"projects"`
	Skills          []SkillStat   `json:"skills"`
	Tools           []ToolStat    `json:"tools"`
	Days            []DayCell     `json:"days"` // Per-day activity, sorted by date ascending
}

// DayCell represents a single day's aggregated activity.
type DayCell struct {
	Date            string  `json:"date"` // "2006-01-02"
	TotalTokens     int64   `json:"totalTokens"`
	InputTokens     int64   `json:"inputTokens"`
	OutputTokens    int64   `json:"outputTokens"`
	ReasoningTokens int64   `json:"reasoningTokens"`
	CacheRead       int64   `json:"cacheRead"`
	CacheCreate     int64   `json:"cacheCreate"`
	Cost            float64 `json:"cost"`
	Sessions        int     `json:"sessions"`
}

// ModelStat tracks usage for a single LLM model.
type ModelStat struct {
	Name            string  `json:"name"`
	TotalTokens     int64   `json:"totalTokens"`
	InputTokens     int64   `json:"inputTokens"`
	OutputTokens    int64   `json:"outputTokens"`
	ReasoningTokens int64   `json:"reasoningTokens"`
	CacheRead       int64   `json:"cacheRead"`
	CacheCreate     int64   `json:"cacheCreate"`
	Cost            float64 `json:"cost"`
	Sessions        int     `json:"sessions"`
}

// ProjectStat tracks usage for a single project directory.
type ProjectStat struct {
	Name            string  `json:"name"`
	TotalTokens     int64   `json:"totalTokens"`
	InputTokens     int64   `json:"inputTokens"`
	OutputTokens    int64   `json:"outputTokens"`
	ReasoningTokens int64   `json:"reasoningTokens"`
	CacheRead       int64   `json:"cacheRead"`
	CacheCreate     int64   `json:"cacheCreate"`
	Cost            float64 `json:"cost"`
	Sessions        int     `json:"sessions"`
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
