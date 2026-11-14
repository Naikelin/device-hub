package openrgb

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"device-hub/internal/rgb"
)

// Profiles devuelve los nombres de los profiles de OpenRGB en disco, ordenados.
// Se leen del directorio de configuración en vez de preguntarle al servidor SDK
// para que la lista esté disponible aunque el servidor no esté corriendo.
func Profiles() ([]string, error) {
	entries, err := os.ReadDir(profileDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), ".json"))
	}
	sort.Strings(names)
	return names, nil
}

func profileDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "OpenRGB", "profiles")
}

// ActiveProfile devuelve el profile que el servidor SDK reporta como activo, o
// "" si no hay ninguno. Requiere el servidor en addr.
func ActiveProfile(addr string) (string, error) {
	client, err := rgb.DialMax(addr, 6)
	if err != nil {
		return "", err
	}
	defer client.Close()
	return client.ActiveProfile()
}
