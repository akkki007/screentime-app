//go:build windows

package focus

import (
	"context"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows reads focus from a WinEvent hook on EVENT_SYSTEM_FOREGROUND and
// idleness from GetLastInputInfo. Pure Win32 through x/sys/windows: no cgo.
//
// The app ID is the executable's base name without ".exe", lowercased
// (appIDFromExe). Titles are read (GetWindowTextW) only after
// SetCaptureTitles(true), the user's opt-in (ADR 5).
type Windows struct {
	Now func() int64
	// IdlePoll overrides how often idle time is checked (idlePollEvery).
	IdlePoll time.Duration

	captureTitles atomic.Bool
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSetWinEventHook   = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent    = user32.NewProc("UnhookWinEvent")
	procGetMessage        = user32.NewProc("GetMessageW")
	procPeekMessage       = user32.NewProc("PeekMessageW")
	procPostThreadMessage = user32.NewProc("PostThreadMessageW")
	procGetWindowText     = user32.NewProc("GetWindowTextW")
	procGetLastInputInfo  = user32.NewProc("GetLastInputInfo")
	procGetTickCount64    = kernel32.NewProc("GetTickCount64")
)

const (
	eventSystemForeground = 0x0003
	winEventOutOfContext  = 0x0000
	objidWindow           = 0
	wmQuit                = 0x0012
	wmUser                = 0x0400
	pmNoRemove            = 0x0000
)

// msg is the Win32 MSG; only its size matters, as nothing reads it.
type msg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type lastInputInfo struct {
	size uint32
	time uint32
}

func (w *Windows) ID() string { return "windows" }

// Available whenever the Win32 entry points resolve, which they always do on
// a desktop Windows; the daemon must run in the user's interactive session
// (not session 0, where services live) to see any foreground window.
func (w *Windows) Available(context.Context) bool {
	return procSetWinEventHook.Find() == nil && procGetLastInputInfo.Find() == nil
}

func (w *Windows) SetCaptureTitles(capture bool) { w.captureTitles.Store(capture) }

func (w *Windows) now() int64 {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now().UnixMilli()
}

// A WinEvent callback gets no user data, only the hook handle, and a callback
// made with syscall.NewCallback is never freed (and the runtime caps how many
// there can be), so there is one for the process and it finds the
// subscription by hook.
var (
	winEventProc = sync.OnceValue(func() uintptr { return syscall.NewCallback(onWinEvent) })
	hooks        sync.Map // hook handle (uintptr) -> *winSub
)

func onWinEvent(hook, event, hwnd, idObject, idChild, thread, eventTime uintptr) uintptr {
	// idObject is a 32-bit LONG: its upper half in the register is undefined.
	if event != eventSystemForeground || int32(idObject) != objidWindow {
		return 0
	}
	if v, ok := hooks.Load(hook); ok {
		v.(*winSub).report(windows.HWND(hwnd))
	}
	return 0
}

// winSub is one OnFocusChange subscription. report runs only on the
// subscription's own message-loop thread.
type winSub struct {
	w       *Windows
	cb      func(Window)
	last    windows.HWND
	stopped atomic.Bool
}

// report delivers the foreground window once. hwnd is 0 while no window has
// the focus (switching, the secure desktop); nothing is reported then, so the
// previous app keeps the time until the next window takes focus.
func (s *winSub) report(hwnd windows.HWND) {
	if hwnd == 0 || hwnd == s.last || s.stopped.Load() {
		return
	}
	s.last = hwnd
	s.cb(s.w.describe(hwnd))
}

// describe reads a window's app, PID and (if opted in) title.
func (w *Windows) describe(hwnd windows.HWND) Window {
	win := Window{AppID: unknownApp, Ts: w.now()}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err == nil && pid != 0 {
		win.PID = pid
		if exe, err := exePath(pid); err == nil {
			win.AppID = appIDFromExe(exe)
		}
	}
	if w.captureTitles.Load() {
		var buf [512]uint16
		n, _, _ := procGetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		win.Title = windows.UTF16ToString(buf[:n])
	}
	return win
}

// exePath is the full image path of a process. PROCESS_QUERY_LIMITED_INFORMATION
// is enough even for most elevated processes; protected ones fail and are
// reported as "unknown".
func exePath(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	var buf [windows.MAX_PATH * 4]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}

// OnFocusChange reports the foreground window at once, then every change.
// SetWinEventHook with WINEVENT_OUTOFCONTEXT delivers its callbacks through
// the message loop of the thread that installed the hook, so that thread is
// locked and sits in GetMessage: it sleeps until Windows has an event, with no
// polling. Unsubscribing posts WM_QUIT to end the loop.
func (w *Windows) OnFocusChange(cb func(Window)) func() {
	sub := &winSub{w: w, cb: cb}
	threadID := make(chan uint32, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// Create this thread's message queue before anyone posts to it.
		var m msg
		procPeekMessage.Call(uintptr(unsafe.Pointer(&m)), 0, wmUser, wmUser, pmNoRemove)

		hook, _, err := procSetWinEventHook.Call(eventSystemForeground, eventSystemForeground,
			0, winEventProc(), 0, 0, winEventOutOfContext)
		if hook == 0 {
			log.Printf("[windows] SetWinEventHook: %v", err)
			threadID <- 0
			return
		}
		hooks.Store(hook, sub)
		threadID <- windows.GetCurrentThreadId()
		// The hook only notifies on change.
		sub.report(windows.GetForegroundWindow())
		for {
			// 0 is WM_QUIT, -1 an error; the hook callbacks run inside this call.
			if r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0); int32(r) <= 0 {
				break
			}
		}
		// UnhookWinEvent must run on the thread that installed the hook.
		procUnhookWinEvent.Call(hook)
		hooks.Delete(hook)
	}()
	tid := <-threadID
	return func() {
		sub.stopped.Store(true)
		if tid != 0 {
			procPostThreadMessage.Call(uintptr(tid), wmQuit, 0, 0)
		}
	}
}

// OnIdleChange polls GetLastInputInfo and reports crossings of thresholdMs.
// Windows has no idle notification for a plain process, so this is the one
// place the daemon polls on Windows; see idlePollEvery for the interval.
func (w *Windows) OnIdleChange(cb func(bool), thresholdMs int64) func() {
	every := w.IdlePoll
	if every == 0 {
		every = idlePollEvery(thresholdMs)
	}
	done := make(chan struct{})
	go pollIdle("windows", done, every, thresholdMs, idleMs, cb)
	return func() { close(done) }
}

// idleMs is the time since the last keyboard or pointer input in this
// session.
func idleMs() (int64, error) {
	info := lastInputInfo{size: uint32(unsafe.Sizeof(lastInputInfo{}))}
	if r, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		return 0, err
	}
	tick, _, _ := procGetTickCount64.Call()
	return idleSince(uint64(tick), info.time), nil
}
