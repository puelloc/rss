package model

import "time"

type FilterMode string

const (
	ModeAny FilterMode = "any"
	ModeAll FilterMode = "all"
)

// FilterRule matches a single field of an item.
// Ops: contains | not_contains | equals | starts_with | ends_with | regex
type FilterRule struct {
	Field         string `json:"field"` // title | description | content | link | author | categories
	Op            string `json:"op"`
	Value         string `json:"value"`
	CaseSensitive bool   `json:"case_sensitive"`
}

// FilterGroup holds rules plus the boolean mode used to combine them.
type FilterGroup struct {
	Mode  FilterMode   `json:"mode"`
	Rules []FilterRule `json:"rules"`
}

// TransformRule mutates a field.
// Ops: replace | regex_replace | prefix | suffix | strip_html | trim | lower | upper
type TransformRule struct {
	Target  string `json:"target"` // title | description | content | link
	Op      string `json:"op"`
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

type Rules struct {
	Include      FilterGroup     `json:"include"`
	Exclude      FilterGroup     `json:"exclude"`
	Transforms   []TransformRule `json:"transforms"`
	LinkTemplate string          `json:"link_template"`
	MaxItems     int             `json:"max_items"`
	MaxAgeDays   int             `json:"max_age_days"`
	SortDesc     bool            `json:"sort_desc"`
}

type Feed struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	SourceURL     string    `json:"source_url"`
	Description   string    `json:"description"`
	Enabled       bool      `json:"enabled"`
	FetchInterval int       `json:"fetch_interval"` // minutes
	Rules         Rules     `json:"rules"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

