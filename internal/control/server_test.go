package control

import (
	"testing"
	"time"

	"device-hub/internal/led"
)

type fakeManager struct {
	applied map[string][3]uint8
	synced  *[3]uint8
	enabled map[string]bool
	ctrl    map[string]led.Controller
}

func newFakeManager() *fakeManager {
	return &fakeManager{
		applied: map[string][3]uint8{},
		enabled: map[string]bool{},
		ctrl:    map[string]led.Controller{},
	}
}

func (f *fakeManager) Apply(deviceID string, color [3]uint8) error {
	f.applied[deviceID] = color
	return nil
}

func (f *fakeManager) Sync(color [3]uint8) error {
	f.synced = &color
	return nil
}

func (f *fakeManager) SetEnabled(deviceID string, enabled bool) error {
	f.enabled[deviceID] = enabled
	return nil
}

func (f *fakeManager) SetController(deviceID string, controller led.Controller) error {
	f.ctrl[deviceID] = controller
	return nil
}

func TestExecuteRoutesEveryAction(t *testing.T) {
	m := newFakeManager()
	s := &Server{m: m}

	cases := []struct {
		line  string
		check func(t *testing.T)
	}{
		{`{"action":"set","device":"a","color":"#ff0000"}`, func(t *testing.T) {
			if m.applied["a"] != [3]uint8{0xff, 0, 0} {
				t.Fatalf("applied = %v", m.applied["a"])
			}
		}},
		{`{"action":"sync","color":"00ff00"}`, func(t *testing.T) {
			if m.synced == nil || *m.synced != [3]uint8{0, 0xff, 0} {
				t.Fatalf("synced = %v", m.synced)
			}
		}},
		{`{"action":"off","device":"a"}`, func(t *testing.T) {
			if m.enabled["a"] {
				t.Fatal("off did not disable")
			}
		}},
		{`{"action":"on","device":"a"}`, func(t *testing.T) {
			if !m.enabled["a"] {
				t.Fatal("on did not enable")
			}
		}},
		{`{"action":"controller","device":"a","controller":"openrazer"}`, func(t *testing.T) {
			if m.ctrl["a"] != led.OpenRazer {
				t.Fatalf("controller = %s", m.ctrl["a"])
			}
		}},
	}

	for _, tc := range cases {
		if got := s.execute(tc.line); !got.OK {
			t.Fatalf("%s -> %+v", tc.line, got)
		}
		tc.check(t)
	}
}

func TestExecuteOpenRGB(t *testing.T) {
	called := make(chan struct{}, 1)
	s := &Server{m: newFakeManager(), onOpenRGB: func() { called <- struct{}{} }}

	if got := s.execute(`{"action":"openrgb"}`); !got.OK {
		t.Fatalf("reply = %+v", got)
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("openrgb handler was not called")
	}
}

func TestExecuteProfile(t *testing.T) {
	var loaded string
	s := &Server{m: newFakeManager(), onProfile: func(name string) error {
		loaded = name
		return nil
	}}
	if got := s.execute(`{"action":"profile","profile":"nk"}`); !got.OK {
		t.Fatalf("reply = %+v", got)
	}
	if loaded != "nk" {
		t.Fatalf("loaded = %q", loaded)
	}
	if got := s.execute(`{"action":"profile"}`); got.OK {
		t.Fatal("missing profile must fail")
	}
}

func TestExecuteRejectsBadInput(t *testing.T) {
	s := &Server{m: newFakeManager()}

	for _, line := range []string{
		`not json`,
		`{"action":"set","device":"a","color":"nope"}`,
		`{"action":"set","color":"#ff0000"}`,
		`{"action":"controller","device":"a"}`,
		`{"action":"bogus"}`,
	} {
		if got := s.execute(line); got.OK {
			t.Fatalf("%s must fail", line)
		}
	}
}
