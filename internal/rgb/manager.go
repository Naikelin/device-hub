package rgb

import (
	"errors"
	"fmt"

	"device-hub/internal/device"
	"device-hub/internal/store"
)

// Manager applies colors to the registered RGB devices and records the result.
type Manager struct {
	reg       *device.Registry
	store     *store.Store
	statePath string
}

func NewManager(reg *device.Registry, st *store.Store, statePath string) *Manager {
	return &Manager{reg: reg, store: st, statePath: statePath}
}

// Set applies color to one device.
func (m *Manager) Set(deviceID string, color [3]uint8) error {
	for _, d := range m.reg.RGBDevices() {
		if d.ID() != deviceID {
			continue
		}
		if err := d.SetAll(color); err != nil {
			return err
		}
		m.store.SetRGB(deviceID, color)
		m.persist()
		return nil
	}
	return fmt.Errorf("rgb: dispositivo RGB no encontrado: %s", deviceID)
}

// Sync applies color to every RGB-capable device, reporting per-device failures.
func (m *Manager) Sync(color [3]uint8) error {
	var errs []error
	for _, d := range m.reg.RGBDevices() {
		if err := d.SetAll(color); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.ID(), err))
			continue
		}
		m.store.SetRGB(d.ID(), color)
	}
	m.store.SetSync(color)
	m.persist()
	return errors.Join(errs...)
}

func (m *Manager) persist() {
	if m.statePath != "" {
		_ = m.store.WriteState(m.statePath)
	}
}
