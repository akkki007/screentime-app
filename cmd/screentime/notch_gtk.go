//go:build gtk3

package main

/*
#cgo pkg-config: gtk+-3.0
#cgo LDFLAGS: -ldl -lm
#include <stdlib.h>
#include "notch_gtk.h"
*/
import "C"

import (
	"log"
	"os"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/akkki007/screentime-app/internal/notch"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// The Linux notch outside GNOME (ADR 14): a small GTK window in the tray
// process, drawn with cairo and Pango in notch_gtk.c. This file is its
// controller. What the pill shows comes from notch.Driver; where things go is
// notch_layout.go. Everything below runs on the GTK main thread, reached
// through application.InvokeAsync, so none of it needs a lock.

// focusSession is how long Focus 25 min lasts: the progress bar's length when
// the tray first sees a session that is already running.
const focusSession = 25 * 60_000

// ignoreFocusOut is how long after opening the panel focus-out is ignored,
// while the window manager is still sorting out focus.
const ignoreFocusOut = 400 * time.Millisecond

type notchUI struct {
	drv      *notch.Driver
	openMain func()

	created, dead bool
	visible, open bool
	cur, from, to [2]float64 // window size now, and the animation's ends
	animating     bool
	animStart     int64 // first tick's frame time, in us; 0 until it comes
	lay           layout
	openedAt      time.Time
	focusUntil    int64 // the focus session being timed, and its length
	focusTotal    int64
}

// theNotch is what the exported C callbacks reach.
var theNotch atomic.Pointer[notchUI]

// startNotch draws the notch unless the GNOME Shell does. Call it once GTK is
// running (after the application has started). It returns nil when there is
// nothing to stop.
func startNotch(openMain func()) *notchUI {
	if ownedByShell(os.Getenv("XDG_CURRENT_DESKTOP")) {
		return nil // the Screentime Shell extension draws it
	}
	n := &notchUI{openMain: openMain}
	theNotch.Store(n)
	n.drv = notch.NewDriver(func() { application.InvokeAsync(n.refresh) })
	n.drv.Start()
	return n
}

// Stop disconnects the notch from the daemon.
func (n *notchUI) Stop() {
	if n != nil {
		n.drv.Stop()
	}
}

// maxW and maxH are the largest the notch gets: a wide pill, or the panel
// with all its rows.
var maxW, maxH = max(pillMaxW, panelW) + 2*shoulder, panelHeight(4)

// create makes the window. It exists only while the notch is enabled, so
// turning it off gives the memory back.
func (n *notchUI) create() {
	flags := C.nt_create(C.double(maxW), C.double(maxH))
	if flags < 0 {
		n.dead = true
		log.Printf("[screentime] notch: no display")
		return
	}
	mode := "dock window"
	if flags&C.NT_LAYER_SHELL != 0 {
		mode = "layer-shell surface"
	}
	log.Printf("[screentime] notch: %s", mode)
	n.created = true
}

func (n *notchUI) destroy() {
	C.nt_destroy()
	*n = notchUI{drv: n.drv, openMain: n.openMain}
}

// measure returns a function that measures text at a size in px.
func measure(px float64, bold bool) func(string) float64 {
	b := C.int(0)
	if bold {
		b = 1
	}
	return func(s string) float64 {
		cs := C.CString(s)
		defer C.free(unsafe.Pointer(cs))
		return float64(C.nt_text_width(cs, C.double(px), b))
	}
}

func setStr(dst **C.char, s string) {
	cs := C.CString(s)
	C.nt_setstr(dst, cs)
	C.free(unsafe.Pointer(cs))
}

func crect(r rect) C.nt_rect {
	return C.nt_rect{x: C.double(r.x), y: C.double(r.y), w: C.double(r.w), h: C.double(r.h)}
}

func boolInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// refresh reads the driver and redraws: new text, a new size to ease to.
func (n *notchUI) refresh() {
	if n.dead {
		return
	}
	if !n.drv.Enabled() {
		if n.created {
			n.destroy()
		}
		return
	}
	if !n.created {
		if n.create(); n.dead {
			return
		}
	}
	v := n.drv.View()
	var p notch.Panel
	if n.open {
		p = n.drv.Panel()
	}
	sc := &C.nt_scene

	// The pill: icon, text, detail, clipped to what a small screen can hold.
	mText, mDetail := measure(13, true), measure(12, false)
	room := textRoom(pillMaxW)
	text := truncate(mText, v.Text, room)
	textW := mText(text)
	if v.Kind == "focus" {
		textW = mText(stableDigits(text)) // so the pill doesn't flutter as seconds change
	}
	detail, detailW := v.Detail, 0.0
	if avail := room - textW - iconGap; avail > 20 && detail != "" {
		detail = truncate(mDetail, detail, avail)
		detailW = mDetail(detail)
	} else {
		detail = ""
	}
	setStr(&sc.icon, v.Icon)
	setStr(&sc.text, text)
	setStr(&sc.detail, detail)
	sc.progress = -1
	if v.Kind == "focus" && v.CountdownTo != nil {
		sc.progress = C.double(n.progress(*v.CountdownTo))
	} else {
		n.focusUntil = 0
	}

	// The panel, when open.
	apps := p.Apps
	if len(apps) > len(sc.app_name) {
		apps = apps[:len(sc.app_name)]
	}
	w, h := notchSize(pillWidth(textW, detailW), n.open, len(apps))
	n.lay = panelLayout(w, len(apps))
	sc.panel = 0
	sc.napps = C.int(len(apps))
	if n.open {
		sc.panel = 1
		setStr(&sc.total, p.Total)
		mTime, mName := measure(12, false), measure(13, false)
		for i, a := range apps {
			row := n.lay.rows[i]
			timeW := mTime(a.Time)
			setStr(&sc.app_time[i], a.Time)
			setStr(&sc.app_name[i], truncate(mName, a.Name, row.w-timeW-12))
			sc.app_pct[i] = C.int(a.Percent)
		}
		for i, r := range n.lay.rows {
			if i < len(sc.rows) {
				sc.rows[i] = crect(r)
			}
		}
		sc.header = crect(n.lay.header)
		pause, focus := "Pause 15 min", "Focus 25 min"
		if p.Paused {
			pause = "Resume"
		}
		if p.Focus {
			focus = "Stop focus"
		}
		for i, r := range []rect{n.lay.pause, n.lay.focus, n.lay.open} {
			sc.btns[i] = crect(r)
		}
		setStr(&sc.btn[0], pause)
		setStr(&sc.btn[1], focus)
		setStr(&sc.btn[2], "Open Screentime")
	}

	n.resize(w, h)
	if !n.visible {
		n.visible = true
		C.nt_show(1)
	}
	C.nt_redraw()
}

// progress is how much of the focus session ending at until (Unix ms) has
// passed. A session is timed from the first time it is seen.
func (n *notchUI) progress(until int64) float64 {
	now := time.Now().UnixMilli()
	if until != n.focusUntil {
		n.focusUntil = until
		n.focusTotal = max(until-now, focusSession)
	}
	return focusProgress(until, n.focusTotal, now)
}

// resize eases the window to w x h, or jumps there if the desktop has
// animations off or this is the first time.
func (n *notchUI) resize(w, h float64) {
	target := [2]float64{w, h}
	if n.cur == [2]float64{} || C.nt_animations() == 0 {
		n.cur, n.to, n.animating = target, target, false
		C.nt_apply(C.double(w), C.double(h))
		return
	}
	if target == n.to {
		return
	}
	n.from, n.to = n.cur, target
	n.animStart = 0
	n.animating = true
	C.nt_tick(1)
}

func (n *notchUI) setOpen(open bool) {
	if n.open == open {
		return
	}
	n.open = open
	if open {
		n.openedAt = time.Now()
	}
	C.nt_set_open(boolInt(open))
	n.refresh()
}

// click handles a button press at (x, y) in the window. While the panel is
// open on X11 the notch holds a grab, so presses elsewhere arrive here too,
// outside the window or on its transparent shoulders: they close the panel.
func (n *notchUI) click(x, y float64) {
	switch n.lay.hitTest(n.open, x, y) {
	case hitPill:
		n.setOpen(!n.open)
	case hitPause:
		go n.act("pause", n.drv.TogglePause)
	case hitFocus:
		go n.act("focus", n.drv.ToggleFocus)
	case hitOpen:
		n.setOpen(false)
		go n.openMain()
	default:
		if n.open && !(rect{shoulder, 0, n.cur[0] - 2*shoulder, n.cur[1]}).contains(x, y) {
			n.setOpen(false)
		}
	}
}

func (n *notchUI) act(what string, f func() error) {
	if err := f(); err != nil {
		log.Printf("[notch] %s failed: %v", what, err)
	}
}

//export ntEvent
func ntEvent(kind C.int, x, y C.double) {
	n := theNotch.Load()
	if n == nil {
		return
	}
	switch kind {
	case C.NT_EV_CLICK:
		n.click(float64(x), float64(y))
	case C.NT_EV_ESCAPE:
		n.setOpen(false)
	case C.NT_EV_FOCUS_OUT:
		if n.open && time.Since(n.openedAt) > ignoreFocusOut {
			n.setOpen(false)
		}
	}
}

//export ntTick
func ntTick(frameUs C.longlong) C.int {
	n := theNotch.Load()
	if n == nil || !n.animating {
		return 0
	}
	if n.animStart == 0 {
		n.animStart = int64(frameUs)
	}
	w, h, done := animate(n.from[0], n.from[1], n.to[0], n.to[1], (int64(frameUs)-n.animStart)/1000)
	n.cur = [2]float64{w, h}
	C.nt_apply(C.double(w), C.double(h))
	if done {
		n.animating = false
		return 0
	}
	return 1
}
