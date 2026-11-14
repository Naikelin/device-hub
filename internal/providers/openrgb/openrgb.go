// Package openrgb expone como dispositivos los controladores del servidor SDK
// de OpenRGB que ningún proveedor específico reclama.
package openrgb

import (
	"os"
	"strings"

	"device-hub/internal/device"
	"device-hub/internal/rgb"
)

const defaultAddr = "127.0.0.1:6742"

// Address devuelve la dirección del servidor SDK (DEVICE_HUB_OPENRGB la
// sobreescribe).
func Address() string {
	if v := os.Getenv("DEVICE_HUB_OPENRGB"); v != "" {
		return v
	}
	return defaultAddr
}

// Device es un controlador de OpenRGB registrado en el hub.
type Device struct {
	id, name string
	kind     device.Kind
}

func (d Device) ID() string        { return d.id }
func (d Device) Name() string      { return d.name }
func (d Device) Kind() device.Kind { return d.kind }
func (d Device) Image() string     { return d.kind.Image() }

// Enumerate lista los controladores del SDK. Los que coinciden con una entrada
// de claimed no se registran como dispositivos, pero sí se mapean a su dueño
// para que el backend OpenRGB pueda manejarlos si el dueño elige ese
// controlador. Devuelve los dispositivos y el mapa deviceID -> nombre.
func Enumerate(addr string, claimed map[string]string) ([]device.Device, map[string]string, error) {
	client, err := rgb.Dial(addr)
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	ctrls, err := client.Controllers()
	if err != nil {
		return nil, nil, err
	}

	devices := make([]device.Device, 0, len(ctrls))
	names := make(map[string]string, len(ctrls))
	for _, ctrl := range ctrls {
		if owner, ok := claimedBy(claimed, ctrl.Name); ok {
			names[owner] = ctrl.Name
			continue
		}
		id := "openrgb." + Slug(ctrl.Name)
		kind := KindFor(ctrl.Name)
		devices = append(devices, Device{id: id, name: ctrl.Name, kind: kind})
		names[id] = ctrl.Name
	}
	return devices, names, nil
}

// Slug convierte un nombre de controlador en un identificador estable.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// KindFor deduce el tipo de dispositivo a partir del nombre del controlador.
func KindFor(name string) device.Kind {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "leviathan"):
		return device.KindSpeaker
	case strings.Contains(n, "quadcast"):
		return device.KindMicrophone
	case strings.Contains(n, "trident"), strings.Contains(n, "g.skill"),
		strings.Contains(n, "vengeance"), strings.Contains(n, "dominato"),
		strings.Contains(n, "hyperx"), strings.Contains(n, "fury"):
		return device.KindMemory
	case strings.Contains(n, "geforce"), strings.Contains(n, "radeon"),
		strings.Contains(n, "rtx"), strings.Contains(n, "gtx"):
		return device.KindGPU
	case strings.Contains(n, "motherboard"), strings.Contains(n, "strix"),
		strings.Contains(n, "asus"), strings.Contains(n, "msi"),
		strings.Contains(n, "gigabyte"), strings.Contains(n, "asrock"):
		return device.KindMotherboard
	}
	return device.KindLighting
}

func claimedBy(claimed map[string]string, name string) (string, bool) {
	n := strings.ToLower(name)
	for match, owner := range claimed {
		if strings.Contains(n, strings.ToLower(match)) {
			return owner, true
		}
	}
	return "", false
}
