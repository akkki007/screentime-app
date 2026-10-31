//go:build gtk3

package main

import (
	"math"
	"strings"
	"testing"
)

func TestOwnedByShell(t *testing.T) {
	for desktop, want := range map[string]bool{
		"GNOME":               true,
		"ubuntu:GNOME":        true,
		"gnome":               true,
		"X-Cinnamon":          false,
		"KDE":                 false,
		"sway":                false,
		"":                    false,
		"GNOME-Classic:GNOME": true,
	} {
		if got := ownedByShell(desktop); got != want {
			t.Errorf("ownedByShell(%q) = %v, want %v", desktop, got, want)
		}
	}
}

func TestPillWidthClamps(t *testing.T) {
	if got := pillWidth(10, 0); got != pillMinW {
		t.Errorf("short text: %v, want the minimum %v", got, pillMinW)
	}
	if got := pillWidth(1000, 0); got != pillMaxW {
		t.Errorf("long text: %v, want the maximum %v", got, pillMaxW)
	}
	plain := pillWidth(80, 0)
	if want := 2*pillPadX + iconSize + iconGap + 80; plain != want {
		t.Errorf("plain: %v, want %v", plain, want)
	}
	if got := pillWidth(80, 40); got != plain+iconGap+40 {
		t.Errorf("with detail: %v, want %v", got, plain+iconGap+40)
	}
}

func TestSize(t *testing.T) {
	w, h := notchSize(150, false, 0)
	if w != 150+2*shoulder || h != pillH {
		t.Errorf("collapsed: %v x %v", w, h)
	}
	w, h = notchSize(150, true, 4)
	if w != panelW+2*shoulder || h != panelHeight(4) {
		t.Errorf("open: %v x %v", w, h)
	}
	w, _ = notchSize(400, true, 4)
	if w != 400+2*shoulder {
		t.Errorf("a wide pill keeps its width when open: %v", w)
	}
	if panelHeight(0) != panelHeight(1) {
		t.Error("an empty list takes one row")
	}
	if panelHeight(4) <= panelHeight(1) {
		t.Error("more apps, taller panel")
	}
}

func TestPanelLayoutStaysInside(t *testing.T) {
	for n := 0; n <= 4; n++ {
		w, h := notchSize(150, true, n)
		l := panelLayout(w, n)
		if l.h != h {
			t.Errorf("n=%d: layout height %v, size height %v", n, l.h, h)
		}
		all := append([]rect{l.header, l.pause, l.focus, l.open}, l.rows...)
		for i, r := range all {
			if r.x < shoulder || r.x+r.w > w-shoulder+1e-9 || r.y < pillH || r.y+r.h > h {
				t.Errorf("n=%d: rect %d %+v outside the panel body (%v x %v)", n, i, r, w, h)
			}
		}
		if l.pause.x+l.pause.w > l.focus.x || l.pause.y+l.pause.h > l.open.y {
			t.Errorf("n=%d: buttons overlap: %+v %+v %+v", n, l.pause, l.focus, l.open)
		}
	}
}

func TestHitTest(t *testing.T) {
	w, _ := notchSize(150, true, 3)
	l := panelLayout(w, 3)
	centre := func(r rect) (float64, float64) { return r.x + r.w/2, r.y + r.h/2 }
	for _, c := range []struct {
		name string
		r    rect
		want hit
	}{
		{"pill", l.pillBox, hitPill},
		{"pause", l.pause, hitPause},
		{"focus", l.focus, hitFocus},
		{"open", l.open, hitOpen},
		{"header", l.header, hitNone},
		{"row", l.rows[0], hitNone},
	} {
		x, y := centre(c.r)
		if got := l.hitTest(true, x, y); got != c.want {
			t.Errorf("%s open: %v, want %v", c.name, got, c.want)
		}
	}
	// Closed, only the pill answers, even where a button was.
	x, y := centre(l.pause)
	if got := l.hitTest(false, x, y); got != hitNone {
		t.Errorf("closed, over the pause button: %v", got)
	}
	x, y = centre(l.pillBox)
	if got := l.hitTest(false, x, y); got != hitPill {
		t.Errorf("closed, over the pill: %v", got)
	}
	// The shoulders and everything outside are not the pill.
	for _, p := range [][2]float64{{2, 5}, {w - 2, 5}, {-5, 5}, {w + 5, 5}, {w / 2, -1}} {
		if got := l.hitTest(true, p[0], p[1]); got != hitNone {
			t.Errorf("(%v, %v): %v, want none", p[0], p[1], got)
		}
	}
	// The bottom edge of the pill belongs to the panel below it.
	if got := l.hitTest(true, w/2, pillH); got == hitPill {
		t.Error("y == pillH is below the pill")
	}
}

func TestEaseAndAnimate(t *testing.T) {
	if ease(0) != 0 || ease(1) != 1 || ease(-3) != 0 || ease(9) != 1 {
		t.Error("ease must run from 0 to 1 and clamp")
	}
	prev := 0.0
	for i := 1; i <= 10; i++ {
		v := ease(float64(i) / 10)
		if v <= prev {
			t.Fatalf("ease not increasing at %d: %v after %v", i, v, prev)
		}
		prev = v
	}
	w, h, done := animate(100, 34, 300, 200, 0)
	if w != 100 || h != 34 || done {
		t.Errorf("start: %v %v %v", w, h, done)
	}
	w, h, done = animate(100, 34, 300, 200, animMs/2)
	if w <= 100 || w >= 300 || h <= 34 || h >= 200 || done {
		t.Errorf("middle: %v %v %v", w, h, done)
	}
	if w, h, done = animate(100, 34, 300, 200, animMs); w != 300 || h != 200 || !done {
		t.Errorf("end: %v %v %v", w, h, done)
	}
}

// fakeWidth measures 7 px a character, so the tests don't need Pango.
func fakeWidth(s string) float64 { return 7 * float64(len([]rune(s))) }

func TestTruncate(t *testing.T) {
	if got := truncate(fakeWidth, "short", 100); got != "short" {
		t.Errorf("fits: %q", got)
	}
	got := truncate(fakeWidth, "a very long application name", 70)
	if fakeWidth(got) > 70 || !strings.HasSuffix(got, "…") || !strings.HasPrefix(got, "a very") {
		t.Errorf("long: %q (%v px)", got, fakeWidth(got))
	}
	if got := truncate(fakeWidth, "abc def ghi", 40); strings.Contains(got, " …") {
		t.Errorf("no space before the ellipsis: %q", got)
	}
	if got := truncate(fakeWidth, "anything", 0); got != "" {
		t.Errorf("no room: %q", got)
	}
	if got := truncate(fakeWidth, "anything", 3); got != "…" {
		t.Errorf("hardly any room: %q", got)
	}
	if got := truncate(fakeWidth, "日本語のアプリ名前", 35); fakeWidth(got) > 35 || !strings.HasSuffix(got, "…") {
		t.Errorf("multibyte: %q", got)
	}
}

func TestStableDigits(t *testing.T) {
	for in, want := range map[string]string{"24:13": "00:00", "1:05:00": "0:00:00", "Paused": "Paused"} {
		if got := stableDigits(in); got != want {
			t.Errorf("stableDigits(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFocusProgress(t *testing.T) {
	const total = 25 * 60_000
	for _, c := range []struct {
		until, now int64
		want       float64
	}{
		{total, 0, 0},
		{total, total / 2, 0.5},
		{total, total, 1},
		{total, total * 2, 1}, // past the end
		{total, -total, 0},    // before the start
	} {
		if got := focusProgress(c.until, total, c.now); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("until=%d now=%d: %v, want %v", c.until, c.now, got, c.want)
		}
	}
	if focusProgress(10, 0, 5) != 0 {
		t.Error("zero length session has no progress")
	}
}

func TestPlaceX(t *testing.T) {
	if got := placeX(0, 1280, 200); got != 540 {
		t.Errorf("single monitor: %d", got)
	}
	if got := placeX(1920, 1280, 200); got != 1920+540 {
		t.Errorf("second monitor: %d", got)
	}
	if a, b := placeX(0, 1000, 301), placeX(0, 1000, 300); a < 349 || b != 350 {
		t.Errorf("odd widths round sensibly: %d %d", a, b)
	}
}

func TestColours(t *testing.T) {
	if colBg != 0x0c0a09 || colAccent != 0xff8f45 || colText != 0xffffff {
		t.Error("the notch's colours are fixed by ADR 14")
	}
	if pillH != 34 {
		t.Error("the collapsed pill is 34 px tall")
	}
	if shoulder+cornerR > pillH {
		t.Error("shoulder and corner must fit in the collapsed pill")
	}
}
