package tray

import (
	"os"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// These probes cover the positive paths the decision-matrix tests cannot reach:
// a watcher that actually answers, and the registry formats a real host uses.

// fakeWatcher answers Properties.Get(RegisteredStatusNotifierItems) like a real
// SNI host.
type fakeWatcher struct{ items []string }

func (f *fakeWatcher) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	if iface == watcherIface && prop == "RegisteredStatusNotifierItems" {
		return dbus.MakeVariant(append([]string(nil), f.items...)), nil
	}
	return dbus.MakeVariant(uint32(0)), nil
}

func serveWatcher(t *testing.T, addr string, items []string) *dbus.Conn {
	t.Helper()
	conn := dialBus(t, addr)
	if err := conn.Export(&fakeWatcher{items: items}, watcherPath, "org.freedesktop.DBus.Properties"); err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := conn.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatalf("request name: %v", err)
	}
	return conn
}

// Owning the name is not enough: the object must answer, because systray
// registers the icon exactly once and a name without an exported object would
// leave the daemon permanently without an icon.
func TestWaitForWatcherRequiresAnsweringWatcher(t *testing.T) {
	addr, stop := privateBus(t)
	defer stop()
	conn := dialBus(t, addr)

	owner := dialBus(t, addr)
	if _, err := owner.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatalf("request: %v", err)
	}
	if waitForWatcher(conn, 600*time.Millisecond) {
		t.Fatal("waitForWatcher accepted a name owner that does not answer Properties.Get")
	}

	if _, err := owner.ReleaseName(watcherName); err != nil {
		t.Fatalf("release: %v", err)
	}
	serveWatcher(t, addr, nil)
	if !waitForWatcher(conn, 2*time.Second) {
		t.Fatal("waitForWatcher did not accept an answering watcher")
	}
}

// A genuine replacement is a single NameOwnerChanged with both owners set.
// When the new watcher still lists our item, the icon is alive: no restart.
func TestNoRestartWhenReplacementStillListsUs(t *testing.T) {
	addr, stop := privateBus(t)
	defer stop()
	conn := dialBus(t, addr)

	items := []string{sniName(os.Getpid()) + "/StatusNotifierItem"}
	serveWatcher(t, addr, items)

	lost := make(chan struct{}, 1)
	go watchWatcherSignals(conn, func() { lost <- struct{}{} })
	time.Sleep(300 * time.Millisecond)

	second := dialBus(t, addr)
	if err := second.Export(&fakeWatcher{items: items}, watcherPath, "org.freedesktop.DBus.Properties"); err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := second.RequestName(watcherName, dbus.NameFlagDoNotQueue|dbus.NameFlagReplaceExisting); err != nil {
		t.Fatalf("replace: %v", err)
	}

	select {
	case <-lost:
		t.Fatal("restart requested although the new watcher still lists our item")
	case <-time.After(1200 * time.Millisecond):
	}
}

// Unrelated bus traffic must never restart the daemon.
func TestNoRestartOnUnrelatedNameChange(t *testing.T) {
	addr, stop := privateBus(t)
	defer stop()
	conn := dialBus(t, addr)
	serveWatcher(t, addr, []string{sniName(os.Getpid()) + "/StatusNotifierItem"})

	lost := make(chan struct{}, 1)
	go watchWatcherSignals(conn, func() { lost <- struct{}{} })
	time.Sleep(300 * time.Millisecond)

	other := dialBus(t, addr)
	for i := 0; i < 5; i++ {
		if _, err := other.RequestName("com.example.probe", dbus.NameFlagDoNotQueue); err != nil {
			t.Fatalf("request other: %v", err)
		}
		if _, err := other.ReleaseName("com.example.probe"); err != nil {
			t.Fatalf("release other: %v", err)
		}
	}

	select {
	case <-lost:
		t.Fatal("restart requested on unrelated NameOwnerChanged")
	case <-time.After(800 * time.Millisecond):
	}
}

// The registry stores "<uniqueName>/StatusNotifierItem", because the library
// registers by object path. That format must be recognised as us.
func TestRegisteredAsUsUniqueName(t *testing.T) {
	addr, stop := privateBus(t)
	defer stop()
	conn := dialBus(t, addr)

	name := sniName(os.Getpid())
	if _, err := conn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatalf("request %s: %v", name, err)
	}
	unique := busOwner(conn, name)
	if unique == "" {
		t.Fatal("no unique name owner")
	}
	if !registeredAsUs(conn, []string{unique + "/StatusNotifierItem"}) {
		t.Fatalf("registry entry %q not recognised as us", unique+"/StatusNotifierItem")
	}
	if registeredAsUs(conn, []string{":1.99/StatusNotifierItem"}) || registeredAsUs(conn, nil) {
		t.Fatal("foreign or empty registry recognised as us")
	}
}

// The signal loop must exit without panicking when the connection terminates;
// the deferred RemoveSignal must not close an already-closed channel.
func TestSignalLoopExitsOnConnCloseWithoutPanic(t *testing.T) {
	addr, stop := privateBus(t)
	defer stop()

	conn, err := dbus.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := conn.Auth(nil); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := conn.Hello(); err != nil {
		t.Fatalf("hello: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic in signal loop: %v", r)
			}
			close(done)
		}()
		watchWatcherSignals(conn, func() {})
	}()

	time.Sleep(200 * time.Millisecond)
	_ = conn.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("signal loop did not exit after the connection closed")
	}
}
