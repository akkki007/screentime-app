//go:build darwin && cgo

package focus

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices

#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>

// Implemented in Go (//export). Only declarations may live in a preamble that
// uses //export, so every function below is static.
extern void goFocusChanged(uintptr_t handle, char *bundleID, char *name, int pid);

static void stReport(uintptr_t handle, NSRunningApplication *app) {
	if (app == nil) {
		return;
	}
	// The strings are only valid during the call; Go copies them.
	goFocusChanged(handle, (char *)[app.bundleIdentifier UTF8String],
		(char *)[app.localizedName UTF8String], (int)app.processIdentifier);
}

// stObserve reports every activation, and returns the observer token
// (retained) for stUnobserve.
static void *stObserve(uintptr_t handle) {
	NSNotificationCenter *nc = [[NSWorkspace sharedWorkspace] notificationCenter];
	// queue:nil runs the block on the posting thread: the main thread, whose
	// run loop stRunMainLoop drives.
	id token = [nc addObserverForName:NSWorkspaceDidActivateApplicationNotification
	                           object:nil
	                            queue:nil
	                       usingBlock:^(NSNotification *note) {
		stReport(handle, note.userInfo[NSWorkspaceApplicationKey]);
	}];
	return (void *)CFBridgingRetain(token);
}

static void stReportFrontmost(uintptr_t handle) {
	stReport(handle, [[NSWorkspace sharedWorkspace] frontmostApplication]);
}

static void stUnobserve(void *token) {
	[[[NSWorkspace sharedWorkspace] notificationCenter] removeObserver:CFBridgingRelease(token)];
}

// stRunMainLoop blocks the calling thread, which must be the main thread, in
// its run loop. A run loop with no sources returns at once, so one that never
// fires keeps it alive until stStopMainLoop.
static void stRunMainLoop(void) {
	CFRunLoopTimerRef keepAlive = CFRunLoopTimerCreateWithHandler(kCFAllocatorDefault,
		CFAbsoluteTimeGetCurrent() + 1e10, 1e10, 0, 0, ^(CFRunLoopTimerRef t) {});
	CFRunLoopAddTimer(CFRunLoopGetCurrent(), keepAlive, kCFRunLoopCommonModes);
	CFRunLoopRun();
	CFRunLoopRemoveTimer(CFRunLoopGetCurrent(), keepAlive, kCFRunLoopCommonModes);
	CFRelease(keepAlive);
}

static void stStopMainLoop(void) {
	CFRunLoopStop(CFRunLoopGetMain());
}

static double stIdleSeconds(void) {
	return CGEventSourceSecondsSinceLastEventType(kCGEventSourceStateCombinedSessionState, kCGAnyInputEventType);
}
*/
import "C"

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Darwin reads focus from NSWorkspaceDidActivateApplicationNotification and
// idleness from CGEventSourceSecondsSinceLastEventType, through a few lines
// of Objective-C in the cgo preamble above.
//
// The app ID is the bundle identifier (com.apple.Safari), or the localized
// name for the rare process without one. Window titles are always empty:
// reading them needs the Accessibility permission (AXUIElement), which a
// background daemon can't ask for without a UI, so this provider doesn't
// implement TitleCapture.
type Darwin struct {
	Now func() int64
	// IdlePoll overrides how often idle time is checked (idlePollEvery).
	IdlePoll time.Duration
}

func (d *Darwin) ID() string { return "macos" }

// Available always: both APIs exist on every supported macOS. Without a login
// session (an ssh shell) the notifications simply never fire.
func (d *Darwin) Available(context.Context) bool { return true }

func (d *Darwin) now() int64 {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now().UnixMilli()
}

// darwinSub is one OnFocusChange subscription. The C side knows it by a
// number, not a pointer, so that a callback already in flight when it
// unsubscribes finds nothing instead of freed memory.
type darwinSub struct {
	d    *Darwin
	cb   func(Window)
	last atomic.Int32 // the last pid reported
}

var (
	subsMu  sync.Mutex
	subs    = map[uintptr]*darwinSub{}
	nextSub uintptr
)

//export goFocusChanged
func goFocusChanged(handle C.uintptr_t, bundleID, name *C.char, pid C.int) {
	subsMu.Lock()
	sub := subs[uintptr(handle)]
	subsMu.Unlock()
	if sub == nil || sub.last.Swap(int32(pid)) == int32(pid) {
		return
	}
	sub.cb(Window{
		AppID: appIDFromBundle(C.GoString(bundleID), C.GoString(name)),
		PID:   uint32(pid),
		Ts:    sub.d.now(),
	})
}

// OnFocusChange reports the frontmost app at once, then each activation.
// The notifications arrive on the main thread's run loop, so the program
// must run RunMainLoop there (cmd/screentimed does); without it only the
// first report is made. The callback runs on that thread and returns at once.
func (d *Darwin) OnFocusChange(cb func(Window)) func() {
	sub := &darwinSub{d: d, cb: cb}
	sub.last.Store(-1)
	subsMu.Lock()
	nextSub++
	handle := nextSub
	subs[handle] = sub
	subsMu.Unlock()
	token := C.stObserve(C.uintptr_t(handle))
	// Not inline: the daemon may hold its lock while subscribing.
	go C.stReportFrontmost(C.uintptr_t(handle))
	return func() {
		C.stUnobserve(token)
		subsMu.Lock()
		delete(subs, handle)
		subsMu.Unlock()
	}
}

// RunMainLoop blocks the main thread in its run loop until StopMainLoop. The
// caller must have locked the main goroutine to the main thread (a
// runtime.LockOSThread in init).
func RunMainLoop() { C.stRunMainLoop() }

// StopMainLoop makes RunMainLoop return.
func StopMainLoop() { C.stStopMainLoop() }

// OnIdleChange polls the combined session's time since any input event and
// reports crossings of thresholdMs; there is no idle notification to wait for.
func (d *Darwin) OnIdleChange(cb func(bool), thresholdMs int64) func() {
	every := d.IdlePoll
	if every == 0 {
		every = idlePollEvery(thresholdMs)
	}
	done := make(chan struct{})
	go pollIdle("macos", done, every, thresholdMs, idleMs, cb)
	return func() { close(done) }
}

func idleMs() (int64, error) {
	return int64(float64(C.stIdleSeconds()) * 1000), nil
}
