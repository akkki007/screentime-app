// Package ipc is the Go side of the daemon's JSON-RPC contract: payload
// types and request validation, matching packages/shared/src/ipc.ts and
// checked against contract/ (schema and golden fixtures).
//
// Nullable fields are pointers without omitempty, so they marshal as null;
// optional fields use omitempty, so they are left out, as in the Zod types.
package ipc

import (
	"encoding/json"
	"regexp"
)

// Version is the IPC protocol major version (IPC_VERSION).
const Version = 1

type UsageSummaryRequest struct {
	From, To int64
	GroupBy  string
}

type UsageRow struct {
	Key string `json:"key"`
	Ms  int64  `json:"ms"`
}

type Session struct {
	ID      int64  `json:"id"`
	AppID   string `json:"appId"`
	Title   string `json:"title,omitempty"`
	StartTs int64  `json:"startTs"`
	EndTs   int64  `json:"endTs"`
	Source  string `json:"source"`
}

type Category struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Color      *string `json:"color"`
	Productive *int64  `json:"productive"`
}

type AppInfo struct {
	AppID      string  `json:"appId"`
	Name       *string `json:"name"`
	Icon       *string `json:"icon"`
	CategoryID *int64  `json:"categoryId"`
}

type FocusMode struct {
	Active bool   `json:"active"`
	Until  *int64 `json:"until"`
}

type TrackerStatus struct {
	Paused       bool      `json:"paused"`
	ResumeAt     *int64    `json:"resumeAt"`
	Idle         bool      `json:"idle"`
	CurrentAppID *string   `json:"currentAppId"`
	Since        *int64    `json:"since"`
	FocusMode    FocusMode `json:"focusMode"`
}

type Limit struct {
	ID         *int64 `json:"id,omitempty"`
	TargetType string `json:"targetType"`
	Target     string `json:"target"`
	DailyMs    int64  `json:"dailyMs"`
	Schedule   string `json:"schedule,omitempty"`
	Action     string `json:"action"`
}

// Settings mirrors packages/shared/src/settings.ts.
type Settings struct {
	CaptureTitles         bool   `json:"captureTitles"`
	IdleThresholdMinutes  int64  `json:"idleThresholdMinutes"`
	BreakRemindersEnabled bool   `json:"breakRemindersEnabled"`
	BreakEveryMinutes     int64  `json:"breakEveryMinutes"`
	BreakLengthMinutes    int64  `json:"breakLengthMinutes"`
	DowntimeEnabled       bool   `json:"downtimeEnabled"`
	DowntimeStart         string `json:"downtimeStart"`
	DowntimeEnd           string `json:"downtimeEnd"`
	OnboardingDone        bool   `json:"onboardingDone"`
}

// DefaultSettings is DEFAULT_SETTINGS: privacy-sensitive options off.
var DefaultSettings = Settings{
	IdleThresholdMinutes: 3,
	BreakEveryMinutes:    50,
	BreakLengthMinutes:   5,
	DowntimeStart:        "22:00",
	DowntimeEnd:          "07:00",
}

// SettingsPatch is a partial update; nil fields are left unchanged.
type SettingsPatch struct {
	CaptureTitles         *bool
	IdleThresholdMinutes  *int64
	BreakRemindersEnabled *bool
	BreakEveryMinutes     *int64
	BreakLengthMinutes    *int64
	DowntimeEnabled       *bool
	DowntimeStart         *string
	DowntimeEnd           *string
	OnboardingDone        *bool
}

type DataExportRequest struct {
	Format   string
	From, To *int64
}

type DataExportResponse struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type BrowserActiveTab struct {
	Domain *string
	Active bool
}

// Notifications (daemon → clients).
type FocusEvent struct {
	AppID string `json:"appId"`
	Since int64  `json:"since"`
}

type LimitHitEvent struct {
	LimitID int64  `json:"limitId"`
	Action  string `json:"action"`
}

type ReminderEvent struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type OK struct {
	OK bool `json:"ok"`
}

var (
	clockRe    = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	scheduleRe = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d-([01]\d|2[0-3]):[0-5]\d$`)
)

const maxMinutes = 24 * 60

// ExpectVoid validates params for methods that take none.
func ExpectVoid(params json.RawMessage) error { return expectVoid(params) }

func ParseUsageSummary(params json.RawMessage) (UsageSummaryRequest, error) {
	o, err := parseObject(params)
	if err != nil {
		return UsageSummaryRequest{}, err
	}
	r := UsageSummaryRequest{From: o.Int("from", IntRule{}), To: o.Int("to", IntRule{})}
	r.GroupBy, _ = o.Enum("groupBy", "app", "category", "hour", "day")
	return r, o.done()
}

func ParseUsageTimeline(params json.RawMessage) (string, error) {
	o, err := parseObject(params)
	if err != nil {
		return "", err
	}
	date, _ := o.String("date")
	return date, o.done()
}

func ParseRange(params json.RawMessage) (from, to int64, err error) {
	o, verr := parseObject(params)
	if verr != nil {
		return 0, 0, verr
	}
	return o.Int("from", IntRule{}), o.Int("to", IntRule{}), o.done()
}

func ParseLimit(params json.RawMessage) (Limit, error) {
	o, err := parseObject(params)
	if err != nil {
		return Limit{}, err
	}
	var l Limit
	l.ID = o.OptInt("id", IntRule{})
	l.TargetType, _ = o.Enum("targetType", "app", "category", "domain")
	l.Target, _ = o.String("target")
	l.DailyMs = o.Int("dailyMs", IntRule{Positive: true})
	if s := o.OptString("schedule", 0, scheduleRe, "expected HH:MM-HH:MM"); s != nil {
		l.Schedule = *s
	}
	l.Action, _ = o.Enum("action", "notify", "overlay", "block")
	return l, o.done()
}

func ParseID(params json.RawMessage) (int64, error) {
	o, err := parseObject(params)
	if err != nil {
		return 0, err
	}
	return o.Int("id", IntRule{}), o.done()
}

func ParseSettingsPatch(params json.RawMessage) (SettingsPatch, error) {
	o, err := parseObject(params)
	if err != nil {
		return SettingsPatch{}, err
	}
	var p SettingsPatch
	optBool := func(key string) *bool {
		if !o.Has(key) {
			o.seen[key] = true
			return nil
		}
		v, ok := o.Bool(key, true, false)
		if !ok {
			return nil
		}
		return &v
	}
	optInt := func(key string, min, max int64) *int64 {
		return o.OptInt(key, IntRule{Min: bound(min), Max: bound(max)})
	}
	optClock := func(key string) *string { return o.OptString(key, 0, clockRe, "expected HH:MM") }

	p.CaptureTitles = optBool("captureTitles")
	p.IdleThresholdMinutes = optInt("idleThresholdMinutes", 1, 60)
	p.BreakRemindersEnabled = optBool("breakRemindersEnabled")
	p.BreakEveryMinutes = optInt("breakEveryMinutes", 5, 240)
	p.BreakLengthMinutes = optInt("breakLengthMinutes", 1, 60)
	p.DowntimeEnabled = optBool("downtimeEnabled")
	p.DowntimeStart = optClock("downtimeStart")
	p.DowntimeEnd = optClock("downtimeEnd")
	p.OnboardingDone = optBool("onboardingDone")
	o.strict()
	return p, o.done()
}

// ParseMinutes validates tracker.pause and focus.start: 1 to 1440 minutes.
func ParseMinutes(params json.RawMessage) (int64, error) {
	o, err := parseObject(params)
	if err != nil {
		return 0, err
	}
	return o.Int("minutes", IntRule{Positive: true, Max: bound(maxMinutes)}), o.done()
}

func ParseSetAppCategory(params json.RawMessage) (appID string, categoryID *int64, err error) {
	o, verr := parseObject(params)
	if verr != nil {
		return "", nil, verr
	}
	appID, _ = o.String("appId")
	categoryID, _ = o.NullableInt("categoryId")
	return appID, categoryID, o.done()
}

func ParseDataExport(params json.RawMessage) (DataExportRequest, error) {
	o, err := parseObject(params)
	if err != nil {
		return DataExportRequest{}, err
	}
	var r DataExportRequest
	r.Format, _ = o.Enum("format", "csv", "json")
	r.From = o.OptInt("from", IntRule{})
	r.To = o.OptInt("to", IntRule{})
	return r, o.done()
}

// ParseDataWipe returns whether to reset everything; params are required
// even though `everything` defaults to false.
func ParseDataWipe(params json.RawMessage) (bool, error) {
	o, err := parseObject(params)
	if err != nil {
		return false, err
	}
	everything, _ := o.Bool("everything", false, false)
	return everything, o.done()
}

func ParseBrowserActiveTab(params json.RawMessage) (BrowserActiveTab, error) {
	o, err := parseObject(params)
	if err != nil {
		return BrowserActiveTab{}, err
	}
	var t BrowserActiveTab
	t.Domain = o.OptString("domain", 253, nil, "")
	t.Active, _ = o.Bool("active", true, false)
	return t, o.done()
}
