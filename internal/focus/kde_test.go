package focus

import (
	"errors"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// kwinCall is what adapters/kwin-script does on window activation.
func kwinCall(conn *dbus.Conn, appID, title, pid string) (bool, error) {
	var capture bool
	err := conn.Object(daemonName, kwinPath).Call(kwinInterface+".FocusChanged", 0, appID, title, pid).Store(&capture)
	return capture, err
}

func startKDE(t *testing.T) (*KDE, func() (*dbus.Conn, error), chan Window, func()) {
	t.Helper()
	bus := privateBus(t)
	k := &KDE{Connect: bus, Now: func() int64 { return 1000 }}
	got := make(chan Window, 8)
	stop := k.OnFocusChange(func(w Window) { got <- w })
	return k, bus, got, stop
}

func ownKWin(t *testing.T, bus func() (*dbus.Conn, error)) *dbus.Conn {
	t.Helper()
	kwin := connect(t, bus)
	if reply, err := kwin.RequestName(kwinService, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("owning %s: %v %v", kwinService, reply, err)
	}
	return kwin
}

func next(t *testing.T, got chan Window) Window {
	t.Helper()
	select {
	case w := <-got:
		return w
	case <-time.After(2 * time.Second):
		t.Fatal("no focus change")
		return Window{}
	}
}

func TestKDEReportsFocusWithoutTitlesUntilOptIn(t *testing.T) {
	k, bus, got, stop := startKDE(t)
	defer stop()
	kwin := ownKWin(t, bus)

	capture, err := kwinCall(kwin, "org.kde.dolphin", "secret.pdf", "1234")
	if err != nil || capture {
		t.Fatalf("FocusChanged = %v, %v; want false, nil", capture, err)
	}
	if w := next(t, got); w != (Window{AppID: "org.kde.dolphin", PID: 1234, Ts: 1000}) {
		t.Fatalf("got %+v", w)
	}

	k.SetCaptureTitles(true)
	capture, err = kwinCall(kwin, "firefox", "Inbox", "77")
	if err != nil || !capture {
		t.Fatalf("FocusChanged = %v, %v; want true, nil", capture, err)
	}
	if w := next(t, got); w.AppID != "firefox" || w.Title != "Inbox" {
		t.Fatalf("got %+v", w)
	}
}

func TestKDERefusesCallersOtherThanKWin(t *testing.T) {
	_, bus, got, stop := startKDE(t)
	defer stop()
	ownKWin(t, bus)
	impostor := connect(t, bus)

	_, err := kwinCall(impostor, "fake.app", "", "1")
	var dbusErr dbus.Error
	if !errors.As(err, &dbusErr) || dbusErr.Name != "org.freedesktop.DBus.Error.AccessDenied" {
		t.Fatalf("err = %v, want AccessDenied", err)
	}
	select {
	case w := <-got:
		t.Fatalf("impostor's focus was reported: %+v", w)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestKDERefusesEveryoneWithoutKWin(t *testing.T) {
	_, bus, _, stop := startKDE(t)
	defer stop()
	if _, err := kwinCall(connect(t, bus), "fake.app", "", "1"); err == nil {
		t.Fatal("call accepted with no KWin on the bus")
	}
}

func TestKDEIgnoresEmptyAppIDAndBadPID(t *testing.T) {
	_, bus, got, stop := startKDE(t)
	defer stop()
	kwin := ownKWin(t, bus)
	if _, err := kwinCall(kwin, "", "", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := kwinCall(kwin, "org.kde.konsole", "", "not-a-pid"); err != nil {
		t.Fatal(err)
	}
	if w := next(t, got); w.AppID != "org.kde.konsole" || w.PID != 0 {
		t.Fatalf("got %+v", w)
	}
}

func TestKDEUnsubscribeReleasesTheName(t *testing.T) {
	_, bus, _, stop := startKDE(t)
	kwin := ownKWin(t, bus)
	stop()
	if _, err := kwinCall(kwin, "org.kde.dolphin", "", "1"); err == nil {
		t.Fatal("call still answered after unsubscribe")
	}
}

func TestKDEAvailable(t *testing.T) {
	bus := privateBus(t)
	k := &KDE{Connect: bus}
	for _, c := range []struct {
		session, desktop string
		want             bool
	}{
		{"wayland", "KDE", true},
		{"wayland", "GNOME", false},
		{"x11", "KDE", false},
	} {
		t.Setenv("XDG_SESSION_TYPE", c.session)
		t.Setenv("XDG_CURRENT_DESKTOP", c.desktop)
		if got := k.Available(t.Context()); got != c.want {
			t.Errorf("%s/%s: Available = %v", c.session, c.desktop, got)
		}
	}
}

func TestKDEDropsCallsOvertakenByANewerOne(t *testing.T) {
	k, bus, got, stop := startKDE(t)
	defer stop()
	kwin := ownKWin(t, bus)
	sender := kwin.Names()[0]

	// As if godbus had run serial 5's handler before serial 4's.
	for _, c := range []struct {
		serial uint32
		appID  string
	}{{5, "org.kde.konsole"}, {4, "org.kde.dolphin"}, {6, "firefox"}} {
		if _, err := k.focusChanged(sender, c.serial, c.appID, "", "1"); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"org.kde.konsole", "firefox"} {
		if w := next(t, got); w.AppID != want {
			t.Fatalf("got %s, want %s", w.AppID, want)
		}
	}
	select {
	case w := <-got:
		t.Fatalf("stale call delivered: %+v", w)
	case <-time.After(100 * time.Millisecond):
	}
}
