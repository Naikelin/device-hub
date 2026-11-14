// Package g435 lee la batería del Logitech G435 LIGHTSPEED.
//
// El G435 no habla HID++/Centurion: su receptor (046D:0ADF) es un puente USB
// Holtek que envuelve un protocolo serial de comandos "PA" con CRC-16/XMODEM.
// El headset reporta solo el voltaje (u16 en mV); el % se estima con la curva
// de litio 3.7 V. Protocolo: https://github.com/DsGrilo/G435-Battery-app
package g435

import (
	"encoding/binary"
	"errors"
	"os"
	"time"

	"device-hub/internal/device"
	"device-hub/internal/hidraw"
)

const (
	vendorHex    = "0000046D"
	productHex   = "00000ADF"
	usagePageTag = "\x06\x00\xff" // usage page 0xFF00

	cmdAWWrite     = 0x80
	cmdPowerSaving = 0x85
	lqGetMode      = 0xE0
	lqSetMode      = 0xE1
	otaGetBattery  = 0x31
)

var voltageCurve = [][2]int{
	{4200, 100}, {4100, 90}, {4000, 80}, {3900, 65}, {3800, 50},
	{3750, 40}, {3700, 30}, {3650, 20}, {3600, 10}, {3500, 5}, {3300, 0},
}

// G435 representa el headset Logitech G435 LIGHTSPEED.
type G435 struct{}

func (G435) ID() string        { return "logitech.g435" }
func (G435) Name() string      { return "Logitech G435" }
func (G435) Kind() device.Kind { return device.KindHeadset }
func (G435) Image() string     { return "headset.png" }

func crc16Xmodem(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// buildReport devuelve el reporte de salida de 64 bytes (sin el report ID 0x00).
func buildReport(hidCmd byte, data []byte) []byte {
	pkt := make([]byte, 65)
	pkt[1] = hidCmd
	pkt[2] = byte(len(data))
	copy(pkt[5:], data)
	crc := crc16Xmodem(pkt)
	pkt[3] = byte(crc & 0xFF)
	pkt[4] = byte(crc >> 8)
	return pkt[1:]
}

func lqCommand(cmdID byte, params ...byte) []byte {
	out := []byte{0x50, 0x41, 0x0E, 0, byte(1 + len(params)), cmdID}
	return append(out, params...)
}

func otaCommand(cmdID byte) []byte {
	return []byte{0x50, 0x41, 0x06, 0, 1, 0, cmdID}
}

type frame struct {
	typ     byte
	seq     byte
	payload []byte
}

func parseFrames(report []byte) []frame {
	if len(report) < 1 {
		return nil
	}
	size := int(report[0])
	if size > len(report)-1 {
		size = len(report) - 1
	}
	data := report[1 : 1+size]
	var frames []frame
	for i := 0; i+10 <= len(data) && data[i] == 'P' && data[i+1] == 'I'; {
		length := int(binary.LittleEndian.Uint16(data[i+8 : i+10]))
		frames = append(frames, frame{data[i+2], data[i+3], data[i+10 : i+10+length]})
		i += 10 + length
	}
	return frames
}

func lqReply(f frame, cmdID byte) (byte, bool) {
	if len(f.payload) >= 2 && f.payload[0] == cmdID {
		return f.payload[1], true
	}
	return 0, false
}

func otaReply(f frame, transmission byte) (int, bool) {
	p := f.payload
	if len(p) >= 5 && p[0] == 0x06 && (p[1]&0x7F) == transmission && p[2] == 0 {
		return int(binary.LittleEndian.Uint16(p[3:5])), true
	}
	return 0, false
}

func voltageToPercent(mv int) int {
	if mv >= voltageCurve[0][0] {
		return 100
	}
	for i := 0; i < len(voltageCurve)-1; i++ {
		vHi, pHi := voltageCurve[i][0], voltageCurve[i][1]
		vLo, pLo := voltageCurve[i+1][0], voltageCurve[i+1][1]
		if mv >= vLo {
			return pLo + (pHi-pLo)*(mv-vLo)/(vHi-vLo)
		}
	}
	return 0
}

type dongle struct {
	f            *os.File
	transmission byte
}

func openDongle() (*dongle, error) {
	path, ok := hidraw.FindDevice(vendorHex, productHex, []byte(usagePageTag))
	if !ok {
		return nil, errors.New("receptor G435 no encontrado")
	}
	f, err := hidraw.Open(path)
	if err != nil {
		return nil, err
	}
	d := &dongle{f: f}
	// La herramienta oficial apaga la economía de energía de la bridge.
	d.hidCommand(cmdPowerSaving, []byte{1})
	return d, nil
}

func (d *dongle) close() { d.f.Close() }

func (d *dongle) hidCommand(hidCmd byte, data []byte) error {
	_, err := d.f.Write(buildReport(hidCmd, data))
	if err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond)
	return nil
}

func (d *dongle) awCommand(cmd []byte) (byte, error) {
	if cmd[3] == 0 {
		d.transmission = d.transmission%127 + 1
		cmd[3] = d.transmission
	}
	return cmd[3], d.hidCommand(cmdAWWrite, cmd)
}

func (d *dongle) poll(timeout time.Duration) []frame {
	d.f.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 64)
	n, err := d.f.Read(buf)
	if err != nil {
		return nil
	}
	return parseFrames(buf[:n])
}

func (d *dongle) wait(match func(frame) (int, bool), timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, f := range d.poll(100 * time.Millisecond) {
			if v, ok := match(f); ok {
				return v, nil
			}
		}
	}
	return 0, errors.New("sin respuesta del dongle G435")
}

func (d *dongle) lqGet(cmdID byte) (byte, error) {
	if _, err := d.awCommand(lqCommand(cmdID)); err != nil {
		return 0, err
	}
	v, err := d.wait(func(f frame) (int, bool) {
		b, ok := lqReply(f, cmdID)
		return int(b), ok
	}, 3*time.Second)
	return byte(v), err
}

func (d *dongle) setRemote(remote bool) error {
	if _, err := d.awCommand(lqCommand(lqSetMode, map[bool]byte{true: 1, false: 0}[remote])); err != nil {
		return err
	}
	mode, err := d.lqGet(lqGetMode)
	if err != nil {
		return err
	}
	want := byte(0)
	if remote {
		want = 1
	}
	if mode != want {
		return errors.New("no cambió de modo el dongle")
	}
	return nil
}

func (d *dongle) readBatteryMV() (int, error) {
	wasRemote, err := d.lqGet(lqGetMode)
	if err != nil {
		return 0, err
	}
	remote := wasRemote == 1
	if !remote {
		if err := d.setRemote(true); err != nil {
			return 0, err
		}
	}
	trans, err := d.awCommand(otaCommand(otaGetBattery))
	if err != nil {
		return 0, err
	}
	mv, err := d.wait(func(f frame) (int, bool) { return otaReply(f, trans) }, 3*time.Second)
	if !remote {
		d.setRemote(false) // mejor esfuerzo al volver
	}
	return mv, err
}

func (G435) Battery() (device.BatteryReading, error) {
	d, err := openDongle()
	if err != nil {
		return device.BatteryReading{}, err
	}
	defer d.close()

	mv, err := d.readBatteryMV()
	if err != nil {
		return device.BatteryReading{}, err
	}
	return device.BatteryReading{Percent: voltageToPercent(mv), Status: "discharging"}, nil
}
