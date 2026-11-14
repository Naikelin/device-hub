package device

// Registry mantiene la lista de dispositivos registrados.
type Registry struct {
	devices []Device
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Register(d Device) { r.devices = append(r.devices, d) }

func (r *Registry) Devices() []Device { return r.devices }

func (r *Registry) RGBDevices() []RGBDevice {
	var out []RGBDevice
	for _, d := range r.devices {
		if rgb, ok := d.(RGBDevice); ok {
			out = append(out, rgb)
		}
	}
	return out
}
