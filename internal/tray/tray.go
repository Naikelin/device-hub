package tray

import (
	"fmt"
	"log"
	"time"

	"device-hub/internal/device"

	"github.com/energye/systray"
)

// Run arranca el systray (SNI) y se queda hasta Quit.
func Run(reg *device.Registry, interval time.Duration) {
	systray.Run(
		func() { onReady(reg, interval) },
		func() {},
	)
}

func onReady(reg *device.Registry, interval time.Duration) {
	deviceItems := map[string]*systray.MenuItem{}

	for _, d := range reg.Devices() {
		it := systray.AddMenuItem(d.Name(), "")
		it.Disable()
		deviceItems[d.ID()] = it
	}
	systray.AddSeparator()
	refreshItem := systray.AddMenuItem("Actualizar", "Leer batería ahora")
	quitItem := systray.AddMenuItem("Salir", "Cerrar")

	refresh := func() {
		readings := map[string]device.BatteryReading{}
		for _, d := range reg.Devices() {
			b, err := d.Battery()
			if err != nil {
				log.Printf("%s: %v", d.Name(), err)
				continue
			}
			readings[d.ID()] = b
		}

		// icono = nivel mínimo; tooltip = resumen
		min := -1
		var tooltip string
		for _, d := range reg.Devices() {
			b, ok := readings[d.ID()]
			if !ok {
				continue
			}
			if min < 0 || b.Percent < min {
				min = b.Percent
			}
			if tooltip != "" {
				tooltip += " · "
			}
			tooltip += fmt.Sprintf("%s %d%%", d.Name(), b.Percent)
			if it, ok := deviceItems[d.ID()]; ok {
				it.SetTitle(fmt.Sprintf("%s — %d%%", d.Name(), b.Percent))
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
