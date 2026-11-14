// Package hidraw localiza y abre dispositivos HID por VID/PID + usage page.
package hidraw

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// FindDevice devuelve el path /dev/hidrawN del dispositivo cuyo uevent contiene
// vendorHex y productHex y cuyo report descriptor contiene usagePageTag.
func FindDevice(vendorHex, productHex string, usagePageTag []byte) (string, bool) {
	entries, err := filepath.Glob("/sys/class/hidraw/hidraw*")
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		uevent, err := os.ReadFile(filepath.Join(entry, "device", "uevent"))
		if err != nil {
			continue
		}
		up := strings.ToUpper(string(uevent))
		if !strings.Contains(up, vendorHex) || !strings.Contains(up, productHex) {
			continue
		}
		rdesc, err := os.ReadFile(filepath.Join(entry, "device", "report_descriptor"))
		if err != nil {
			continue
		}
		if !bytes.Contains(rdesc, usagePageTag) {
			continue
		}
		return filepath.Join("/dev", filepath.Base(entry)), true
	}
	return "", false
}

// Open abre el dispositivo hidraw en lectura/escritura.
func Open(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR, 0)
}
