package led

import (
	"path/filepath"
	"testing"

	"device-hub/internal/device"
	"device-hub/internal/store"
)

type fakeBackend struct {
	sets map[string][3]uint8
	offs map[string]int
	err  error
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{sets: map[string][3]uint8{}, offs: map[string]int{}}
}

func (f *fakeBackend) Set(deviceID string, color [3]uint8) error {
	if f.err != nil {
		return f.err
	}
	f.sets[deviceID] = color
	return nil
}

func (f *fakeBackend) Off(deviceID string) error {
	if f.err != nil {
		return f.err
	}
	f.offs[deviceID]++
	return nil
}

func newTestManager(t *testing.T, backend Backend) *Manager {
	t.Helper()
	reg := device.NewRegistry()
	st := store.New(reg)
	m, err := NewManager(Config{
		Path:      filepath.Join(t.TempDir(), "leds.json"),
		Backends:  map[Controller]Backend{OpenRGB: backend, OpenRazer: backend},
		Available: map[string][]Controller{"mouse": {OpenRazer, OpenRGB}, "keyboard": {OpenRGB}},
		Defaults: map[string]Choice{
			"mouse":    {Controller: OpenRazer, Enabled: true},
			"keyboard": {Controller: OpenRGB, Enabled: true},
		},
		Store: st,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestDefaultsFillEveryDevice(t *testing.T) {
	m := newTestManager(t, newFakeBackend())

	if got := m.Choice("mouse"); got != (Choice{Controller: OpenRazer, Enabled: true}) {
		t.Fatalf("mouse choice = %+v", got)
	}
	if got := m.Choice("keyboard"); got != (Choice{Controller: OpenRGB, Enabled: true}) {
		t.Fatalf("keyboard choice = %+v", got)
	}
}

func TestSetEnabledOffThenOn(t *testing.T) {
	backend := newFakeBackend()
	m := newTestManager(t, backend)

	if err := m.SetEnabled("mouse", false); err != nil {
		t.Fatalf("off: %v", err)
	}
	if backend.offs["mouse"] != 1 {
		t.Fatalf("Off calls = %d", backend.offs["mouse"])
	}
	if m.Choice("mouse").Enabled {
		t.Fatal("mouse should be disabled")
	}

	if err := m.SetEnabled("mouse", true); err != nil {
		t.Fatalf("on: %v", err)
	}
	if got := backend.sets["mouse"]; got != DefaultColor {
		t.Fatalf("on color = %v, want %v", got, DefaultColor)
	}
	if !m.Choice("mouse").Enabled {
		t.Fatal("mouse should be enabled")
	}
}

func TestSetEnabledFailureKeepsState(t *testing.T) {
	backend := newFakeBackend()
	m := newTestManager(t, backend)
	backend.err = errTest

	if err := m.SetEnabled("mouse", false); err == nil {
		t.Fatal("expected error")
	}
	if !m.Choice("mouse").Enabled {
		t.Fatal("state must not change when the backend fails")
	}
}

func TestSetControllerRejectsUnavailable(t *testing.T) {
	m := newTestManager(t, newFakeBackend())

	if err := m.SetController("keyboard", OpenRazer); err == nil {
		t.Fatal("keyboard must reject openrazer")
	}
	if err := m.SetController("mouse", "bogus"); err == nil {
		t.Fatal("unknown controller must be rejected")
	}
	if got := m.Choice("mouse").Controller; got != OpenRazer {
		t.Fatalf("controller changed to %s", got)
	}
}

func TestSetControllerReappliesThroughNewBackend(t *testing.T) {
	backend := newFakeBackend()
	m := newTestManager(t, backend)

	if err := m.Apply("mouse", [3]uint8{1, 2, 3}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := m.SetController("mouse", OpenRGB); err != nil {
		t.Fatalf("controller: %v", err)
	}
	if got := backend.sets["mouse"]; got != [3]uint8{1, 2, 3} {
		t.Fatalf("reapplied color = %v", got)
	}
	if got := m.Choice("mouse").Controller; got != OpenRGB {
		t.Fatalf("controller = %s", got)
	}
}

func TestSyncSkipsDisabledDevices(t *testing.T) {
	backend := newFakeBackend()
	m := newTestManager(t, backend)

	if err := m.SetEnabled("keyboard", false); err != nil {
		t.Fatalf("off: %v", err)
	}
	backend.sets = map[string][3]uint8{}

	color := [3]uint8{9, 9, 9}
	if err := m.Sync(color); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if _, ok := backend.sets["keyboard"]; ok {
		t.Fatal("disabled device must be skipped")
	}
	if backend.sets["mouse"] != color {
		t.Fatalf("mouse color = %v", backend.sets["mouse"])
	}
}

func TestChoicesPersistAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "leds.json")
	backend := newFakeBackend()

	cfg := Config{
		Path:      path,
		Backends:  map[Controller]Backend{OpenRGB: backend},
		Available: map[string][]Controller{"keyboard": {OpenRGB}},
		Defaults:  map[string]Choice{"keyboard": {Controller: OpenRGB, Enabled: true}},
	}
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := m.SetEnabled("keyboard", false); err != nil {
		t.Fatalf("off: %v", err)
	}

	reloaded, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Choice("keyboard").Enabled {
		t.Fatal("disabled flag did not persist")
	}
}

func TestRestoreOffOnlyTouchesDisabled(t *testing.T) {
	backend := newFakeBackend()
	m := newTestManager(t, backend)
	if err := m.SetEnabled("keyboard", false); err != nil {
		t.Fatalf("off: %v", err)
	}
	backend.offs = map[string]int{}

	m.RestoreOff()

	if backend.offs["keyboard"] != 1 {
		t.Fatalf("keyboard Off calls = %d", backend.offs["keyboard"])
	}
	if backend.offs["mouse"] != 0 {
		t.Fatalf("enabled mouse must be left alone, Off calls = %d", backend.offs["mouse"])
	}
}

var errTest = &testError{}

type testError struct{}

func (*testError) Error() string { return "backend down" }
