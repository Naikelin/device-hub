package tray

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	watcherName  = "org.kde.StatusNotifierWatcher"
	watcherPath  = "/StatusNotifierWatcher"
	watcherIface = "org.kde.StatusNotifierWatcher"

	// watcherWaitTimeout bounds how long Run waits for the SNI watcher before
	// starting the tray anyway. A desktop with no tray host must not delay the
	// rest of the daemon.
	watcherWaitTimeout = 30 * time.Second

	// probeInterval is how often the readiness probe and the registration check
	// poll the bus.
	probeInterval = 200 * time.Millisecond

	// registerCheckWait bounds the post-start confirmation. It is diagnostic
	// only: the daemon keeps running without an icon rather than crash-looping.
	registerCheckWait = 3 * time.Second

	// watcherExitCode makes systemd (Restart=on-failure) bring the daemon back
	// after the watcher disappears.
	watcherExitCode = 1
)

// sniName is the bus name the systray library requests for this process:
// "org.kde.StatusNotifierItem-<pid>-1".
func sniName(pid int) string {
	return fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", pid)
}

// itemRegistered reports whether busName is in the watcher registry.
// Entries look like "org.kde.StatusNotifierItem-123-1/StatusNotifierItem" or
// ":1.36/StatusNotifierItem", so only the part before "/" is the bus name.
func itemRegistered(items []string, busName string) bool {
	if busName == "" {
		return false
	}
	for _, item := range items {
		if name, _, _ := strings.Cut(item, "/"); name == busName {
			return true
		}
	}
	return false
}

// watcherTransitionNeedsRestart decides whether a NameOwnerChanged event left
// the icon unusable. registered is our item's current presence in the registry.
//
// A watcher that dies always orphans the icon, whatever the registry still
// answers. A watcher that appears or is replaced gives a brand new registry, so
// we only survive it if we are still listed.
func watcherTransitionNeedsRestart(oldOwner, newOwner string, registered bool) bool {
	if oldOwner == newOwner {
		return false
	}
	if newOwner == "" {
		return true
	}
	return !registered
}

// registeredAsUs reports whether the watcher lists this process. The library
// registers by object path, so the watcher may record either the well-known
// name or the unique name behind it.
func registeredAsUs(conn *dbus.Conn, items []string) bool {
	name := sniName(os.Getpid())
	return itemRegistered(items, name) || itemRegistered(items, busOwner(conn, name))
}

// probeWatcher reports whether the SNI watcher is serving and returns its
// current registry. Owning the bus name is not enough: the object must answer,
// because systray.Run registers the icon exactly once and never retries.
func probeWatcher(conn *dbus.Conn) (bool, []string) {
	var has bool
	err := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus").
		Call("org.freedesktop.DBus.NameHasOwner", 0, watcherName).Store(&has)
	if err != nil || !has {
		return false, nil
	}

	var variant dbus.Variant
	err = conn.Object(watcherName, watcherPath).
		Call("org.freedesktop.DBus.Properties.Get", 0, watcherIface, "RegisteredStatusNotifierItems").
		Store(&variant)
	if err != nil {
		return false, nil
	}
	items, _ := variant.Value().([]string)
	return true, items
}

// waitForWatcher blocks until the watcher answers, up to timeout. This is what
// keeps the one-shot registration from landing before the tray host exists,
// which is what happened when the daemon started 21 minutes before ryoku-shell.
func waitForWatcher(conn *dbus.Conn, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if ready, _ := probeWatcher(conn); ready {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(probeInterval)
	}
}

// watchWatcher recovers the icon when the watcher goes away. The systray
// library closes a package-level quit channel guarded by a sync.Once and never
// reopens it, so the icon cannot be registered again in the same process: under
// systemd the process cycles and starts again, which also covers a shell reload
// that hands the desktop a brand new (empty) registry.
//
// Without a supervisor, exiting would kill the control socket and the D-Bus
// service for good, so the daemon keeps serving and only the icon stays missing.
func watchWatcher(conn *dbus.Conn) {
	watchWatcherSignals(conn, func() {
		if os.Getenv("INVOCATION_ID") == "" {
			log.Printf("tray: not supervised by systemd; keeping the daemon alive without the icon")
			return
		}
		log.Printf("tray: restarting to register the icon again")
		os.Exit(watcherExitCode)
	})
}

// watchWatcherSignals calls onLoss when a NameOwnerChanged event leaves the
// icon orphaned: the watcher died, or a different watcher took over its name.
// onLoss is a parameter so a test can observe the detection on a private bus
// instead of exiting the test binary.
func watchWatcherSignals(conn *dbus.Conn, onLoss func()) {
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	// RemoveSignal unregisters the channel but does not close it; godbus closes
	// it only when the connection itself terminates (default_handler.go). Closing
	// it here would panic if the connection had already done so.
	defer conn.RemoveSignal(signals)

	err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/DBus"),
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
	)
	if err != nil {
		log.Printf("tray: cannot watch %s: %v", watcherName, err)
		return
	}

	for sig := range signals {
		if sig.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(sig.Body) != 3 {
			continue
		}
		name, _ := sig.Body[0].(string)
		oldOwner, _ := sig.Body[1].(string)
		newOwner, _ := sig.Body[2].(string)
		if name != watcherName {
			continue
		}

		var registered bool
		if ready, items := probeWatcher(conn); ready {
			registered = registeredAsUs(conn, items)
		}
		if !watcherTransitionNeedsRestart(oldOwner, newOwner, registered) {
			continue
		}
		log.Printf("tray: %s changed (%q -> %q) and the icon is not registered",
			watcherName, oldOwner, newOwner)
		onLoss()
	}
}

// busOwner returns the unique bus name that owns busName, or "" when nobody
// does. The systray library registers the item by object path, so the watcher
// records the sender's unique name (":1.94"), never the well-known
// "org.kde.StatusNotifierItem-<pid>-1" it also requests.
func busOwner(conn *dbus.Conn, busName string) string {
	var owner string
	err := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus").
		Call("org.freedesktop.DBus.GetNameOwner", 0, busName).Store(&owner)
	if err != nil {
		return ""
	}
	return owner
}

// confirmRegistration waits for the icon to appear in the watcher registry and
// logs the result.
func confirmRegistration(conn *dbus.Conn, wait time.Duration) {
	name := sniName(os.Getpid())
	deadline := time.Now().Add(wait)
	for {
		if ready, items := probeWatcher(conn); ready && registeredAsUs(conn, items) {
			log.Printf("tray: icon registered as %s", name)
			return
		}
		if time.Now().After(deadline) {
			log.Printf("tray: warning: %s is not in the watcher registry after %s; the daemon keeps running without the icon", name, wait)
			return
		}
		time.Sleep(probeInterval)
	}
}
