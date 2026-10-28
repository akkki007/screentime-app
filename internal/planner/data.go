package planner

import (
	"database/sql"
	"sort"

	"github.com/akkki007/screentime-app/internal/categories"
	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/queries"
	"github.com/akkki007/screentime-app/internal/timeutil"
)

// What the model may see (ADR 8, ADR 5): per-app and per-category totals for
// the last 7 days and today, the app limits, an app's display name and its
// .desktop Comment and Categories, and the user's goal. Window titles and
// browser hostnames are never read here: this file does not touch the
// sessions' title column or the web_sessions table, and domain limits are
// left out because their target is a hostname.

const topApps = 20

type appRow struct {
	AppID             string   `json:"appId"`
	Name              string   `json:"name,omitempty"`
	Category          string   `json:"category"`
	Description       string   `json:"description,omitempty"`
	DesktopCategories []string `json:"desktopCategories,omitempty"`
	TodayMinutes      int64    `json:"todayMinutes"`
	WeekMinutes       int64    `json:"last7DaysMinutes"`
}

type categoryRow struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"` // productive | distracting | neutral
	TodayMinutes int64  `json:"todayMinutes"`
	WeekMinutes  int64  `json:"last7DaysMinutes"`
}

type limitRow struct {
	TargetType   string `json:"targetType"` // app | category
	Target       string `json:"target"`
	DailyMinutes int64  `json:"dailyMinutes"`
}

// aggregates is both the prompt data and the set of things a suggestion may
// point at.
type aggregates struct {
	Date       string        `json:"date"`
	Goal       string        `json:"goal,omitempty"`
	Apps       []appRow      `json:"apps"`
	Categories []categoryRow `json:"categories"`
	Limits     []limitRow    `json:"limits"`

	known  map[string]bool  // every app ID in the database
	catIDs map[string]int64 // category name to ID
}

func minutes(ms int64) int64 { return (ms + 30_000) / 60_000 }

func (p *Planner) gather(goal string) (*aggregates, error) {
	db := p.cfg.DB
	now := p.cfg.Now()
	today := timeutil.StartOfLocalDay(now)
	weekFrom := timeutil.AddLocalDays(today, -6)

	sum := func(from int64, by string) (map[string]int64, error) {
		rows, err := queries.UsageSummary(db, ipc.UsageSummaryRequest{From: from, To: now, GroupBy: by})
		if err != nil {
			return nil, err
		}
		m := make(map[string]int64, len(rows))
		for _, r := range rows {
			m[r.Key] = r.Ms
		}
		return m, nil
	}
	appToday, err := sum(today, "app")
	if err != nil {
		return nil, err
	}
	appWeek, err := sum(weekFrom, "app")
	if err != nil {
		return nil, err
	}
	catToday, err := sum(today, "category")
	if err != nil {
		return nil, err
	}
	catWeek, err := sum(weekFrom, "category")
	if err != nil {
		return nil, err
	}

	a := &aggregates{
		Date:       timeutil.LocalDateKey(now),
		Goal:       cleanText(goal, ipc.MaxGoalChars),
		Apps:       []appRow{},
		Categories: []categoryRow{},
		Limits:     []limitRow{},
		known:      map[string]bool{},
		catIDs:     map[string]int64{},
	}

	cats, err := queries.ListCategories(db)
	if err != nil {
		return nil, err
	}
	for _, c := range cats {
		a.catIDs[c.Name] = c.ID
		kind := "neutral"
		if c.Productive != nil {
			kind = map[int64]string{1: "productive", 0: "distracting"}[*c.Productive]
			if kind == "" {
				kind = "neutral"
			}
		}
		a.Categories = append(a.Categories, categoryRow{
			Name: cleanText(c.Name, 60), Kind: kind,
			TodayMinutes: minutes(catToday[c.Name]), WeekMinutes: minutes(catWeek[c.Name]),
		})
	}

	meta, err := appMeta(db)
	if err != nil {
		return nil, err
	}
	for id := range meta {
		a.known[id] = true
	}
	ids := make([]string, 0, len(appWeek))
	for id := range appWeek {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if appWeek[ids[i]] != appWeek[ids[j]] {
			return appWeek[ids[i]] > appWeek[ids[j]]
		}
		return ids[i] < ids[j]
	})
	if len(ids) > topApps {
		ids = ids[:topApps]
	}
	for _, id := range ids {
		m := meta[id]
		row := appRow{
			AppID: id, Name: cleanText(m.name, 60), Category: cleanText(m.category, 60),
			TodayMinutes: minutes(appToday[id]), WeekMinutes: minutes(appWeek[id]),
		}
		if e, ok := p.cfg.Lookup(id); ok {
			row.Description = cleanText(e.Comment, 120)
			for _, c := range e.Categories {
				if c = cleanText(c, 30); c != "" && len(row.DesktopCategories) < 6 {
					row.DesktopCategories = append(row.DesktopCategories, c)
				}
			}
		}
		a.Apps = append(a.Apps, row)
	}

	limits, err := queries.ListLimits(db)
	if err != nil {
		return nil, err
	}
	for _, l := range limits {
		if l.TargetType == "domain" {
			continue // a hostname
		}
		a.Limits = append(a.Limits, limitRow{
			TargetType: l.TargetType, Target: cleanText(l.Target, 100), DailyMinutes: minutes(l.DailyMs),
		})
	}
	return a, nil
}

type appMetaRow struct{ name, category string }

func appMeta(db *sql.DB) (map[string]appMetaRow, error) {
	rows, err := db.Query(`SELECT apps.app_id, COALESCE(apps.name, ''), COALESCE(categories.name, 'Other')
		FROM apps LEFT JOIN categories ON categories.id = apps.category_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]appMetaRow{}
	for rows.Next() {
		var id string
		var m appMetaRow
		if err := rows.Scan(&id, &m.name, &m.category); err != nil {
			return nil, err
		}
		out[id] = m
	}
	return out, rows.Err()
}

// maxCandidates is how many apps one categorize call asks about.
const maxCandidates = ipc.MaxCategorize

type candidate struct {
	AppID       string   `json:"appId"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Desktop     []string `json:"desktopCategories,omitempty"`
}

// categorizeInput is what a categorize call asks about and may answer with.
type categorizeInput struct {
	apps   []candidate
	byID   map[string]bool
	catIDs map[string]int64 // every category but Other
}

// candidates are the apps whose category the model may decide: automatic
// (never chosen by the user, nor by an earlier model run) and still Other or
// unset. Rules come first: an app that the substring rules or the .desktop
// Categories can place is left to them, and the model never sees it.
func (p *Planner) candidates(only []string) (*categorizeInput, error) {
	rows, err := p.cfg.DB.Query(`SELECT apps.app_id, COALESCE(apps.name, '')
		FROM apps LEFT JOIN categories ON categories.id = apps.category_id
		WHERE apps.category_source = 'auto' AND (apps.category_id IS NULL OR categories.name = 'Other')
		ORDER BY (SELECT COALESCE(SUM(end_ts - start_ts), 0) FROM sessions WHERE sessions.app_id = apps.id) DESC, apps.app_id`)
	if err != nil {
		return nil, err
	}
	type raw struct{ id, name string }
	var found []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.id, &r.name); err != nil {
			rows.Close()
			return nil, err
		}
		found = append(found, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	want := map[string]bool{}
	for _, id := range only {
		want[id] = true
	}
	in := &categorizeInput{byID: map[string]bool{}, catIDs: map[string]int64{}}
	for _, r := range found {
		if len(in.apps) >= maxCandidates {
			break
		}
		if len(want) > 0 && !want[r.id] {
			continue
		}
		e, _ := p.cfg.Lookup(r.id)
		if categories.DefaultFor(r.id) != "" || categories.FromDesktop(e.Categories) != "" {
			continue
		}
		c := candidate{AppID: r.id, Name: cleanText(r.name, 60), Description: cleanText(e.Comment, 120)}
		for _, dc := range e.Categories {
			if dc = cleanText(dc, 30); dc != "" && len(c.Desktop) < 6 {
				c.Desktop = append(c.Desktop, dc)
			}
		}
		in.apps = append(in.apps, c)
		in.byID[r.id] = true
	}
	if len(in.apps) == 0 {
		return in, nil
	}
	cats, err := queries.ListCategories(p.cfg.DB)
	if err != nil {
		return nil, err
	}
	for _, c := range cats {
		if c.Name != "Other" {
			in.catIDs[c.Name] = c.ID
		}
	}
	return in, nil
}

// apply stores the model's categories. The UPDATE repeats the eligibility
// test, so an app the user categorised after the candidates were read keeps
// their choice: nothing here can overwrite a 'user' row.
func (p *Planner) apply(answers []ipc.PlannerCategorization) ([]ipc.PlannerCategorization, error) {
	out := []ipc.PlannerCategorization{}
	if len(answers) == 0 {
		return out, nil
	}
	tx, err := p.cfg.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, a := range answers {
		res, err := tx.Exec(`UPDATE apps SET category_id = ?, category_source = 'model'
			WHERE app_id = ? AND category_source = 'auto'
			AND (category_id IS NULL OR category_id = (SELECT id FROM categories WHERE name = 'Other'))`,
			a.CategoryID, a.AppID)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			out = append(out, a)
		}
	}
	return out, tx.Commit()
}
