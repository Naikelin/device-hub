// Package fantech lee la batería del teclado Fantech Atom HE68 PRO y controla
// su RGB a través del receptor de 2.4 GHz.
//
// El receptor (0x0C45:0xFEFE) expone una interfaz HID sin report ID con reports
// de 32 bytes (usage page 0xFF60). La batería se pide con "AA 10 18" + 29 ceros
// y vuelve como "55 10 18" con el % en BCD en el byte 11. El RGB usa el modo
// custom y una tabla de 128 LEDs enviada en 22 reports de seis LEDs cada uno.
package fantech

import (
	"errors"
	"os"
	"sync"
	"time"

	"device-hub/internal/device"
	"device-hub/internal/hidraw"
)

const (
	vendorHex    = "00000C45"
	productHex   = "0000FEFE"
	usagePageTag = "\x06\x60\xff" // usage page 0xFF60 (interfaz de 32 bytes)

	query          = "\xAA\x10\x18" + "\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"
	responsePrefix = "\x55\x10\x18"
	levelOffset    = 11

	rgbReportLen  = 32
	rgbTableSize  = 128
	rgbChunkLEDs  = 6
	rgbChunkDelay = 26 * time.Millisecond
	rgbModeDelay  = 50 * time.Millisecond
)

// mu serializa el acceso al receptor: la lectura de batería y el RGB comparten
// el mismo hidraw y no deben pisarse.
var mu sync.Mutex

// customModePayload selecciona el modo Custom-lighting (capturado de la app
// oficial). El RGB se puede actualizar aunque otro efecto siga visible, por eso
// se activa Custom antes de cada tabla.
var customModePayload = []byte{
	0xAA, 0x23, 0x10, 0x00, 0x00, 0x00, 0x01, 0x00,
	0x80, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00,
	0x01, 0x05, 0x04, 0x00, 0x00, 0x00, 0xAA, 0x55,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
}

// DeviceID es el identificador del teclado en el hub.
const DeviceID = "fantech.atom-he68-pro"

// Fantech representa el teclado Atom HE68 PRO.
type Fantech struct{}

func (Fantech) ID() string        { return DeviceID }
func (Fantech) Name() string      { return "Fantech Atom HE68 PRO" }
func (Fantech) Kind() device.Kind { return device.KindKeyboard }
func (Fantech) Image() string     { return "keyboard.svg" }

func (Fantech) Battery() (device.BatteryReading, error) {
	mu.Lock()
	defer mu.Unlock()

	path, ok := hidraw.FindDevice(vendorHex, productHex, []byte(usagePageTag))
	if !ok {
		return device.BatteryReading{}, errors.New("receptor Fantech no encontrado")
	}
	f, err := hidraw.Open(path)
	if err != nil {
		return device.BatteryReading{}, err
	}
	defer f.Close()

	if _, err := f.Write([]byte(query)); err != nil {
		return device.BatteryReading{}, err
	}
	f.SetReadDeadline(time.Now().Add(1 * time.Second))

	buf := make([]byte, 32)
	n, err := f.Read(buf)
	if err != nil {
		return device.BatteryReading{}, err
	}
	buf = buf[:n]

	if len(buf) <= levelOffset || string(buf[:3]) != responsePrefix {
		return device.BatteryReading{}, errors.New("respuesta inesperada del receptor")
	}
	encoded := buf[levelOffset]
	percent := decodeBCD(encoded)
	if percent < 0 || percent > 100 {
		return device.BatteryReading{}, errors.New("porcentaje BCD inválido")
	}

	return device.BatteryReading{Percent: percent, Status: "discharging"}, nil
}

// ---- RGB (receptor 2.4 GHz) ----------------------------------------------

// SetAll pinta los 128 LEDs del teclado con un solo color. Activa el modo
// Custom y envía la tabla en 22 reports de seis LEDs.
func SetAll(color [3]uint8) error {
	mu.Lock()
	defer mu.Unlock()

	path, ok := hidraw.FindDevice(vendorHex, productHex, []byte(usagePageTag))
	if !ok {
		return errors.New("receptor Fantech no encontrado")
	}
	f, err := hidraw.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(customModePayload); err != nil {
		return err
	}
	time.Sleep(rgbModeDelay)

	for start := 0; start < rgbTableSize; start += rgbChunkLEDs {
		count := min(rgbChunkLEDs, rgbTableSize-start)
		payload := make([]byte, rgbReportLen)
		payload[0] = 0xAA
		payload[1] = 0x24
		payload[2] = byte(count * 4)
		offset := start * 4
		payload[3] = byte(offset)
		payload[4] = byte(offset >> 8)
		payload[5] = byte(offset >> 16)
		if start+count == rgbTableSize {
			payload[6] = 1
		}
		cursor := 8
		for address := start; address < start+count; address++ {
			payload[cursor] = byte(address)
			payload[cursor+1] = color[0]
			payload[cursor+2] = color[1]
			payload[cursor+3] = color[2]
			cursor += 4
		}
		if _, err := f.Write(payload); err != nil {
			return err
		}
		drain(f)
		time.Sleep(rgbChunkDelay)
	}
	return nil
}

// drain descarta la respuesta del receptor sin bloquear.
func drain(f *os.File) {
	_ = f.SetReadDeadline(time.Now().Add(5 * time.Millisecond))
	buf := make([]byte, rgbReportLen)
	_, _ = f.Read(buf)
	_ = f.SetReadDeadline(time.Time{})
}

// decodeBCD decodifica un byte BCD empaquetado (0x92 -> 92).
func decodeBCD(encoded byte) int {
	return int(encoded>>4)*10 + int(encoded&0x0F)
}
