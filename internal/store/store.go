// Package store cachea las últimas lecturas de batería y el último color RGB y
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

// Reading es la última lectura exitosa de un dispositivo.
type Reading struct {
	Device  device.Device
	Battery device.BatteryReading
}

// Store mantiene las lecturas y el estado RGB, actualizados por el daemon.
type Store struct {
	reg      *device.Registry
	mu       sync.RWMutex
	writeMu  sync.Mutex
	readings map[string]Reading
	rgb      map[string][3]uint8
	sync     *[3]uint8
}

func New(reg *device.Registry) *Store {
	return &Store{
		reg:      reg,
		readings: make(map[string]Reading),
		rgb:      make(map[string][3]uint8),
	}
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

// WriteState escribe el estado como JSON.
//
// Se escribe en sitio (sin rename) a propósito: el panel observa el archivo con
// inotify y un rename reemplaza el inodo, cortando el watch. El panel tolera una
// lectura a medias manteniendo el último estado válido.
func (s *Store) WriteState(path string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	type devJSON struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Image   string `json:"image"`
		Percent int    `json:"percent"`
		Status  string `json:"status"`
		RGB     bool   `json:"rgb"`
	}
	var devs []devJSON
	for _, r := range s.Readings() {
		_, isRGB := r.Device.(device.RGBDevice)
		devs = append(devs, devJSON{
			ID:      r.Device.ID(),
			Name:    r.Device.Name(),
			Kind:    r.Device.Kind().String(),
			Image:   r.Device.Image(),
			Percent: r.Battery.Percent,
			Status:  r.Battery.Status,
			RGB:     isRGB,
		})
	}

	s.mu.RLock()
	rgb := make(map[string]string, len(s.rgb))
	for id, c := range s.rgb {
		rgb[id] = hexColor(c)
	}
	var syncColor *string
	if s.sync != nil {
		h := hexColor(*s.sync)
		syncColor = &h
	}
	s.mu.RUnlock()

	state := struct {
		Devices []devJSON         `json:"devices"`
		RGB     map[string]string `json:"rgb,omitempty"`
		Sync    *string           `json:"sync,omitempty"`
	}{Devices: devs, RGB: rgb, Sync: syncColor}

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
