package openrgb

import (
	"testing"

	"device-hub/internal/device"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Razer Naga Pro":                     "razer-naga-pro",
		"G.Skill Trident Z5 Neo":             "g-skill-trident-z5-neo",
		"ASUS ROG STRIX B650E-F GAMING WIFI": "asus-rog-strix-b650e-f-gaming-wifi",
		"  spaced  ":                         "spaced",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKindFor(t *testing.T) {
	cases := map[string]device.Kind{
		"Razer Leviathan V2 Pro":        device.KindSpeaker,
		"HyperX QuadCast S":             device.KindMicrophone,
		"G.Skill Trident Z5 Neo":        device.KindMemory,
		"HyperX Fury RGB":               device.KindMemory,
		"NVIDIA GeForce RTX 4070":       device.KindGPU,
		"ASUS ROG STRIX B650E-F GAMING": device.KindMotherboard,
		"Some Unknown Controller":       device.KindLighting,
	}
	for name, want := range cases {
		if got := KindFor(name); got != want {
			t.Errorf("KindFor(%q) = %v, want %v", name, got, want)
		}
	}
}
