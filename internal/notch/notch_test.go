package notch

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/akkki007/screentime-app/internal/ipc"
)

// The fixtures are shared with adapters/gnome-extension/notch.test.js: the
// Shell's notch and every other desktop's must agree on each step.
type fixtures struct {
	FormatShort [][2]json.RawMessage `json:"formatShort"`
	Countdown   [][2]json.RawMessage `json:"countdown"`
	Cases       []struct {
		Name  string  `json:"name"`
		Opts  Options `json:"opts"`
		Steps []struct {
			At        int64              `json:"at"`
			Connected *bool              `json:"connected"`
			Status    *ipc.TrackerStatus `json:"status"`
			Today     []App              `json:"today"`
			LimitHit  *struct {
				Target  string `json:"target"`
				DailyMs int64  `json:"dailyMs"`
			} `json:"limitHit"`
			Reminder *struct {
				Kind    string `json:"kind"`
				Message string `json:"message"`
			} `json:"reminder"`
			Expect      *View  `json:"expect"`
			ExpectPanel *Panel `json:"expectPanel"`
			ExpectNext  *int64 `json:"expectNext"`
		} `json:"steps"`
	} `json:"cases"`
}

func load(t *testing.T) fixtures {
	t.Helper()
	raw, err := os.ReadFile("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixtures
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFormats(t *testing.T) {
	f := load(t)
	check := func(name string, table [][2]json.RawMessage, fn func(int64) string) {
		for _, row := range table {
			var in int64
			var want string
			if json.Unmarshal(row[0], &in) != nil || json.Unmarshal(row[1], &want) != nil {
				t.Fatalf("%s: bad row %s", name, row)
			}
			if got := fn(in); got != want {
				t.Errorf("%s(%d) = %q, want %q", name, in, got, want)
			}
		}
	}
	check("FormatShort", f.FormatShort, FormatShort)
	check("Countdown", f.Countdown, Countdown)
}

func TestFixtures(t *testing.T) {
	f := load(t)
	if len(f.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range f.Cases {
		t.Run(c.Name, func(t *testing.T) {
			m := New(c.Opts)
			for i, s := range c.Steps {
				switch {
				case s.Connected != nil:
					m.Connected(*s.Connected)
				case s.Status != nil:
					m.Status(*s.Status, s.At)
				case s.Today != nil:
					m.Today(s.Today)
				case s.LimitHit != nil:
					m.LimitHit(s.LimitHit.Target, s.LimitHit.DailyMs, s.At)
				case s.Reminder != nil:
					m.Reminder(s.Reminder.Kind, s.Reminder.Message, s.At)
				case s.Expect != nil:
					if got := m.View(s.At); !reflect.DeepEqual(got, *s.Expect) {
						t.Errorf("step %d: view = %+v, want %+v", i, got, *s.Expect)
					}
				case s.ExpectPanel != nil:
					if got := m.Panel(); !reflect.DeepEqual(got, *s.ExpectPanel) {
						t.Errorf("step %d: panel = %+v, want %+v", i, got, *s.ExpectPanel)
					}
				case s.ExpectNext != nil:
					if got := m.NextChange(s.At); got != *s.ExpectNext {
						t.Errorf("step %d: next change = %d, want %d", i, got, *s.ExpectNext)
					}
				default:
					t.Fatalf("step %d does nothing", i)
				}
			}
		})
	}
}
