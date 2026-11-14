package led

import (
	"fmt"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"

	"device-hub/internal/providers/fantech"
	"device-hub/internal/rgb"
)

// ---- OpenRGB SDK ---------------------------------------------------------

// OpenRGBBackend drives controllers through the OpenRGB SDK server. The
// deviceID -> controller name map is built once at startup from the same
// enumeration that registers the devices.
type OpenRGBBackend struct {
	addr  string
	names map[string]string

	mu     sync.Mutex
	client *rgb.Client
	ctrls  map[string]rgb.Controller // lowercase controller name
}

// NewOpenRGBBackend builds a backend for the SDK server at addr.
func NewOpenRGBBackend(addr string, names map[string]string) *OpenRGBBackend {
	return &OpenRGBBackend{addr: addr, names: names}
}

// Set switches the controller to its Direct mode and pushes one colour per LED.
func (b *OpenRGBBackend) Set(deviceID string, color [3]uint8) error {
	name := b.names[deviceID]
	if name == "" {
		return fmt.Errorf("led: OpenRGB no conoce %s", deviceID)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	ctrl, err := b.resolve(name)
	if err != nil {
		return err
	}
	if ctrl.NumLEDs <= 0 {
		return fmt.Errorf("led: OpenRGB reporta %q sin LEDs", name)
	}
	if err := b.client.SetCustomMode(ctrl.Index); err != nil {
		b.reset()
		return fmt.Errorf("led: OpenRGB modo directo %q: %w", name, err)
	}
	colors := make([][3]uint8, ctrl.NumLEDs)
	for i := range colors {
		colors[i] = color
	}
	if err := b.client.UpdateLEDs(ctrl.Index, colors); err != nil {
		b.reset()
		return fmt.Errorf("led: OpenRGB color %q: %w", name, err)
	}
	return nil
}

// Off paints every LED black, which is the closest the SDK gets to off.
func (b *OpenRGBBackend) Off(deviceID string) error {
	return b.Set(deviceID, [3]uint8{})
}

// resolve finds a controller by name, reconnecting once when the cached list is
// stale (the server re-enumerates when devices are added or removed).
func (b *OpenRGBBackend) resolve(name string) (rgb.Controller, error) {
	if err := b.ensure(); err != nil {
		return rgb.Controller{}, err
	}
	if ctrl, ok := b.ctrls[strings.ToLower(name)]; ok {
		return ctrl, nil
	}
	b.reset()
	if err := b.ensure(); err != nil {
		return rgb.Controller{}, err
	}
	ctrl, ok := b.ctrls[strings.ToLower(name)]
	if !ok {
		return rgb.Controller{}, fmt.Errorf("led: OpenRGB no reporta %q", name)
	}
	return ctrl, nil
}

func (b *OpenRGBBackend) ensure() error {
	if b.client != nil {
		return nil
	}
	client, err := rgb.Dial(b.addr)
	if err != nil {
		return err
	}
	ctrls, err := client.Controllers()
	if err != nil {
		client.Close()
		return err
	}
	index := make(map[string]rgb.Controller, len(ctrls))
	for _, c := range ctrls {
		index[strings.ToLower(c.Name)] = c
	}
	b.client = client
	b.ctrls = index
	return nil
}

func (b *OpenRGBBackend) reset() {
	if b.client != nil {
		_ = b.client.Close()
	}
	b.client = nil
	b.ctrls = nil
}

// ---- openrazer D-Bus -----------------------------------------------------

const (
	razerBusName  = "org.razer"
	razerPathBase = "/org/razer/device/"
)

// OpenRazerBackend drives Razer devices through the openrazer daemon, which is
// what keeps them in step with the rest of the Razer ecosystem.
type OpenRazerBackend struct {
	serials map[string]string // deviceID -> USB serial
}

// NewOpenRazerBackend builds a backend from the deviceID -> serial map.
func NewOpenRazerBackend(serials map[string]string) *OpenRazerBackend {
	return &OpenRazerBackend{serials: serials}
}

// Set applies a static colour.
func (b *OpenRazerBackend) Set(deviceID string, color [3]uint8) error {
	obj, err := b.object(deviceID)
	if err != nil {
		return err
	}
	call := obj.Call("razer.device.lighting.chroma.setStatic", 0, color[0], color[1], color[2])
	return call.Err
}

// Off clears the effect with setNone.
func (b *OpenRazerBackend) Off(deviceID string) error {
	obj, err := b.object(deviceID)
	if err != nil {
		return err
	}
	call := obj.Call("razer.device.lighting.chroma.setNone", 0)
	return call.Err
}

func (b *OpenRazerBackend) object(deviceID string) (dbus.BusObject, error) {
	serial := b.serials[deviceID]
	if serial == "" {
		return nil, fmt.Errorf("led: openrazer no conoce %s", deviceID)
	}
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	return conn.Object(razerBusName, dbus.ObjectPath(razerPathBase+serial)), nil
}

// ---- direct hidraw -------------------------------------------------------

// DirectBackend talks to the Fantech receiver over hidraw, bypassing both
// daemons.
type DirectBackend struct{}

// Set sends an all-colour table to the keyboard.
func (DirectBackend) Set(deviceID string, color [3]uint8) error {
	if deviceID != fantech.DeviceID {
		return fmt.Errorf("led: el backend directo solo maneja %s", fantech.DeviceID)
	}
	return fantech.SetAll(color)
}

// Off sends an all-black table.
func (DirectBackend) Off(deviceID string) error {
	return DirectBackend{}.Set(deviceID, [3]uint8{})
}
