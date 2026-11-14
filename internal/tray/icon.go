// Package tray implementa el icono de systray (SNI) con la batería de los
// dispositivos. Es agnóstico de la barra: cualquier host SNI lo muestra.
package tray

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

func fillColor(percent int) color.RGBA {
	switch {
	case percent > 50:
		return color.RGBA{76, 175, 80, 255}
	case percent > 20:
		return color.RGBA{255, 193, 7, 255}
	default:
		return color.RGBA{244, 67, 54, 255}
	}
}

// batteryIcon dibuja una batería con nivel de carga y la devuelve como PNG.
// percent < 0 => vacía/gris (sin datos).
func batteryIcon(percent int) []byte {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	outline := color.RGBA{210, 210, 210, 255}

	pad := 4
	nubW := 12
	bodyX0, bodyY0 := pad, pad*2
	bodyX1, bodyY1 := size-pad-nubW, size-pad*2

	// contorno del cuerpo
	for x := bodyX0; x <= bodyX1; x++ {
		img.Set(x, bodyY0, outline)
		img.Set(x, bodyY1, outline)
	}
	for y := bodyY0; y <= bodyY1; y++ {
		img.Set(bodyX0, y, outline)
		img.Set(bodyX1, y, outline)
	}

	// terminal
	nubY0, nubY1 := size/2-size/6, size/2+size/6
	for x := bodyX1; x <= bodyX1+nubW; x++ {
		for y := nubY0; y <= nubY1; y++ {
			img.Set(x, y, outline)
		}
	}

	// relleno
	if percent > 0 {
		c := fillColor(percent)
		if percent > 100 {
			percent = 100
		}
		inner := 4
		fillW := (bodyX1 - bodyX0 - 2*inner) * percent / 100
		for x := bodyX0 + inner; x <= bodyX0+inner+fillW; x++ {
			for y := bodyY0 + inner; y <= bodyY1-inner; y++ {
				img.Set(x, y, c)
			}
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
