package ipc

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Issue is one validation problem, reported as "path: message" like the Zod
// schemas in packages/shared, so the UI can show it next to the field.
type Issue struct {
	Path    string // "" means the params as a whole
	Message string
}

// ValidationError lists every issue found, in schema order.
type ValidationError struct{ Issues []Issue }

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Issues))
	for i, issue := range e.Issues {
		path := issue.Path
		if path == "" {
			path = "params"
		}
		parts[i] = path + ": " + issue.Message
	}
	return strings.Join(parts, "; ")
}

// object reads one JSON object field by field, collecting issues.
type object struct {
	fields map[string]json.RawMessage
	issues []Issue
	seen   map[string]bool
}

// parseObject decodes params that must be an object. A missing or null
// params is "Required"; any other non-object is a type error.
func parseObject(params json.RawMessage) (*object, *ValidationError) {
	if isAbsent(params) {
		return nil, &ValidationError{[]Issue{{"", "Required"}}}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(params, &fields); err != nil || fields == nil {
		return nil, &ValidationError{[]Issue{{"", "Expected object, received " + jsonType(params)}}}
	}
	return &object{fields: fields, seen: map[string]bool{}}, nil
}

// expectVoid accepts only absent params, for methods that take none.
func expectVoid(params json.RawMessage) error {
	if len(params) == 0 {
		return nil
	}
	return &ValidationError{[]Issue{{"", "Expected void, received " + jsonType(params)}}}
}

func isAbsent(raw json.RawMessage) bool { return len(raw) == 0 || isNull(raw) }
func isNull(raw json.RawMessage) bool   { return strings.TrimSpace(string(raw)) == "null" }

func jsonType(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	switch {
	case s == "null":
		return "null"
	case s == "true" || s == "false":
		return "boolean"
	case strings.HasPrefix(s, `"`):
		return "string"
	case strings.HasPrefix(s, "["):
		return "array"
	case strings.HasPrefix(s, "{"):
		return "object"
	default:
		return "number"
	}
}

func (o *object) fail(path, msg string) { o.issues = append(o.issues, Issue{path, msg}) }

func (o *object) raw(key string) (json.RawMessage, bool) {
	o.seen[key] = true
	v, ok := o.fields[key]
	return v, ok
}

// done returns the collected issues, if any.
func (o *object) done() error {
	if len(o.issues) == 0 {
		return nil
	}
	return &ValidationError{o.issues}
}

// strict adds an issue for keys the schema doesn't know (Zod's .strict()).
// Without it, unknown keys are ignored, as Zod strips them.
func (o *object) strict() {
	var unknown []string
	for k := range o.fields {
		if !o.seen[k] {
			unknown = append(unknown, "'"+k+"'")
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		o.fail("", "Unrecognized key(s) in object: "+strings.Join(unknown, ", "))
	}
}

// IntRule bounds an integer field. Zero values mean "no bound".
type IntRule struct {
	Positive bool
	Min, Max *int64
}

func bound(v int64) *int64 { return &v }

// Int reads a required integer.
func (o *object) Int(key string, rule IntRule) int64 {
	v, _ := o.int(key, rule, true)
	return v
}

// OptInt reads an optional integer.
func (o *object) OptInt(key string, rule IntRule) *int64 {
	v, ok := o.int(key, rule, false)
	if !ok {
		return nil
	}
	return &v
}

func (o *object) int(key string, rule IntRule, required bool) (int64, bool) {
	raw, present := o.raw(key)
	if !present {
		if required {
			o.fail(key, "Required")
		}
		return 0, false
	}
	if jsonType(raw) != "number" {
		o.fail(key, "Expected number, received "+jsonType(raw))
		return 0, false
	}
	f, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || math.IsInf(f, 0) {
		o.fail(key, "Expected number, received "+string(raw))
		return 0, false
	}
	if f != math.Trunc(f) {
		o.fail(key, "Expected integer, received float")
		return 0, false
	}
	// Zod's int() accepts any integral float; clamp what int64 can't hold.
	v := int64(f)
	if f > math.MaxInt64 {
		v = math.MaxInt64
	} else if f < math.MinInt64 {
		v = math.MinInt64
	}
	switch {
	case rule.Positive && v <= 0:
		o.fail(key, "Number must be greater than 0")
	case rule.Min != nil && v < *rule.Min:
		o.fail(key, fmt.Sprintf("Number must be greater than or equal to %d", *rule.Min))
	case rule.Max != nil && v > *rule.Max:
		o.fail(key, fmt.Sprintf("Number must be less than or equal to %d", *rule.Max))
	default:
		return v, true
	}
	return 0, false
}

// NullableInt reads a required field that may be null.
func (o *object) NullableInt(key string) (*int64, bool) {
	raw, present := o.raw(key)
	if present && isNull(raw) {
		return nil, true
	}
	v, ok := o.int(key, IntRule{}, true)
	if !ok {
		return nil, false
	}
	return &v, true
}

// String reads a required string, optionally capped in length and matched
// against a pattern (with the schema's custom message).
func (o *object) String(key string) (string, bool) { return o.str(key, true, 0, nil, "") }

// OptString reads an optional string.
func (o *object) OptString(key string, maxLen int, re *regexp.Regexp, reMsg string) *string {
	if raw, present := o.fields[key]; !present || isNull(raw) {
		o.seen[key] = true
		if present {
			o.fail(key, "Expected string, received null")
		}
		return nil
	}
	v, ok := o.str(key, false, maxLen, re, reMsg)
	if !ok {
		return nil
	}
	return &v
}

func (o *object) str(key string, required bool, maxLen int, re *regexp.Regexp, reMsg string) (string, bool) {
	raw, present := o.raw(key)
	if !present {
		o.fail(key, "Required")
		return "", false
	}
	var v string
	if jsonType(raw) != "string" || json.Unmarshal(raw, &v) != nil {
		o.fail(key, "Expected string, received "+jsonType(raw))
		return "", false
	}
	// Zod counts UTF-16 code units; domains are ASCII, so bytes agree.
	if maxLen > 0 && len(v) > maxLen {
		o.fail(key, fmt.Sprintf("String must contain at most %d character(s)", maxLen))
		return "", false
	}
	if re != nil && !re.MatchString(v) {
		o.fail(key, reMsg)
		return "", false
	}
	return v, true
}

// Enum reads a required string that must be one of values.
func (o *object) Enum(key string, values ...string) (string, bool) {
	raw, present := o.raw(key)
	if !present {
		o.fail(key, "Required")
		return "", false
	}
	var v string
	if jsonType(raw) != "string" || json.Unmarshal(raw, &v) != nil {
		o.fail(key, fmt.Sprintf("Expected %s, received %s", quoted(values, " | "), jsonType(raw)))
		return "", false
	}
	for _, allowed := range values {
		if v == allowed {
			return v, true
		}
	}
	o.fail(key, fmt.Sprintf("Invalid enum value. Expected %s, received '%s'", quoted(values, " | "), v))
	return "", false
}

func quoted(values []string, sep string) string {
	q := make([]string, len(values))
	for i, v := range values {
		q[i] = "'" + v + "'"
	}
	return strings.Join(q, sep)
}

// Bool reads a boolean; when optional and absent it returns def.
func (o *object) Bool(key string, required bool, def bool) (bool, bool) {
	raw, present := o.raw(key)
	if !present {
		if required {
			o.fail(key, "Required")
			return false, false
		}
		return def, true
	}
	var v bool
	if jsonType(raw) != "boolean" || json.Unmarshal(raw, &v) != nil {
		o.fail(key, "Expected boolean, received "+jsonType(raw))
		return false, false
	}
	return v, true
}

// Has reports whether key is present (for partial updates).
func (o *object) Has(key string) bool {
	_, ok := o.fields[key]
	return ok
}
