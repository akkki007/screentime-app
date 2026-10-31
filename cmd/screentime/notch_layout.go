//go:build gtk3

package main

import "strings"

// The notch's geometry and colours (ADR 14). Everything here is plain Go so it
// can be tested without GTK; notch_gtk.go measures text and draws, this file
// decides where things go. Sizes are logical pixels: GTK scales the cairo
// context for HiDPI, so nothing here knows the scale factor.
const (
	pillH      = 34.0 // collapsed height
	shoulder   = 10.0 // radius of the concave shoulders at the top edge
	cornerR    = 14.0 // radius of the rounded bottom corners
	pillPadX   = 14.0 // space left and right of the pill's content
	iconSize   = 14.0
	iconGap    = 8.0 // icon to text, text to detail
	pillMinW   = 112.0
	pillMaxW   = 420.0
	panelW     = 320.0
	panelPad   = 16.0
	headerH    = 54.0 // "Today" and the total
	appRowH    = 30.0
	btnH       = 32.0
	btnGap     = 8.0
	panelFootH = 14.0 // space under the last button

	animMs = 180 // width and height ease

	// Colours, as 0xRRGGBB.
	colBg      = 0x0c0a09
	colText    = 0xffffff
	colDim     = 0xa8a29e
	colAccent  = 0xff8f45
	colSurface = 0x1c1917
	colTrack   = 0x292524
)

// rect is a rectangle in the window's coordinates.
type rect struct{ x, y, w, h float64 }

func (r rect) contains(x, y float64) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// hit is what a click landed on.
type hit int

const (
	hitNone hit = iota
	hitPill
	hitPause
	hitFocus
	hitOpen
)

// layout is where the panel's parts sit for a window of width w with napps
// rows. Row and button positions are shared by the drawing and the hit test.
type layout struct {
	w, h    float64
	header  rect
	rows    []rect
	pause   rect
	focus   rect
	open    rect
	pillBox rect // the pill row, in the same coordinates
}

// pillWidth is the pill's width for its content: icon, text and an optional
// detail, each as measured. It is clamped to what fits on a small screen.
func pillWidth(textW, detailW float64) float64 {
	w := 2*pillPadX + iconSize + iconGap + textW
	if detailW > 0 {
		w += iconGap + detailW
	}
	return clamp(w, pillMinW, pillMaxW)
}

// textRoom is the width left for text and detail together in a pill of width w.
func textRoom(w float64) float64 { return w - 2*pillPadX - iconSize - iconGap }

// panelHeight is the height of the whole notch with the panel open, for n app
// rows (an empty list still takes one row, for "No activity yet").
func panelHeight(n int) float64 {
	if n < 1 {
		n = 1
	}
	return pillH + headerH + float64(n)*appRowH + 10 + 2*btnH + btnGap + panelFootH
}

// notchSize is the notch window's size: the pill (or the panel when open) plus a
// shoulder on each side. The shoulders are the concave flares at the top.
func notchSize(pillW float64, open bool, napps int) (w, h float64) {
	if !open {
		return pillW + 2*shoulder, pillH
	}
	return max(pillW, panelW) + 2*shoulder, panelHeight(napps)
}

// panelLayout places the panel's parts in a window of width w.
func panelLayout(w float64, napps int) layout {
	if napps < 1 {
		napps = 1
	}
	body := w - 2*shoulder // the pill's own width
	l := layout{w: w, h: panelHeight(napps), pillBox: rect{shoulder, 0, body, pillH}}
	x := shoulder + panelPad
	inner := body - 2*panelPad
	y := pillH
	l.header = rect{x, y, inner, headerH}
	y += headerH
	for i := 0; i < napps; i++ {
		l.rows = append(l.rows, rect{x, y, inner, appRowH})
		y += appRowH
	}
	y += 10
	half := (inner - btnGap) / 2
	l.pause = rect{x, y, half, btnH}
	l.focus = rect{x + half + btnGap, y, half, btnH}
	y += btnH + btnGap
	l.open = rect{x, y, inner, btnH}
	return l
}

// hitTest says what a click at (x, y) landed on. With the panel closed only
// the pill answers.
func (l layout) hitTest(open bool, x, y float64) hit {
	switch {
	case l.pillBox.contains(x, y):
		return hitPill
	case !open:
		return hitNone
	case l.pause.contains(x, y):
		return hitPause
	case l.focus.contains(x, y):
		return hitFocus
	case l.open.contains(x, y):
		return hitOpen
	}
	return hitNone
}

// ease is a cubic ease-out for t in [0, 1].
func ease(t float64) float64 {
	t = clamp(t, 0, 1)
	u := 1 - t
	return 1 - u*u*u
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func clamp(v, lo, hi float64) float64 { return min(max(v, lo), hi) }

// animate is the size at elapsed ms of an animation from one size to another,
// and whether it has finished.
func animate(fromW, fromH, toW, toH float64, elapsed int64) (w, h float64, done bool) {
	if elapsed >= animMs {
		return toW, toH, true
	}
	t := ease(float64(elapsed) / animMs)
	return lerp(fromW, toW, t), lerp(fromH, toH, t), false
}

// truncate shortens s with an ellipsis until measure says it fits in max
// pixels. measure is the text's rendered width.
func truncate(measure func(string) float64, s string, max float64) string {
	if measure(s) <= max {
		return s
	}
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	for n := len(r) - 1; n > 0; n-- {
		cand := strings.TrimRight(string(r[:n]), " ") + "…"
		if measure(cand) <= max {
			return cand
		}
	}
	return "…"
}

// stableDigits is s with every digit replaced by 0, to measure a countdown at
// its widest so the pill doesn't flutter as the seconds change.
func stableDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return '0'
		}
		return r
	}, s)
}

// focusProgress is the share of a focus session that has passed, given when
// it ends, how long it was and now (Unix ms). It is 0 before it starts and 1
// at the end.
func focusProgress(until, total, now int64) float64 {
	if total <= 0 {
		return 0
	}
	return clamp(1-float64(until-now)/float64(total), 0, 1)
}

// placeX is the left edge for a window of width w centred on a monitor that
// starts at monX and is monW wide.
func placeX(monX, monW int, w float64) int {
	return monX + (monW-int(w+0.5))/2
}

// ownedByShell is whether this desktop draws the notch itself: the GNOME Shell
// extension does (ADR 14), so the tray process must not draw a second one.
// desktop is XDG_CURRENT_DESKTOP, a colon separated list such as
// "ubuntu:GNOME".
func ownedByShell(desktop string) bool {
	for _, d := range strings.Split(desktop, ":") {
		if strings.EqualFold(strings.TrimSpace(d), "GNOME") {
			return true
		}
	}
	return false
}
