package rgb

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"
)

// buildControllerBlock encodes a protocol-4 controller data block.
func buildControllerBlock(name string, numLEDs int, zones []struct {
	name string
	leds int
}, modes []Mode) []byte {
	var b []byte
	put := func(v uint32) { b = binary.LittleEndian.AppendUint32(b, v) }
	put16 := func(v uint16) { b = binary.LittleEndian.AppendUint16(b, v) }
	str := func(s string) {
		put16(uint16(len(s) + 1))
		b = append(b, s...)
		b = append(b, 0)
	}

	put(0) // data_size (patched at the end)
	put(0) // device type
	str(name)
	str("") // vendor
	str("") // description
	str("") // version
	str("") // serial
	str("") // location
	put16(uint16(len(modes)))
	put(0) // active_mode
	for _, m := range modes {
		b = append(b, encodeMode(m, 4)...)
	}
	put16(uint16(len(zones)))
	for _, z := range zones {
		str(z.name)
		put(0)              // zone type
		put(0)              // leds_min
		put(0)              // leds_max
		put(uint32(z.leds)) // num_leds
		put16(0)            // matrix size
		put16(0)            // segments
	}
	put16(uint16(numLEDs))
	for i := 0; i < numLEDs; i++ {
		str(fmt.Sprintf("L%d", i))
		put(0) // LED value
	}
	put16(0) // num_colors

	binary.LittleEndian.PutUint32(b, uint32(len(b)))
	return b
}

func TestParseController(t *testing.T) {
	raw := buildControllerBlock("Test Device", 3, []struct {
		name string
		leds int
	}{{"Logo", 1}, {"Keys", 2}}, nil)

	ctrl, err := parseController(raw, 4)
	if err != nil {
		t.Fatalf("parseController: %v", err)
	}
	if ctrl.Name != "Test Device" {
		t.Errorf("name = %q, want Test Device", ctrl.Name)
	}
	if ctrl.NumLEDs != 3 {
		t.Errorf("NumLEDs = %d, want 3", ctrl.NumLEDs)
	}
	if len(ctrl.Zones) != 2 {
		t.Fatalf("len(Zones) = %d, want 2", len(ctrl.Zones))
	}
	if ctrl.Zones[1].Name != "Keys" || ctrl.Zones[1].NumLEDs != 2 {
		t.Errorf("zone[1] = %+v, want Keys/2", ctrl.Zones[1])
	}
}

func TestParseControllerModes(t *testing.T) {
	raw := buildControllerBlock("Leviathan", 2, nil, []Mode{
		{Name: "Off", Value: 1},
		{Name: "Static", Value: 2, Flags: 80, Brightness: 255, ColorMode: 2,
			Colors: [][3]uint8{{1, 2, 3}}},
	})

	ctrl, err := parseController(raw, 4)
	if err != nil {
		t.Fatalf("parseController: %v", err)
	}
	if len(ctrl.Modes) != 2 {
		t.Fatalf("len(Modes) = %d, want 2", len(ctrl.Modes))
	}
	if ctrl.Modes[0].Name != "Off" || ctrl.Modes[0].Index != 0 {
		t.Errorf("mode[0] = %+v, want Off/0", ctrl.Modes[0])
	}
	static := ctrl.Modes[1]
	if static.Name != "Static" || static.Value != 2 || static.Flags != 80 {
		t.Errorf("mode[1] = %+v, want Static/2/80", static)
	}
	if len(static.Colors) != 1 || static.Colors[0] != [3]uint8{1, 2, 3} {
		t.Errorf("static colors = %v, want [[1 2 3]]", static.Colors)
	}
}

func TestEncodeModeRoundTrip(t *testing.T) {
	want := Mode{
		Index:      2,
		Name:       "Static",
		Value:      7,
		Flags:      80,
		SpeedMin:   1,
		SpeedMax:   3,
		ColorsMin:  1,
		ColorsMax:  1,
		Speed:      2,
		Brightness: 255,
		Direction:  0,
		ColorMode:  2,
		Colors:     [][3]uint8{{9, 8, 7}},
	}

	p := &parser{buf: encodeMode(want, 4)}
	got := parseMode(p, 4)
	if p.err != nil {
		t.Fatalf("parseMode: %v", p.err)
	}
	if got.Name != want.Name || got.Value != want.Value || got.Flags != want.Flags {
		t.Errorf("header = %+v", got)
	}
	if got.Speed != want.Speed || got.Brightness != want.Brightness || got.ColorMode != want.ColorMode {
		t.Errorf("fields = %+v", got)
	}
	if len(got.Colors) != 1 || got.Colors[0] != [3]uint8{9, 8, 7} {
		t.Errorf("colors = %v", got.Colors)
	}
}

func TestParseControllerTruncated(t *testing.T) {
	raw := buildControllerBlock("Test", 2, nil, nil)
	if _, err := parseController(raw[:len(raw)-8], 4); err == nil {
		t.Fatal("expected an error for a truncated block")
	}
}

// TestLiveControllers lists controllers from a running OpenRGB server.
// Skipped unless OPENRGB_ADDR is set, e.g. OPENRGB_ADDR=127.0.0.1:6742.
func TestLiveControllers(t *testing.T) {
	addr := os.Getenv("OPENRGB_ADDR")
	if addr == "" {
		t.Skip("OPENRGB_ADDR not set")
	}
	client, err := Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	controllers, err := client.Controllers()
	if err != nil {
		t.Fatalf("Controllers: %v", err)
	}
	if len(controllers) == 0 {
		t.Fatal("server reported no controllers")
	}
	for _, ctrl := range controllers {
		t.Logf("#%d %q LEDs=%d zones=%d active=%d", ctrl.Index, ctrl.Name, ctrl.NumLEDs, len(ctrl.Zones), ctrl.ActiveMode)
	}
}

// TestLiveSetColor sets a device to a color from OPENRGB_SET_COLOR (RRGGBB),
// matched by OPENRGB_MATCH (default "naga pro"). Skipped unless both are set.
func TestLiveSetColor(t *testing.T) {
	addr := os.Getenv("OPENRGB_ADDR")
	hex := os.Getenv("OPENRGB_SET_COLOR")
	if addr == "" || hex == "" {
		t.Skip("OPENRGB_ADDR and OPENRGB_SET_COLOR not set")
	}
	match := strings.ToLower(os.Getenv("OPENRGB_MATCH"))
	if match == "" {
		match = "naga pro"
	}
	var color [3]uint8
	if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &color[0], &color[1], &color[2]); err != nil {
		t.Fatalf("bad color %q: %v", hex, err)
	}

	client, err := Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	controllers, err := client.Controllers()
	if err != nil {
		t.Fatalf("Controllers: %v", err)
	}
	var found *Controller
	for i := range controllers {
		if strings.Contains(strings.ToLower(controllers[i].Name), match) {
			found = &controllers[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no controller matching %q", match)
	}
	if err := client.SetCustomMode(found.Index); err != nil {
		t.Fatalf("SetCustomMode: %v", err)
	}
	colors := make([][3]uint8, found.NumLEDs)
	for i := range colors {
		colors[i] = color
	}
	if err := client.UpdateLEDs(found.Index, colors); err != nil {
		t.Fatalf("UpdateLEDs: %v", err)
	}
	t.Logf("set %q (%d LEDs) to #%s", found.Name, found.NumLEDs, hex)

	// Re-read and report the colors the server now holds for the device.
	again, err := client.Controllers()
	if err != nil {
		t.Fatalf("Controllers (re-read): %v", err)
	}
	for _, ctrl := range again {
		if ctrl.Name == found.Name {
			t.Logf("device now reports colors: %v", ctrl.Colors)
		}
	}
}
