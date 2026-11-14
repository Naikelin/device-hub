package device

// Registry mantiene la lista de dispositivos registrados.
type Registry struct {
	devices []Device
}

func NewRegistry() *Registry { return &Registry{} }

func (r *Registry) Register(d Device) { r.devices = append(r.devices, d) }

func (r *Registry) Devices() []Device { return r.devices }

// Get busca un dispositivo por ID.
func (r *Registry) Get(id string) (Device, bool) {
	for _, d := range r.devices {
		if d.ID() == id {
			return d, true
		}
	}
	return nil, false
}
