package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"device-hub/internal/device"
)

type fakeBattery struct{}

func (fakeBattery) ID() string        { return "batt" }
func (fakeBattery) Name() string      { return "Battery Device" }
func (fakeBattery) Kind() device.Kind { return device.KindMouse }
func (fakeBattery) Image() string     { return "mouse.svg" }
func (fakeBattery) Battery() (device.BatteryReading, error) {
	return device.BatteryReading{Percent: 42, Status: "charging"}, nil
}

type fakePlain struct{}

func (fakePlain) ID() string        { return "plain" }
func (fakePlain) Name() string      { return "Plain Device" }
func (fakePlain) Kind() device.Kind { return device.KindGPU }
func (fakePlain) Image() string     { return "gpu.svg" }

type stateDoc struct {
	Devices []struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Kind       string `json:"kind"`
		HasBattery bool   `json:"hasBattery"`
		Percent    int    `json:"percent"`
		Status     string `json:"status"`
		LED        struct {
			Controller  string   `json:"controller"`
			Controllers []string `json:"controllers"`
			Enabled     bool     `json:"enabled"`
		} `json:"led"`
	} `json:"devices"`
	RGB  map[string]string `json:"rgb"`
	Sync *string           `json:"sync"`
}

func TestWriteStateIncludesEveryDevice(t *testing.T) {
	reg := device.NewRegistry()
	reg.Register(fakeBattery{})
	reg.Register(fakePlain{})

	s := New(reg)
	s.Refresh()
	s.SetLED("batt", "openrazer", []string{"openrazer", "openrgb"}, true)
	s.SetLED("plain", "openrgb", nil, true)
	s.SetRGB("batt", [3]uint8{0xff, 0x00, 0x00})
	s.SetSync([3]uint8{0x22, 0xd3, 0xee})

	path := filepath.Join(t.TempDir(), "state.json")
	if err := s.WriteState(path); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc stateDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(doc.Devices) != 2 {
		t.Fatalf("devices = %d, want 2", len(doc.Devices))
	}
	batt := doc.Devices[0]
	if !batt.HasBattery || batt.Percent != 42 || batt.Status != "charging" {
		t.Fatalf("battery device = %+v", batt)
	}
	if batt.LED.Controller != "openrazer" || len(batt.LED.Controllers) != 2 || !batt.LED.Enabled {
		t.Fatalf("battery LED = %+v", batt.LED)
	}
	plain := doc.Devices[1]
	if plain.HasBattery {
		t.Fatalf("plain device must not report battery: %+v", plain)
	}
	if plain.LED.Controllers == nil {
		t.Fatal("controllers must be an empty array, not null")
	}
	if doc.RGB["batt"] != "#ff0000" {
		t.Fatalf("rgb = %v", doc.RGB)
	}
	if doc.Sync == nil || *doc.Sync != "#22d3ee" {
		t.Fatalf("sync = %v", doc.Sync)
	}
}

func TestReadingsKeepOrderAndSkipNothing(t *testing.T) {
	reg := device.NewRegistry()
	reg.Register(fakeBattery{})
	reg.Register(fakePlain{})

	s := New(reg)
	s.Refresh()

	readings := s.Readings()
	if len(readings) != 2 {
		t.Fatalf("readings = %d, want 2", len(readings))
	}
	if readings[0].Device.ID() != "batt" || readings[1].Device.ID() != "plain" {
		t.Fatalf("order = %s, %s", readings[0].Device.ID(), readings[1].Device.ID())
	}
	if readings[1].Battery != nil {
		t.Fatal("plain device must have no battery reading")
	}
}
