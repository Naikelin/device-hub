// Package dbus expone el daemon como servicio D-Bus (org.devicehub).
//
// El panel QML (Quickshell) consume esta API para renderizar las cards de los
// dispositivos. La señal Toggled se emite al hacer click en el icono del tray.
package dbus

import (
	"fmt"

	"github.com/godbus/dbus/v5"

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
}

// NewService toma el nombre org.devicehub y exporta el objeto Manager.
func NewService(store *store.Store) (*Service, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	s := &Service{conn: conn, store: store}

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
		out = append(out, DeviceInfo{
			ID:      r.Device.ID(),
			Name:    r.Device.Name(),
			Kind:    r.Device.Kind().String(),
			Image:   r.Device.Image(),
			Percent: int32(r.Battery.Percent),
		})
	}
	return out, nil
}

// EmitToggle emite la señal Toggled (click en el tray).
func (s *Service) EmitToggle() {
	_ = s.conn.Emit(BusPath, BusIface+".Toggled")
}

// Close libera la conexión D-Bus.
func (s *Service) Close() {
	_ = s.conn.Close()
}
