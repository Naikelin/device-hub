package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"device-hub/internal/control"
	"device-hub/internal/dbus"
	"device-hub/internal/device"
	"device-hub/internal/led"
	"device-hub/internal/providers/fantech"
	"device-hub/internal/providers/g435"
	"device-hub/internal/providers/openrgb"
	"device-hub/internal/providers/razer"
	"device-hub/internal/store"
	"device-hub/internal/tray"
)

// defaultPanelPath busca panel/panel.qml junto al ejecutable o en el cwd.
func defaultPanelPath() string {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "panel", "panel.qml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if abs, err := filepath.Abs("panel/panel.qml"); err == nil {
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	return ""
}

// defaultRGBPath devuelve panel/rgb.qml junto al panel.
func defaultRGBPath() string {
	panel := defaultPanelPath()
	if panel == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(panel), "rgb.qml")
}

func statePath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "device-hub", "state.json")
	}
	return filepath.Join(os.TempDir(), "device-hub-state.json")
}

func controlPath() string {
	return filepath.Join(filepath.Dir(statePath()), "control.sock")
}

func ledConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME") + "/.config"
	}
	return filepath.Join(dir, "device-hub", "leds.json")
}

// discoverOpenRGB espera a que el servidor SDK termine de detectar los
// controladores. openrgb-server tarda entre 5 y 10 s; se corta cuando el
// recuento se estabiliza o cuando el servidor no responde.
func discoverOpenRGB(addr string) ([]device.Device, map[string]string) {
	claimed := map[string]string{"naga pro": razer.Razer{}.ID()}
	var (
		devices  []device.Device
		names    = map[string]string{}
		last     = -1
		failures int
	)
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		devs, n, err := openrgb.Enumerate(addr, claimed)
		if err != nil {
			failures++
			if failures >= 3 {
				log.Printf("openrgb: servidor SDK no disponible: %v", err)
				break
			}
		} else {
			failures = 0
			if len(devs) > 0 && len(devs) == last {
				break
			}
			last = len(devs)
			devices, names = devs, n
		}
		time.Sleep(time.Second)
	}
	return devices, names
}

// ledPlan decide qué controladores acepta cada dispositivo y con cuál arranca.
func ledPlan(reg *device.Registry) (map[string][]led.Controller, map[string]led.Choice) {
	available := make(map[string][]led.Controller)
	defaults := make(map[string]led.Choice)
	for _, d := range reg.Devices() {
		switch d.ID() {
		case razer.Razer{}.ID():
			available[d.ID()] = []led.Controller{led.OpenRazer, led.OpenRGB}
			defaults[d.ID()] = led.Choice{Controller: led.OpenRazer, Enabled: true}
		case fantech.DeviceID:
			available[d.ID()] = []led.Controller{led.Direct}
			defaults[d.ID()] = led.Choice{Controller: led.Direct, Enabled: true}
		case g435.G435{}.ID():
			// El G435 no tiene LEDs: solo reporta batería.
			available[d.ID()] = nil
			defaults[d.ID()] = led.Choice{}
		default:
			available[d.ID()] = []led.Controller{led.OpenRGB}
			defaults[d.ID()] = led.Choice{Controller: led.OpenRGB, Enabled: true}
		}
	}
	return available, defaults
}

// razerSerials mapea el dispositivo Razer a su serial de openrazer.
func razerSerials() map[string]string {
	serials := map[string]string{}
	serial, err := razer.Serial()
	if err != nil {
		log.Printf("razer: serial de openrazer: %v", err)
		return serials
	}
	serials[razer.Razer{}.ID()] = serial
	return serials
}

var (
	spawnMu   sync.Mutex
	lastSpawn time.Time
)

// launch arranca un programa GUI sin quedarse esperándolo.
func launch(bin string, args ...string) {
	go func() {
		cmd := exec.Command(bin, args...)
		if err := cmd.Start(); err != nil {
			log.Printf("%s: %v", bin, err)
			return
		}
		_ = cmd.Wait()
	}()
}

// openRGBWindow muestra la ventana RGB si ya existe y, si no, la lanza.
func openRGBWindow(path, target string) {
	if path == "" {
		return
	}
	if err := exec.Command("qs", "ipc", "-p", path, "call", target, "open").Run(); err == nil {
		return
	}
	spawnMu.Lock()
	if time.Since(lastSpawn) < 2*time.Second {
		spawnMu.Unlock()
		return
	}
	lastSpawn = time.Now()
	spawnMu.Unlock()
	go func() {
		out, err := exec.Command("qs", "-p", path).CombinedOutput()
		if err != nil {
			log.Printf("rgb window: %v: %s", err, out)
		}
	}()
}

func main() {
	interval := flag.Duration("interval", 60*time.Second, "intervalo de refresco")
	panel := flag.String("panel", defaultPanelPath(), "ruta a panel.qml (vacío desactiva el panel)")
	rgbWindow := flag.String("rgb", defaultRGBPath(), "ruta a rgb.qml (vacío desactiva la ventana RGB)")
	ipcTarget := flag.String("ipc-target", "devicehub", "target de IPC registrado por el panel")
	rgbTarget := flag.String("rgb-ipc-target", "devicehub-rgb", "target de IPC de la ventana RGB")
	flag.Parse()

	reg := device.NewRegistry()
	reg.Register(razer.Razer{})
	reg.Register(g435.G435{})
	reg.Register(fantech.Fantech{})

	addr := openrgb.Address()
	openrgbDevices, names := discoverOpenRGB(addr)
	for _, d := range openrgbDevices {
		reg.Register(d)
	}
	log.Printf("openrgb: %d controladores registrados", len(openrgbDevices))

	st := store.New(reg)

	backends := map[led.Controller]led.Backend{
		led.OpenRGB:   led.NewOpenRGBBackend(addr, names),
		led.OpenRazer: led.NewOpenRazerBackend(razerSerials()),
		led.Direct:    led.DirectBackend{},
	}
	available, defaults := ledPlan(reg)
	ledManager, err := led.NewManager(led.Config{
		Path:      ledConfigPath(),
		StatePath: statePath(),
		Backends:  backends,
		Available: available,
		Defaults:  defaults,
		Store:     st,
	})
	if err != nil {
		log.Fatalf("led: %v", err)
	}

	openRGB := func() { openRGBWindow(*rgbWindow, *rgbTarget) }
	openOpenRGB := func() { launch("openrgb", "--gui") }

	// Las listas de profiles se leen del disco: no hace falta el servidor para
	// mostrarlas, y se refrescan en cada tick del tray.
	refreshProfiles := func() {
		names, err := openrgb.Profiles()
		if err != nil {
			log.Printf("openrgb: profiles: %v", err)
			return
		}
		st.SetProfiles(names)
		if active, err := openrgb.ActiveProfile(addr); err == nil {
			st.SetActiveProfile(active)
		}
	}
	refreshProfiles()

	loadProfile := func(name string) error {
		names, err := openrgb.Profiles()
		if err != nil {
			return fmt.Errorf("openrgb: profiles: %w", err)
		}
		if !slices.Contains(names, name) {
			return fmt.Errorf("openrgb: profile desconocido: %s", name)
		}
		// El CLI se conecta al servidor SDK y aplica el profile en ~70 ms.
		// Se prefiere al paquete LOAD_PROFILE porque ese requiere reenviar los
		// modos de cada controlador después.
		out, err := exec.Command("openrgb", "--profile", name).CombinedOutput()
		if err != nil {
			return fmt.Errorf("openrgb --profile %s: %v: %s", name, err, strings.TrimSpace(string(out)))
		}
		ledManager.ForgetColors()
		st.ClearColors()
		st.SetActiveProfile(name)
		refreshProfiles()
		_ = st.WriteState(statePath())
		return nil
	}

	// Servidor de control para el panel (Unix socket, JSON por línea).
	control, err := control.NewServer(controlPath(), ledManager, openRGB, openOpenRGB, loadProfile)
	if err != nil {
		log.Printf("control server: %v", err)
	} else {
		go control.Run()
		defer control.Close()
	}

	svc, err := dbus.NewService(st, ledManager)
	if err != nil {
		log.Fatalf("dbus: %v", err)
	}
	defer svc.Close()

	// Click en el tray: señal D-Bus para otras herramientas + toggle del panel.
	onToggle := func() {
		svc.EmitToggle()
		if *panel == "" {
			return
		}
		go func() {
			out, err := exec.Command("qs", "ipc", "-p", *panel, "call", *ipcTarget, "toggle").CombinedOutput()
			if err != nil {
				log.Printf("panel ipc: %v: %s", err, out)
			}
		}()
	}

	// Los LEDs apagados para ahorrar batería se mantienen apagados tras un
	// reinicio del daemon. Los encendidos se dejan como los dejó el perfil de
	// login de OpenRGB.
	go func() {
		time.Sleep(2 * time.Second)
		ledManager.RestoreOff()
	}()

	tray.Run(st, *interval, statePath(), tray.Handlers{
		Toggle:  onToggle,
		Colors:  openRGB,
		OpenRGB: openOpenRGB,
		Tick:    refreshProfiles,
	})
}
