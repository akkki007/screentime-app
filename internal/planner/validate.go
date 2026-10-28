package planner

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/akkki007/screentime-app/internal/ipc"
)

// The model's answer is untrusted. Everything below checks it against what
// really exists and drops what does not fit; nothing is repaired or guessed.
// Even when the endpoint ignored the JSON schema, an answer that passes
// these functions is one the UI can show.

// minConfidence is the least the model must claim for a category to be
// applied. It is the model's own, uncalibrated number: a guard against
// obvious guesses, not a measured accuracy.
const minConfidence = 0.5

const (
	maxReason  = 200
	maxSummary = 400
)

var errUnusable = errors.New("the model's answer was not usable; nothing was changed")

// decodeAnswer parses the model's text as one JSON object into v, tolerating
// a Markdown code fence (endpoints that ignore response_format add one).
func decodeAnswer(content string, v any) error {
	s := strings.TrimSpace(content)
	if rest, ok := strings.CutPrefix(s, "```"); ok {
		rest = strings.TrimPrefix(rest, "json")
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "```"))
	}
	if json.Unmarshal([]byte(s), v) != nil {
		return errUnusable
	}
	return nil
}

type modelSuggestion struct {
	Kind     string   `json:"kind"`
	AppID    *string  `json:"appId"`
	Category *string  `json:"category"`
	Minutes  *float64 `json:"minutes"`
	Reason   string   `json:"reason"`
}

func wholeMinutes(v *float64, lo, hi int64) (int64, bool) {
	if v == nil || math.IsNaN(*v) || *v != math.Trunc(*v) || *v < float64(lo) || *v > float64(hi) {
		return 0, false
	}
	return int64(*v), true
}

// validateSuggest turns the model's text into suggestions that name only
// apps and categories that exist, with minutes in range, at most
// ipc.MaxSuggestions of them.
func validateSuggest(content string, a *aggregates) (ipc.PlannerSuggestResult, error) {
	var ans struct {
		Summary     string            `json:"summary"`
		Suggestions []modelSuggestion `json:"suggestions"`
	}
	if err := decodeAnswer(content, &ans); err != nil {
		return ipc.PlannerSuggestResult{}, err
	}
	res := ipc.PlannerSuggestResult{
		Summary:     trimTo(cleanText(ans.Summary, maxSummary+1), maxSummary),
		Suggestions: []ipc.PlannerSuggestion{},
	}
	seen := map[string]bool{}
	for _, m := range ans.Suggestions {
		if len(res.Suggestions) >= ipc.MaxSuggestions {
			break
		}
		s, ok := validSuggestion(m, a)
		if !ok {
			continue
		}
		id := s.Kind + "\x00" + s.AppID
		if s.CategoryID != nil {
			id += fmt.Sprintf("\x00%d", *s.CategoryID)
		}
		if seen[id] && s.Kind != "note" && s.Kind != "break" {
			continue
		}
		seen[id] = true
		res.Suggestions = append(res.Suggestions, s)
	}
	return res, nil
}

func validSuggestion(m modelSuggestion, a *aggregates) (ipc.PlannerSuggestion, bool) {
	var s ipc.PlannerSuggestion
	reason := cleanText(m.Reason, maxReason+1)
	if reason == "" || len([]rune(reason)) > maxReason {
		return s, false
	}
	s.Kind, s.Reason = m.Kind, reason

	// A target, when the model names one, must exist. Naming both an app and
	// a category is ambiguous, so it is refused.
	hasApp := m.AppID != nil && *m.AppID != ""
	hasCat := m.Category != nil && *m.Category != ""
	if hasApp && hasCat {
		return s, false
	}
	if hasApp {
		if !a.known[*m.AppID] {
			return s, false
		}
		s.AppID = *m.AppID
	}
	if hasCat {
		id, ok := a.catIDs[*m.Category]
		if !ok {
			return s, false
		}
		s.CategoryID = &id
	}

	switch m.Kind {
	case "limit":
		mins, ok := wholeMinutes(m.Minutes, 5, 1440)
		if !ok || !(hasApp || hasCat) {
			return s, false
		}
		s.Minutes = mins
	case "focus":
		mins, ok := wholeMinutes(m.Minutes, 5, 180)
		if !ok {
			return s, false
		}
		s.Minutes = mins
	case "break":
		mins, ok := wholeMinutes(m.Minutes, 1, 60)
		if !ok {
			return s, false
		}
		s.Minutes = mins
		s.AppID, s.CategoryID = "", nil // a break is not about an app
	case "note":
		// Advice only: any number the model added means nothing.
	default:
		return s, false
	}
	return s, true
}

// validateCategorize keeps the answers that name a candidate app and a real
// category other than Other, with enough confidence, one per app.
func validateCategorize(content string, in *categorizeInput) ([]ipc.PlannerCategorization, error) {
	var ans struct {
		Results []struct {
			AppID      string   `json:"appId"`
			Category   string   `json:"category"`
			Confidence *float64 `json:"confidence"`
		} `json:"results"`
	}
	if err := decodeAnswer(content, &ans); err != nil {
		return nil, err
	}
	out := []ipc.PlannerCategorization{}
	done := map[string]bool{}
	for _, r := range ans.Results {
		id, ok := in.catIDs[r.Category] // Other is not in catIDs
		if !ok || !in.byID[r.AppID] || done[r.AppID] {
			continue
		}
		if r.Confidence == nil || math.IsNaN(*r.Confidence) || *r.Confidence < minConfidence || *r.Confidence > 1 {
			continue
		}
		done[r.AppID] = true
		out = append(out, ipc.PlannerCategorization{AppID: r.AppID, CategoryID: id, Confidence: *r.Confidence})
	}
	return out, nil
}
