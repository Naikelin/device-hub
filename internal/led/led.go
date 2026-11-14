// Package led decides which backend drives each device's LEDs and applies
// colours and on/off through it.
package led

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"device-hub/internal/store"
)

// Controller identifies a backend that can drive a device's LEDs.
type Controller string

const (
	OpenRGB   Controller = "openrgb"
	OpenRazer Controller = "openrazer"
	Direct    Controller = "directo"
)

// Valid reports whether c is a known controller.
func (c Controller) Valid() bool {
	switch c {
	case OpenRGB, OpenRazer, Direct:
		return true
	}
	return false
}

// DefaultColor is applied when a device is switched back on before any colour
// was chosen for it.
var DefaultColor = [3]uint8{0x7a, 0xa2, 0xf7}

// Backend applies colours to the devices it knows how to drive.
type Backend interface {
	Set(deviceID string, color [3]uint8) error
	Off(deviceID string) error
}

// Choice es la configuración de LEDs de un dispositivo.
type Choice struct {
	Controller Controller `json:"controller"`
	Enabled    bool       `json:"enabled"`
}

// Config reúne las dependencias del Manager.
type Config struct {
	// Path es el archivo donde se persisten las elecciones.
	Path string
	// StatePath es el state.json que observa el panel; vacío desactiva el
	// refresco inmediato.
	StatePath string
	// Backends por controlador.
	Backends map[Controller]Backend
	// Available lista los controladores que acepta cada dispositivo.
	Available map[string][]Controller
	// Defaults es la elección inicial de cada dispositivo.
	Defaults map[string]Choice
	// Store publica el estado hacia el panel.
	Store *store.Store
}

// Manager holds the per-device choice, persists it and dispatches to backends.
type Manager struct {
	mu        sync.Mutex
	path      string
	statePath string
	backends  map[Controller]Backend
	available map[string][]Controller
	defaults  map[string]Choice
	choices   map[string]Choice
	colors    map[string][3]uint8
	store     *store.Store
}

type configFile struct {
	Devices map[string]Choice `json:"devices"`
}

// NewManager loads the saved choices and fills gaps with the defaults.
func NewManager(cfg Config) (*Manager, error) {
	m := &Manager{
		path:      cfg.Path,
		statePath: cfg.StatePath,
		backends:  cfg.Backends,
		available: cfg.Available,
		defaults:  cfg.Defaults,
		choices:   make(map[string]Choice, len(cfg.Available)),
		colors:    make(map[string][3]uint8),
		store:     cfg.Store,
	}
	if data, err := os.ReadFile(cfg.Path); err == nil {
		var saved configFile
		if json.Unmarshal(data, &saved) == nil {
			for id, choice := range saved.Devices {
				m.choices[id] = choice
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for id, allowed := range cfg.Available {
		if choice, ok := m.choices[id]; ok && containsController(allowed, choice.Controller) {
			continue
		}
		m.choices[id] = cfg.Defaults[id]
	}
	m.publish()
	return m, nil
}

// Available lists the controllers a device accepts.
func (m *Manager) Available(deviceID string) []Controller { return m.available[deviceID] }

// Choice returns the effective choice for a device.
func (m *Manager) Choice(deviceID string) Choice {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.choices[deviceID]
}

// Choices returns a copy of every device choice.
func (m *Manager) Choices() map[string]Choice {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Choice, len(m.choices))
	for id, c := range m.choices {
		out[id] = c
	}
	return out
}

// SetController switches the backend that drives a device and reapplies its
// current state through the new backend.
func (m *Manager) SetController(deviceID string, controller Controller) error {
	if !controller.Valid() {
		return fmt.Errorf("led: controlador desconocido: %s", controller)
	}
	if !containsController(m.available[deviceID], controller) {
		return fmt.Errorf("led: %s no soporta el controlador %s", deviceID, controller)
	}

	m.mu.Lock()
	choice := m.choices[deviceID]
	color, ok := m.colors[deviceID]
	if !ok {
		color = DefaultColor
	}
	m.mu.Unlock()

	if choice.Enabled {
		if err := m.apply(deviceID, controller, color, true); err != nil {
			return err
		}
	} else {
		if err := m.off(deviceID, controller); err != nil {
			return err
		}
	}

	m.mu.Lock()
	choice.Controller = controller
	m.choices[deviceID] = choice
	m.mu.Unlock()
	return m.save()
}

// SetEnabled turns a device's LEDs on or off through its controller.
func (m *Manager) SetEnabled(deviceID string, enabled bool) error {
	m.mu.Lock()
	choice := m.choices[deviceID]
	color, ok := m.colors[deviceID]
	if !ok {
		color = DefaultColor
	}
	m.mu.Unlock()

	if enabled {
		if err := m.apply(deviceID, choice.Controller, color, true); err != nil {
			return err
		}
	} else {
		if err := m.off(deviceID, choice.Controller); err != nil {
			return err
		}
	}

	m.mu.Lock()
	choice.Enabled = enabled
	m.choices[deviceID] = choice
	m.mu.Unlock()
	return m.save()
}

// Apply sets a colour on one device, enabling it.
func (m *Manager) Apply(deviceID string, color [3]uint8) error {
	m.mu.Lock()
	choice := m.choices[deviceID]
	m.mu.Unlock()

	if err := m.apply(deviceID, choice.Controller, color, true); err != nil {
		return err
	}

	m.mu.Lock()
	choice.Enabled = true
	m.choices[deviceID] = choice
	m.colors[deviceID] = color
	m.mu.Unlock()
	return m.save()
}

// Sync sets the colour on every enabled device, reporting per-device failures.
func (m *Manager) Sync(color [3]uint8) error {
	m.mu.Lock()
	choices := make(map[string]Choice, len(m.choices))
	for id, c := range m.choices {
		choices[id] = c
	}
	m.mu.Unlock()

	var (
		errs    []error
		applied []string
	)
	for _, id := range sortedKeys(choices) {
		choice := choices[id]
		if !choice.Enabled {
			continue
		}
		if err := m.apply(id, choice.Controller, color, true); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		applied = append(applied, id)
	}

	m.mu.Lock()
	for _, id := range applied {
		m.colors[id] = color
	}
	m.mu.Unlock()
	if m.store != nil {
		m.store.SetSync(color)
	}
	_ = m.save()
	return errors.Join(errs...)
}

// RestoreOff reapplies the off state of disabled devices at startup, so a
// device turned off to save battery stays off after a daemon restart. Enabled
// devices are left alone on purpose: their colour belongs to the login profile.
func (m *Manager) RestoreOff() {
	for id, choice := range m.Choices() {
		if !choice.Enabled && choice.Controller != "" {
			_ = m.off(id, choice.Controller)
		}
	}
}

func (m *Manager) apply(deviceID string, controller Controller, color [3]uint8, record bool) error {
	backend, ok := m.backends[controller]
	if !ok {
		return fmt.Errorf("led: backend no disponible: %s", controller)
	}
	if err := backend.Set(deviceID, color); err != nil {
		return err
	}
	if record && m.store != nil {
		m.store.SetRGB(deviceID, color)
	}
	return nil
}

func (m *Manager) off(deviceID string, controller Controller) error {
	backend, ok := m.backends[controller]
	if !ok {
		return fmt.Errorf("led: backend no disponible: %s", controller)
	}
	return backend.Off(deviceID)
}

func (m *Manager) save() error {
	m.mu.Lock()
	cfg := configFile{Devices: make(map[string]Choice, len(m.choices))}
	for id, c := range m.choices {
		cfg.Devices[id] = c
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(m.path, data, 0o644); err != nil {
		return err
	}
	m.publish()
	m.flush()
	return nil
}

// publish copies the choices into the store so state.json reflects them.
func (m *Manager) publish() {
	if m.store == nil {
		return
	}
	for id, choice := range m.Choices() {
		controllers := make([]string, 0, len(m.available[id]))
		for _, c := range m.available[id] {
			controllers = append(controllers, string(c))
		}
		m.store.SetLED(id, string(choice.Controller), controllers, choice.Enabled)
	}
}

// flush rewrites state.json so the panel sees the change immediately.
func (m *Manager) flush() {
	if m.store == nil || m.statePath == "" {
		return
	}
	_ = m.store.WriteState(m.statePath)
}

func containsController(list []Controller, want Controller) bool {
	for _, c := range list {
		if c == want {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]Choice) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
