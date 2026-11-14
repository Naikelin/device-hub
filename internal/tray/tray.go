package tray

import (
	"fmt"
	"time"

	"device-hub/internal/store"

	"github.com/energye/systray"
)

// Run arranca el systray (SNI) y se queda hasta Quit.
// statePath es el archivo JSON que observa el panel QML.
// onToggle se invoca además al hacer click izquierdo (para la señal D-Bus).
// onRGB abre la ventana de control RGB.
func Run(st *store.Store, interval time.Duration, statePath string, onToggle, onRGB func()) {
	systray.Run(
		func() { onReady(st, interval, statePath, onToggle, onRGB) },
		func() {},
	)
}

func onReady(st *store.Store, interval time.Duration, statePath string, onToggle, onRGB func()) {
	deviceItems := map[string]*systray.MenuItem{}

	for _, d := range st.Devices() {
		it := systray.AddMenuItem(d.Name(), "")
		it.Disable()
		deviceItems[d.ID()] = it
	}
	systray.AddSeparator()
	rgbItem := systray.AddMenuItem("Control RGB…", "Abrir la ventana de color")
	refreshItem := systray.AddMenuItem("Actualizar", "Leer batería ahora")
	quitItem := systray.AddMenuItem("Salir", "Cerrar")

	// Click izquierdo -> abrir/cerrar el panel (IPC al panel + señal D-Bus).
	systray.SetOnClick(func(systray.IMenu) {
		if onToggle != nil {
			onToggle()
		}
	})

	writeState := func() {
		if statePath != "" {
			_ = st.WriteState(statePath)
		}
	}

	refresh := func() {
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
			if !ok || r.Battery == nil {
				if it, ok := deviceItems[d.ID()]; ok {
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
			if it, ok := deviceItems[d.ID()]; ok {
				it.SetTitle(fmt.Sprintf("%s — %d%%", d.Name(), r.Battery.Percent))
			}
		}
		if tooltip == "" {
			tooltip = "Sin dispositivos con batería"
		}
		systray.SetIcon(batteryIcon(min))
		systray.SetTooltip(tooltip)
	}

	rgbItem.Click(func() {
		if onRGB != nil {
			onRGB()
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
