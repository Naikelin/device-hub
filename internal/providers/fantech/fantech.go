// Package fantech lee la batería del teclado Fantech Atom HE68 PRO.
//
// El receptor (0x0C45:0xFEFE) acepta una request de 32 bytes "AA 10 18" + 29
// ceros. La respuesta "55 10 18" lleva el % como BCD empaquetado en el byte 11.
package fantech

import (
	"errors"
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
)

// Fantech representa el teclado Atom HE68 PRO.
type Fantech struct{}

func (Fantech) ID() string        { return "fantech.atom-he68-pro" }
func (Fantech) Name() string      { return "Fantech Atom HE68 PRO" }
func (Fantech) Kind() device.Kind { return device.KindKeyboard }
func (Fantech) Image() string     { return "keyboard.png" }

func (Fantech) Battery() (device.BatteryReading, error) {
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

// decodeBCD decodifica un byte BCD empaquetado (0x92 -> 92).
func decodeBCD(encoded byte) int {
	return int(encoded>>4)*10 + int(encoded&0x0F)
}
