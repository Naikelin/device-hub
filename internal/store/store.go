// Package store cachea las últimas lecturas de batería y el estado de LEDs y
// publica un archivo de estado JSON que el panel QML observa con
// Quickshell.Io.FileView.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"device-hub/internal/device"
)

// Reading es la última lectura exitosa de un dispositivo. Battery es nil para
// los dispositivos sin batería (motherboard, RAM, GPU, altavoz, micrófono).
type Reading struct {
	Device  device.Device
	Battery *device.BatteryReading
}

// LEDState es la configuración de LEDs de un dispositivo, replicada desde el
// manager para que el panel la vea.
type LEDState struct {
	Controller  string
	Controllers []string
	Enabled     bool
}

// Store mantiene las lecturas y el estado de LEDs, actualizados por el daemon.
type Store struct {
	reg      *device.Registry
	mu       sync.RWMutex
	writeMu  sync.Mutex
	readings map[string]Reading
	led      map[string]LEDState
	rgb      map[string][3]uint8
	sync     *[3]uint8

	profiles      []string
	activeProfile string
}

func New(reg *device.Registry) *Store {
	return &Store{
		reg:      reg,
		readings: make(map[string]Reading),
		led:      make(map[string]LEDState),
		rgb:      make(map[string][3]uint8),
	}
}

// Devices devuelve los dispositivos registrados (para la estructura del menú).
func (s *Store) Devices() []device.Device { return s.reg.Devices() }

// Refresh lee la batería de todos los dispositivos y actualiza el cache.
func (s *Store) Refresh() {
	readings := make(map[string]Reading)
	for _, d := range s.reg.Devices() {
		r := Reading{Device: d}
		if bd, ok := d.(device.BatteryDevice); ok {
			if b, err := bd.Battery(); err == nil {
				reading := b
				r.Battery = &reading
			}
		}
		readings[d.ID()] = r
	}
	s.mu.Lock()
	s.readings = readings
	s.mu.Unlock()
}

// Readings devuelve las lecturas cacheadas (sin re-leer el hardware). Incluye
// los dispositivos sin batería.
func (s *Store) Readings() []Reading {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Reading, 0, len(s.readings))
	for _, d := range s.reg.Devices() {
		if r, ok := s.readings[d.ID()]; ok {
			out = append(out, r)
		}
	}
	return out
}

// SetLED registra el controlador elegido para un dispositivo.
func (s *Store) SetLED(deviceID, controller string, controllers []string, enabled bool) {
	if controllers == nil {
		controllers = []string{}
	}
	s.mu.Lock()
	s.led[deviceID] = LEDState{Controller: controller, Controllers: controllers, Enabled: enabled}
	s.mu.Unlock()
}

// SetRGB registra el último color aplicado a un dispositivo.
func (s *Store) SetRGB(deviceID string, color [3]uint8) {
	s.mu.Lock()
	s.rgb[deviceID] = color
	s.mu.Unlock()
}

// SetSync registra el último color de sincronización.
func (s *Store) SetSync(color [3]uint8) {
	s.mu.Lock()
	c := color
	s.sync = &c
	s.mu.Unlock()
}

// ClearColors olvida los colores conocidos, por ejemplo tras cargar un
// profile de OpenRGB que cambió el hardware por fuera del hub.
func (s *Store) ClearColors() {
	s.mu.Lock()
	s.rgb = make(map[string][3]uint8)
	s.sync = nil
	s.mu.Unlock()
}

// SetProfiles registra la lista de profiles de OpenRGB disponible.
func (s *Store) SetProfiles(names []string) {
	if names == nil {
		names = []string{}
	}
	s.mu.Lock()
	s.profiles = names
	s.mu.Unlock()
}

// SetActiveProfile registra el último profile cargado desde el hub.
func (s *Store) SetActiveProfile(name string) {
	s.mu.Lock()
	s.activeProfile = name
	s.mu.Unlock()
}

// WriteState escribe el estado como JSON.
//
// Se escribe en sitio (sin rename) a propósito: el panel observa el archivo con
// inotify y un rename reemplaza el inodo, cortando el watch. El panel tolera una
// lectura a medias manteniendo el último estado válido.
func (s *Store) WriteState(path string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	type ledJSON struct {
		Controller  string   `json:"controller"`
		Controllers []string `json:"controllers"`
		Enabled     bool     `json:"enabled"`
	}
	type devJSON struct {
		ID         string  `json:"id"`
		Name       string  `json:"name"`
		Kind       string  `json:"kind"`
		Image      string  `json:"image"`
		HasBattery bool    `json:"hasBattery"`
		Percent    int     `json:"percent"`
		Status     string  `json:"status"`
		LED        ledJSON `json:"led"`
	}

	s.mu.RLock()
	devs := make([]devJSON, 0, len(s.readings))
	for _, d := range s.reg.Devices() {
		r, ok := s.readings[d.ID()]
		if !ok {
			continue
		}
		entry := devJSON{
			ID:    r.Device.ID(),
			Name:  r.Device.Name(),
			Kind:  r.Device.Kind().String(),
			Image: r.Device.Image(),
		}
		if r.Battery != nil {
			entry.HasBattery = true
			entry.Percent = r.Battery.Percent
			entry.Status = r.Battery.Status
		}
		if led, ok := s.led[d.ID()]; ok {
			entry.LED = ledJSON{
				Controller:  led.Controller,
				Controllers: led.Controllers,
				Enabled:     led.Enabled,
			}
		} else {
			entry.LED = ledJSON{Controllers: []string{}}
		}
		devs = append(devs, entry)
	}

	rgb := make(map[string]string, len(s.rgb))
	for id, c := range s.rgb {
		rgb[id] = hexColor(c)
	}
	var syncColor *string
	if s.sync != nil {
		h := hexColor(*s.sync)
		syncColor = &h
	}
	profiles := append([]string(nil), s.profiles...)
	activeProfile := s.activeProfile
	s.mu.RUnlock()

	state := struct {
		Devices       []devJSON         `json:"devices"`
		RGB           map[string]string `json:"rgb,omitempty"`
		Sync          *string           `json:"sync,omitempty"`
		Profiles      []string          `json:"profiles,omitempty"`
		ActiveProfile string            `json:"activeProfile,omitempty"`
	}{Devices: devs, RGB: rgb, Sync: syncColor, Profiles: profiles, ActiveProfile: activeProfile}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func hexColor(c [3]uint8) string {
	return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2])
}
