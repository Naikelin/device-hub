// Package store cachea las últimas lecturas de batería para que el tray y el
// panel QML lean valores frescos sin re-leer el hardware cada vez.
package store

import (
	"sync"

	"device-hub/internal/device"
)

// Reading es la última lectura exitosa de un dispositivo.
type Reading struct {
	Device  device.Device
	Battery device.BatteryReading
}

// Store mantiene las lecturas, actualizadas por el poller del daemon.
type Store struct {
	reg      *device.Registry
	mu       sync.RWMutex
	readings map[string]Reading
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
