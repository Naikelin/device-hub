// Package razer lee la batería de dispositivos Razer vía sysfs de openrazer y
// controla su RGB a través del servidor SDK de OpenRGB.
package razer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"device-hub/internal/device"
	"device-hub/internal/rgb"
)

// Razer representa un mouse Razer (Naga Pro) con soporte openrazer.
type Razer struct{}

func (Razer) ID() string        { return "razer.naga-pro" }
func (Razer) Name() string      { return "Razer Naga Pro" }
func (Razer) Kind() device.Kind { return device.KindMouse }
func (Razer) Image() string     { return "mouse.svg" }

func findChargeDir() (string, error) {
	entries, _ := filepath.Glob("/sys/bus/hid/drivers/razermouse/*")
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(e, "charge_level")); err == nil {
			return e, nil
		}
	}
	return "", fmt.Errorf("dispositivo razermouse no encontrado")
}

func (Razer) Battery() (device.BatteryReading, error) {
	dir, err := findChargeDir()
	if err != nil {
		return device.BatteryReading{}, err
	}

	raw, err := os.ReadFile(filepath.Join(dir, "charge_level"))
	if err != nil {
		return device.BatteryReading{}, err
	}
	level, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return device.BatteryReading{}, err
	}
	// charge_level es 0-255; el daemon openrazer lo escala a 0-100.
	percent := level * 100 / 255

	status := "discharging"
	if cs, err := os.ReadFile(filepath.Join(dir, "charge_status")); err == nil {
		switch strings.TrimSpace(string(cs)) {
		case "1":
			status = "charging"
		case "2":
			status = "fully charged"
		}
	}

	return device.BatteryReading{Percent: percent, Status: status}, nil
}

// ---- RGB (OpenRGB SDK) ---------------------------------------------------

const (
	openrgbAddrEnv = "DEVICE_HUB_OPENRGB"
	openrgbAddr    = "127.0.0.1:6742"
	openrgbMatch   = "naga pro"
)

func openrgbAddress() string {
	if v := os.Getenv(openrgbAddrEnv); v != "" {
		return v
	}
	return openrgbAddr
}

// openrgbController abre el servidor y localiza el controlador del Naga Pro.
func openrgbController() (*rgb.Client, rgb.Controller, error) {
	client, err := rgb.Dial(openrgbAddress())
	if err != nil {
		return nil, rgb.Controller{}, err
	}
	controllers, err := client.Controllers()
	if err != nil {
		client.Close()
		return nil, rgb.Controller{}, err
	}
	for _, ctrl := range controllers {
		if strings.Contains(strings.ToLower(ctrl.Name), openrgbMatch) {
			return client, ctrl, nil
		}
	}
	client.Close()
	return nil, rgb.Controller{}, errors.New("razer: OpenRGB no reporta el Naga Pro")
}

func (Razer) Zones() []device.Zone {
	client, ctrl, err := openrgbController()
	if err != nil {
		return nil
	}
	defer client.Close()
	zones := make([]device.Zone, len(ctrl.Zones))
	for i, z := range ctrl.Zones {
		zones[i] = device.Zone{ID: i, Name: z.Name}
	}
	return zones
}

func (Razer) SetZone(zoneID int, color [3]uint8) error {
	client, ctrl, err := openrgbController()
	if err != nil {
		return err
	}
	defer client.Close()
	if zoneID < 0 || zoneID >= len(ctrl.Zones) {
		return fmt.Errorf("razer: zona %d fuera de rango", zoneID)
	}
	if err := client.SetCustomMode(ctrl.Index); err != nil {
		return err
	}
	colors := make([][3]uint8, ctrl.Zones[zoneID].NumLEDs)
	for i := range colors {
		colors[i] = color
	}
	return client.UpdateZoneLEDs(ctrl.Index, uint32(zoneID), colors)
}

func (Razer) SetAll(color [3]uint8) error {
	client, ctrl, err := openrgbController()
	if err != nil {
		return err
	}
	defer client.Close()
	if ctrl.NumLEDs <= 0 {
		return errors.New("razer: controlador sin LEDs")
	}
	if err := client.SetCustomMode(ctrl.Index); err != nil {
		return err
	}
	colors := make([][3]uint8, ctrl.NumLEDs)
	for i := range colors {
		colors[i] = color
	}
	return client.UpdateLEDs(ctrl.Index, colors)
}
