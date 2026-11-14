// Package device define el modelo unificado de dispositivos y el registry.
package device

// Kind clasifica un periférico.
type Kind int

const (
	KindMouse Kind = iota
	KindKeyboard
	KindHeadset
)

func (k Kind) String() string {
	switch k {
	case KindMouse:
		return "mouse"
	case KindKeyboard:
		return "keyboard"
	case KindHeadset:
		return "headset"
	default:
		return "unknown"
	}
}

// BatteryReading es una instantánea de la batería de un dispositivo.
type BatteryReading struct {
	Percent int    // 0-100
	Status  string // "discharging" | "charging" | "fully charged" | "unknown"
}

// Zone es una región LED controlable por separado.
type Zone struct {
	ID   int
	Name string
}

// Device es la interfaz mínima que todo periférico implementa.
type Device interface {
	ID() string
	Name() string
	Kind() Kind
	Image() string // asset de la foto
	Battery() (BatteryReading, error)
}

// RGBDevice extiende Device con control de color por zona.
type RGBDevice interface {
	Device
	Zones() []Zone
	SetZone(zoneID int, color [3]uint8) error
	SetAll(color [3]uint8) error
}
