package tray

import (
	"fmt"
	"time"

	"device-hub/internal/store"

	"github.com/energye/systray"
)

// Handlers son las acciones que el menú dispara.
type Handlers struct {
	Toggle  func() // click en el icono: abre/cierra el panel
	Colors  func() // ventana de colores del hub (rgb.qml)
	OpenRGB func() // GUI oficial de OpenRGB
	Tick    func() // refresca datos externos antes de escribir el estado
}

// Run arranca el systray (SNI) y se queda hasta Quit.
// statePath es el archivo JSON que observa el panel QML.
func Run(st *store.Store, interval time.Duration, statePath string, h Handlers) {
	systray.Run(
		func() { onReady(st, interval, statePath, h) },
		func() {},
	)
}

func onReady(st *store.Store, interval time.Duration, statePath string, h Handlers) {
	deviceItems := map[string]*systray.MenuItem{}

	for _, d := range st.Devices() {
		it := systray.AddMenuItem(d.Name(), "")
		it.Disable()
		deviceItems[d.ID()] = it
	}
	systray.AddSeparator()
	colorsItem := systray.AddMenuItem("Colores LED…", "Abrir la paleta del hub")
	openrgbItem := systray.AddMenuItem("OpenRGB…", "Abrir la GUI de OpenRGB")
	refreshItem := systray.AddMenuItem("Actualizar", "Leer batería ahora")
	quitItem := systray.AddMenuItem("Salir", "Cerrar")

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
					it.SetTitle(d.Name() + " — desconectado")
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
			tooltip = "Sin dispositivos con batería"
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
