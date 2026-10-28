package focus

import "strings"

// unknownApp is the app ID for a window whose process can't be identified
// (a protected or already-exited process).
const unknownApp = "unknown"

// appIDFromExe is the Windows app ID for an executable path: the file name
// without ".exe", lowercased, so Chrome.EXE and chrome.exe are one app.
// Windows has no desktop-entry IDs, so the exe name is the stable handle:
// it survives updates and moves of the install directory, and reads well
// as a fallback display name. Two products that share an exe name share an
// ID, and UWP apps all appear as applicationframehost. Both are accepted
// until there is a use for the richer AppUserModelID.
//
// Either path separator is accepted: the path comes from the OS, and this
// runs in tests on every platform.
func appIDFromExe(path string) string {
	if i := strings.LastIndexAny(path, `\/`); i >= 0 {
		path = path[i+1:]
	}
	if len(path) > 4 && strings.EqualFold(path[len(path)-4:], ".exe") {
		path = path[:len(path)-4]
	}
	if path == "" {
		return unknownApp
	}
	return strings.ToLower(path)
}

// appIDFromBundle is the macOS app ID: the bundle identifier
// (com.apple.Safari, the closest thing to a desktop-entry ID), or the
// localized name for the few processes that have none.
func appIDFromBundle(bundleID, localizedName string) string {
	switch {
	case bundleID != "":
		return bundleID
	case localizedName != "":
		return localizedName
	}
	return unknownApp
}

// idleSince is the time in ms since the last input on Windows, where
// GetLastInputInfo reports a 32-bit tick count that wraps every 49.7 days
// and GetTickCount64 a 64-bit one: only the low 32 bits are comparable, and
// unsigned subtraction handles the wrap.
func idleSince(tick64 uint64, lastInput uint32) int64 {
	return int64(uint32(tick64) - lastInput)
}
