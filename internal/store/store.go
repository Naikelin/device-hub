// Package store cachea las últimas lecturas de batería y publica un archivo de
// estado JSON que el panel QML observa (Quickshell.Io.FileView). También lleva
// un contador "toggle" que se incrementa al hacer click en el tray.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"device-hub/internal/device"
)

// Reading es la última lectura exitosa de un dispositivo.
type Reading struct {
	Device  device.Device
	Battery device.BatteryReading
}

// Store mantiene las lecturas y el contador de toggle.
type Store struct {
	reg      *device.Registry
	mu       sync.RWMutex
	readings map[string]Reading
	toggle   atomic.Int64
}

func New(reg *device.Registry) *Store {
	return &Store{reg: reg, readings: make(map[string]Reading)}
}

// Devices devuelve los dispositivos registrados (para la estructura del menú).
func (s *Store) Devices() []device.Device { return s.reg.Devices() }

// Refresh lee la batería de todos los dispositivos y actualiza el cache.
func (s *Store) Refresh() {
	readings := make(map[string]Reading)
	for _, d := range s.reg.Devices() {
		if b, err := d.Battery(); err == nil {
			readings[d.ID()] = Reading{Device: d, Battery: b}
		}
	}
	s.mu.Lock()
	s.readings = readings
	s.mu.Unlock()
}

// Readings devuelve las lecturas cacheadas (sin re-leer el hardware).
func (s *Store) Readings() []Reading {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Reading
	for _, d := range s.reg.Devices() {
		if r, ok := s.readings[d.ID()]; ok {
			out = append(out, r)
		}
	}
	return out
}

// Toggle incrementa el contador (click en el tray).
func (s *Store) Toggle() { s.toggle.Add(1) }

// WriteState escribe el estado (dispositivos + toggle) como JSON, atómicamente.
func (s *Store) WriteState(path string) error {
	type devJSON struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Image   string `json:"image"`
		Percent int    `json:"percent"`
	}
	var devs []devJSON
	for _, r := range s.Readings() {
		devs = append(devs, devJSON{
			ID:      r.Device.ID(),
			Name:    r.Device.Name(),
			Kind:    r.Device.Kind().String(),
			Image:   r.Device.Image(),
			Percent: r.Battery.Percent,
		})
	}
	state := struct {
		Devices []devJSON `json:"devices"`
		Toggle  int64     `json:"toggle"`
	}{devs, s.toggle.Load()}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
