package tray

import (
	"fmt"
	"time"

	"device-hub/internal/store"

	"github.com/energye/systray"
)

// Run arranca el systray (SNI) y se queda hasta Quit.
// onToggle se invoca al hacer click izquierdo en el icono.
func Run(st *store.Store, interval time.Duration, onToggle func()) {
	systray.Run(
		func() { onReady(st, interval, onToggle) },
		func() {},
	)
}

func onReady(st *store.Store, interval time.Duration, onToggle func()) {
	deviceItems := map[string]*systray.MenuItem{}

	for _, d := range st.Devices() {
		it := systray.AddMenuItem(d.Name(), "")
		it.Disable()
		deviceItems[d.ID()] = it
	}
	systray.AddSeparator()
	refreshItem := systray.AddMenuItem("Actualizar", "Leer batería ahora")
	quitItem := systray.AddMenuItem("Salir", "Cerrar")

	// Click izquierdo en el icono -> abrir/cerrar el panel (señal Toggled).
	if onToggle != nil {
		systray.SetOnClick(func(systray.IMenu) { onToggle() })
	}

	refresh := func() {
		st.Refresh()
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
			tooltip = "Sin dispositivos"
		}
		systray.SetIcon(batteryIcon(min))
		systray.SetTooltip(tooltip)
	}

	refreshItem.Click(refresh)
	quitItem.Click(func() { systray.Quit() })

	refresh()
	go func() {
		for range time.Tick(interval) {
			refresh()
		}
	}()
}
