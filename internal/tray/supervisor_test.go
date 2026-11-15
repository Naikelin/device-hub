package tray

import (
	"bufio"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestSNIName(t *testing.T) {
	if got, want := sniName(1278), "org.kde.StatusNotifierItem-1278-1"; got != want {
		t.Fatalf("sniName(1278) = %q, want %q", got, want)
	}
}

func TestItemRegistered(t *testing.T) {
	// The watcher registry stores "busName/objectPath", and the bus name can be
	// a unique name (":1.36") or a well-known one.
	items := []string{
		"org.kde.StatusNotifierItem-7845-1/StatusNotifierItem",
		":1.36/StatusNotifierItem",
	}
	cases := []struct {
		busName string
		want    bool
	}{
		{"org.kde.StatusNotifierItem-7845-1", true},
		{":1.36", true},
		{"org.kde.StatusNotifierItem-1278-1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := itemRegistered(items, tc.busName); got != tc.want {
			t.Errorf("itemRegistered(%q) = %v, want %v", tc.busName, got, tc.want)
		}
	}
}

func TestWatcherTransitionNeedsRestart(t *testing.T) {
	cases := []struct {
		oldOwner, newOwner string
		registered         bool
		want               bool
	}{
		{"", "", false, false},           // no transition
		{":1.37", ":1.37", false, false}, // same owner
		{":1.37", "", true, true},        // watcher died: always orphaned
		{":1.37", ":1.44", false, true},  // replaced: fresh empty registry
		{":1.37", ":1.44", true, false},  // replaced but we are still listed
		{"", ":1.44", false, true},       // appeared after we gave up waiting
		{"", ":1.44", true, false},       // appeared and we are listed
	}
	for _, tc := range cases {
		if got := watcherTransitionNeedsRestart(tc.oldOwner, tc.newOwner, tc.registered); got != tc.want {
			t.Errorf("watcherTransitionNeedsRestart(%q, %q, %v) = %v, want %v",
				tc.oldOwner, tc.newOwner, tc.registered, got, tc.want)
		}
	}
}

// privateBus starts a throwaway session bus. The test must never take the
// org.kde.StatusNotifierWatcher name on the desktop's real bus: that would
// hijack the live tray.
func privateBus(t *testing.T) (string, func()) {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon not installed")
	}

	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start dbus-daemon: %v", err)
	}

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		_ = cmd.Process.Kill()
		t.Fatalf("dbus-daemon printed no address: %v", scanner.Err())
	}
	addr := strings.TrimSpace(scanner.Text())

	return addr, func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func dialBus(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Dial(addr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	if err := conn.Auth(nil); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := conn.Hello(); err != nil {
		t.Fatalf("hello: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestWatcherLossIsDetected(t *testing.T) {
	addr, stop := privateBus(t)
	defer stop()

	conn := dialBus(t, addr)
	owner := dialBus(t, addr)

	if _, err := owner.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatalf("request %s: %v", watcherName, err)
	}

	lost := make(chan struct{}, 1)
	go watchWatcherSignals(conn, func() { lost <- struct{}{} })
	time.Sleep(300 * time.Millisecond) // let the match rule land

	if _, err := owner.ReleaseName(watcherName); err != nil {
		t.Fatalf("release %s: %v", watcherName, err)
	}

	select {
	case <-lost:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher loss was not detected")
	}
}

func TestWatcherReplacementIsDetected(t *testing.T) {
	addr, stop := privateBus(t)
	defer stop()

	conn := dialBus(t, addr)
	first := dialBus(t, addr)
	second := dialBus(t, addr)

	if _, err := first.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatalf("request %s: %v", watcherName, err)
	}

	lost := make(chan struct{}, 1)
	go watchWatcherSignals(conn, func() { lost <- struct{}{} })
	time.Sleep(300 * time.Millisecond)

	// A shell reload does exactly this: the old owner releases and a new one
	// takes the name, leaving a fresh empty registry behind.
	if _, err := first.ReleaseName(watcherName); err != nil {
		t.Fatalf("release %s: %v", watcherName, err)
	}
	if _, err := second.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatalf("second request %s: %v", watcherName, err)
	}

	select {
	case <-lost:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher replacement was not detected")
	}
}

// A watcher that appears after Run gave up waiting would never register us, so
// the daemon must restart and try again.
func TestWatcherAppearanceDetectedWhenUnregistered(t *testing.T) {
	addr, stop := privateBus(t)
	defer stop()

	conn := dialBus(t, addr)
	owner := dialBus(t, addr)

	lost := make(chan struct{}, 1)
	go watchWatcherSignals(conn, func() { lost <- struct{}{} })
	time.Sleep(300 * time.Millisecond)

	if _, err := owner.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatalf("request %s: %v", watcherName, err)
	}

	select {
	case <-lost:
	case <-time.After(3 * time.Second):
		t.Fatal("a watcher appearing while unregistered was not detected")
	}
}
