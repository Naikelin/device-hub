// Package razer lee la batería de dispositivos Razer vía sysfs de openrazer.
package razer

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"device-hub/internal/device"
)

// Razer representa un mouse Razer (Naga Pro) con soporte openrazer.
type Razer struct{}

func (Razer) ID() string        { return "razer.naga-pro" }
func (Razer) Name() string      { return "Razer Naga Pro" }
func (Razer) Kind() device.Kind { return device.KindMouse }
func (Razer) Image() string     { return "mouse.png" }

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
