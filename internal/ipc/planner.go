package ipc

import (
	"encoding/json"
	"strings"
	"unicode"
)

// The optional planner's RPC surface (docs/adr/0008-local-planner-sidecar.md).
// Every method is additive; the settings it needs live in Settings.

// Limits on planner input, shared with the planner package's validators.
const (
	MaxGoalChars   = 500
	MaxKeyBytes    = 512
	MaxCategorize  = 50
	MaxAppIDChars  = 300
	MaxSuggestions = 8
)

// PlannerStatus is the planner.status result. Host is the hostname a remote
// provider would contact ("" for local); ServerFound and ModelFound only mean
// something for the local provider. The API key is never part of it.
type PlannerStatus struct {
	Enabled     bool    `json:"enabled"`
	Provider    string  `json:"provider"`
	State       string  `json:"state"` // off | starting | ready | error
	ServerFound bool    `json:"serverFound"`
	ModelFound  bool    `json:"modelFound"`
	KeySet      bool    `json:"keySet"`
	Host        string  `json:"host"`
	LastError   *string `json:"lastError"`
}

// PlannerSuggestion is one validated suggestion; it never changes anything by
// itself, the UI applies it through limits.set and friends.
type PlannerSuggestion struct {
	Kind       string `json:"kind"` // limit | focus | break | note
	AppID      string `json:"appId,omitempty"`
	CategoryID *int64 `json:"categoryId,omitempty"`
	Minutes    int64  `json:"minutes,omitempty"`
	Reason     string `json:"reason"`
}

type PlannerSuggestResult struct {
	Summary     string              `json:"summary"`
	Suggestions []PlannerSuggestion `json:"suggestions"`
}

// PlannerCategorization is one app the model categorised and the daemon applied.
type PlannerCategorization struct {
	AppID      string  `json:"appId"`
	CategoryID int64   `json:"categoryId"`
	Confidence float64 `json:"confidence"`
}

// ParsePlannerSetKey returns the API key, trimmed. Empty clears it. A key
// ends up in an HTTP header, so whitespace and control characters inside it
// are refused.
func ParsePlannerSetKey(params json.RawMessage) (string, error) {
	o, err := parseObject(params)
	if err != nil {
		return "", err
	}
	raw, _ := o.String("key")
	key := strings.TrimSpace(raw)
	if len(key) > MaxKeyBytes {
		o.fail("key", "String must contain at most 512 character(s)")
	}
	if strings.IndexFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		o.fail("key", "must not contain whitespace or control characters")
	}
	o.strict()
	return key, o.done()
}

// ParsePlannerSuggest returns the optional goal text.
func ParsePlannerSuggest(params json.RawMessage) (goal string, err error) {
	o, verr := parseObject(params)
	if verr != nil {
		return "", verr
	}
	if g := o.OptString("goal", MaxGoalChars, nil, ""); g != nil {
		goal = *g
	}
	o.strict()
	return goal, o.done()
}

// ParsePlannerCategorize returns the apps to look at; nil means every
// eligible app (up to MaxCategorize). Params may be omitted altogether.
func ParsePlannerCategorize(params json.RawMessage) ([]string, error) {
	if isAbsent(params) {
		return nil, nil
	}
	o, err := parseObject(params)
	if err != nil {
		return nil, err
	}
	ids := o.OptStringList("appIds", MaxCategorize, MaxAppIDChars)
	o.strict()
	return ids, o.done()
}
