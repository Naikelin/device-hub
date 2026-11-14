// Package dbus expone el daemon como servicio D-Bus (org.devicehub).
//
// El panel QML (Quickshell) consume el estado por archivo JSON, pero el daemon
// publica esta API para otras herramientas y para depuración con busctl/gdbus.
package dbus

import (
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"

	"device-hub/internal/led"
	"device-hub/internal/store"
)

const (
	BusName  = "org.devicehub"
	BusPath  = "/org/devicehub"
	BusIface = "org.devicehub.Manager"
)

// DeviceInfo es la representación D-Bus (ssssi) de un dispositivo.
type DeviceInfo struct {
	ID      string
	Name    string
	Kind    string
	Image   string
	Percent int32
}

// Service es el objeto exportado en el bus de sesión.
type Service struct {
	conn  *dbus.Conn
	store *store.Store
	led   *led.Manager
}

// NewService toma el nombre org.devicehub y exporta el objeto Manager.
func NewService(st *store.Store, ledManager *led.Manager) (*Service, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	s := &Service{conn: conn, store: st, led: ledManager}

	reply, err := conn.RequestName(BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("el nombre %s ya está en uso", BusName)
	}
	if err := conn.Export(s, BusPath, BusIface); err != nil {
		conn.Close()
		return nil, err
	}
	return s, nil
}

// GetDevices devuelve los dispositivos con su batería (a(ssssi)).
func (s *Service) GetDevices() ([]DeviceInfo, *dbus.Error) {
	readings := s.store.Readings()
	out := make([]DeviceInfo, 0, len(readings))
	for _, r := range readings {
		var percent int
		if r.Battery != nil {
			percent = r.Battery.Percent
		}
		out = append(out, DeviceInfo{
			ID:      r.Device.ID(),
			Name:    r.Device.Name(),
			Kind:    r.Device.Kind().String(),
			Image:   r.Device.Image(),
			Percent: int32(percent),
		})
	}
	return out, nil
}

// SetRGB aplica un color (0-255 por canal) a un dispositivo.
func (s *Service) SetRGB(deviceID string, r, g, b byte) *dbus.Error {
	if s.led == nil {
		return dbus.MakeFailedError(errors.New("led no disponible"))
	}
	color := [3]uint8{r, g, b}
	if err := s.led.Apply(deviceID, color); err != nil {
		return dbus.MakeFailedError(err)
	}
	s.emitRGB(deviceID, color)
	return nil
}

// SyncRGB aplica el mismo color a todos los dispositivos encendidos.
func (s *Service) SyncRGB(r, g, b byte) *dbus.Error {
	if s.led == nil {
		return dbus.MakeFailedError(errors.New("led no disponible"))
	}
	color := [3]uint8{r, g, b}
	if err := s.led.Sync(color); err != nil {
		return dbus.MakeFailedError(err)
	}
	s.emitRGB("", color)
	return nil
}

// SetLED enciende o apaga los LEDs de un dispositivo.
func (s *Service) SetLED(deviceID string, enabled bool) *dbus.Error {
	if s.led == nil {
		return dbus.MakeFailedError(errors.New("led no disponible"))
	}
	if err := s.led.SetEnabled(deviceID, enabled); err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

// SetController cambia el backend que maneja los LEDs de un dispositivo.
func (s *Service) SetController(deviceID, controller string) *dbus.Error {
	if s.led == nil {
		return dbus.MakeFailedError(errors.New("led no disponible"))
	}
	if err := s.led.SetController(deviceID, led.Controller(controller)); err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

func (s *Service) emitRGB(deviceID string, color [3]uint8) {
	_ = s.conn.Emit(BusPath, BusIface+".RGBChanged", deviceID, color[0], color[1], color[2])
}

// EmitToggle emite la señal Toggled (click en el tray).
func (s *Service) EmitToggle() {
	_ = s.conn.Emit(BusPath, BusIface+".Toggled")
}

// Close libera la conexión D-Bus.
func (s *Service) Close() {
	_ = s.conn.Close()
}
