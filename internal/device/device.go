// Package device define el modelo unificado de dispositivos y el registry.
package device

// Kind clasifica un periférico o componente con LEDs.
type Kind int

const (
	KindMouse Kind = iota
	KindKeyboard
	KindHeadset
	KindMotherboard
	KindMemory
	KindGPU
	KindSpeaker
	KindMicrophone
	KindLighting
)

func (k Kind) String() string {
	switch k {
	case KindMouse:
		return "mouse"
	case KindKeyboard:
		return "keyboard"
	case KindHeadset:
		return "headset"
	case KindMotherboard:
		return "motherboard"
	case KindMemory:
		return "memory"
	case KindGPU:
		return "gpu"
	case KindSpeaker:
		return "speaker"
	case KindMicrophone:
		return "microphone"
	default:
		return "lighting"
	}
}

// Image devuelve el asset del panel que representa este tipo.
func (k Kind) Image() string {
	switch k {
	case KindMouse:
		return "mouse.svg"
	case KindKeyboard:
		return "keyboard.svg"
	case KindHeadset:
		return "headset.svg"
	case KindMotherboard:
		return "motherboard.svg"
	case KindMemory:
		return "memory.svg"
	case KindGPU:
		return "gpu.svg"
	case KindSpeaker:
		return "speaker.svg"
	case KindMicrophone:
		return "microphone.svg"
	default:
		return "unknown.svg"
	}
}

// BatteryReading es una instantánea de la batería de un dispositivo.
type BatteryReading struct {
	Percent int    // 0-100
	Status  string // "discharging" | "charging" | "fully charged" | "unknown"
}

// Device es la identidad mínima de un dispositivo con LEDs.
type Device interface {
	ID() string
	Name() string
	Kind() Kind
	Image() string // asset de la foto
}

// BatteryDevice es un Device que además reporta batería.
type BatteryDevice interface {
	Device
	Battery() (BatteryReading, error)
}
