package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncWritesAndRemovesTheDropIn(t *testing.T) {
	u := &Unit{Dir: filepath.Join(t.TempDir(), "screentime-daemon.service.d")}
	path := filepath.Join(u.Dir, DropInName)

	if changed, err := u.Sync(false); err != nil || changed {
		t.Fatalf("off with no drop-in: changed=%v err=%v", changed, err)
	}
	if changed, err := u.Sync(true); err != nil || !changed {
		t.Fatalf("turning on: changed=%v err=%v", changed, err)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6") {
		t.Fatalf("drop-in = %q, %v", b, err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", st.Mode().Perm())
	}
	// Already on: no restart, so a restart can never loop.
	if changed, err := u.Sync(true); err != nil || changed {
		t.Fatalf("on again: changed=%v err=%v", changed, err)
	}
	if changed, err := u.Sync(false); err != nil || !changed {
		t.Fatalf("turning off: changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("drop-in still there: %v", err)
	}
}

func TestSyncRepairsAnEditedDropIn(t *testing.T) {
	u := &Unit{Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(u.Dir, DropInName), []byte("[Service]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := u.Sync(true); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
}

func TestDropInDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if d, err := DropInDir("screentime-daemon.service"); err != nil || d != "/cfg/systemd/user/screentime-daemon.service.d" {
		t.Errorf("dir = %q, %v", d, err)
	}
	for _, bad := range []string{"", "../x.service", "a/b.service"} {
		if _, err := DropInDir(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestDetectOutsideSystemd(t *testing.T) {
	t.Setenv("INVOCATION_ID", "")
	if Detect() != nil {
		t.Error("detected a unit outside systemd")
	}
}
