package tray

import (
	"fmt"
	"log"
	"time"

	"device-hub/internal/store"

	"github.com/energye/systray"
	"github.com/godbus/dbus/v5"
)

// Handlers son las acciones que el menú dispara.
type Handlers struct {
	Toggle  func() // click en el icono: abre/cierra el panel
	Colors  func() // ventana de colores del hub (rgb.qml)
	OpenRGB func() // GUI oficial de OpenRGB
	Tick    func() // refresca datos externos antes de escribir el estado
}

// Run starts the systray (SNI) and blocks until Quit. It waits for the SNI
// watcher to answer before starting, because the library registers the icon
// exactly once and never retries a failed registration.
// statePath is the JSON file the QML panel watches.
func Run(st *store.Store, interval time.Duration, statePath string, h Handlers) {
	ready := func() { onReady(st, interval, statePath, h) }

	conn, err := dbus.SessionBus()
	if err != nil {
		log.Printf("tray: no session bus (%v); starting without the icon", err)
		systray.Run(ready, nil)
		return
	}

	if !waitForWatcher(conn, watcherWaitTimeout) {
		log.Printf("tray: no %s after %s; starting without the icon", watcherName, watcherWaitTimeout)
	} else {
		go confirmRegistration(conn, registerCheckWait)
	}

	// Watched on both paths: a watcher that shows up late still gets the icon,
	// and one that goes away never leaves the daemon without a tray.
	go watchWatcher(conn)

	systray.Run(ready, nil)
}

func onReady(st *store.Store, interval time.Duration, statePath string, h Handlers) {
	deviceItems := map[string]*systray.MenuItem{}

	for _, d := range st.Devices() {
		it := systray.AddMenuItem(d.Name(), "")
		it.Disable()
		deviceItems[d.ID()] = it
	}
	systray.AddSeparator()
	colorsItem := systray.AddMenuItem("LED colors…", "Open the hub palette")
	openrgbItem := systray.AddMenuItem("OpenRGB…", "Open the OpenRGB GUI")
	refreshItem := systray.AddMenuItem("Refresh", "Read battery now")
	quitItem := systray.AddMenuItem("Quit", "Close")

	// Click izquierdo -> abrir/cerrar el panel (IPC al panel + señal D-Bus).
	systray.SetOnClick(func(systray.IMenu) {
		if h.Toggle != nil {
			h.Toggle()
		}
	})

	writeState := func() {
		if statePath != "" {
			_ = st.WriteState(statePath)
		}
	}

	refresh := func() {
		if h.Tick != nil {
			h.Tick()
		}
		st.Refresh()
		writeState()

		byID := map[string]store.Reading{}
		for _, r := range st.Readings() {
			byID[r.Device.ID()] = r
		}

		min := -1
		var tooltip string
		for _, d := range st.Devices() {
			r, ok := byID[d.ID()]
			if !ok {
				continue
			}
			it := deviceItems[d.ID()]
			if !r.Connected {
				if it != nil {
					it.SetTitle(d.Name() + " — disconnected")
				}
				continue
			}
			if r.Battery == nil {
				if it != nil {
					it.SetTitle(d.Name())
				}
				continue
			}
			if min < 0 || r.Battery.Percent < min {
				min = r.Battery.Percent
			}
			if tooltip != "" {
				tooltip += " · "
			}
			tooltip += fmt.Sprintf("%s %d%%", d.Name(), r.Battery.Percent)
			if it != nil {
				it.SetTitle(fmt.Sprintf("%s — %d%%", d.Name(), r.Battery.Percent))
			}
		}
		if tooltip == "" {
			tooltip = "No battery devices"
		}
		systray.SetIcon(batteryIcon(min))
		systray.SetTooltip(tooltip)
	}

	colorsItem.Click(func() {
		if h.Colors != nil {
			h.Colors()
		}
	})
	openrgbItem.Click(func() {
		if h.OpenRGB != nil {
			h.OpenRGB()
		}
	})
	refreshItem.Click(refresh)
	quitItem.Click(func() { systray.Quit() })

	refresh()
	go func() {
		for range time.Tick(interval) {
			refresh()
		}
	}()
}
