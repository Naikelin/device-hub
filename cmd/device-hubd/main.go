package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"device-hub/internal/dbus"
	"device-hub/internal/device"
	"device-hub/internal/providers/fantech"
	"device-hub/internal/providers/g435"
	"device-hub/internal/providers/razer"
	"device-hub/internal/rgb"
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

func statePath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "device-hub", "state.json")
	}
	return filepath.Join(os.TempDir(), "device-hub-state.json")
}

func controlPath() string {
	return filepath.Join(filepath.Dir(statePath()), "control.sock")
}

func main() {
	interval := flag.Duration("interval", 60*time.Second, "intervalo de refresco")
	panel := flag.String("panel", defaultPanelPath(), "ruta a panel.qml (vacío desactiva el panel)")
	ipcTarget := flag.String("ipc-target", "devicehub", "target de IPC registrado por el panel")
	flag.Parse()

	reg := device.NewRegistry()
	reg.Register(razer.Razer{})
	reg.Register(g435.G435{})
	reg.Register(fantech.Fantech{})

	st := store.New(reg)
	rgbManager := rgb.NewManager(reg, st, statePath())

	// Servidor de control para el panel (Unix socket, JSON por línea).
	control, err := rgb.NewServer(controlPath(), rgbManager)
	if err != nil {
		log.Printf("rgb control server: %v", err)
	} else {
		go control.Run()
		defer control.Close()
	}

	svc, err := dbus.NewService(st, rgbManager)
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

	tray.Run(st, *interval, statePath(), onToggle)
}
