package g435

import "testing"

func TestCRC16XmodemCheckValue(t *testing.T) {
	// Valor de verificación estándar de CRC-16/XMODEM para "123456789".
	if got := crc16Xmodem([]byte("123456789")); got != 0x31C3 {
		t.Fatalf("crc16Xmodem = 0x%04X, want 0x31C3", got)
	}
}

func TestVoltageToPercent(t *testing.T) {
	cases := map[int]int{
		4200: 100,
		4100: 90,
		4000: 80,
		3900: 65,
		3800: 50,
		3700: 30,
		3600: 10,
		3300: 0,
		0:    0,
	}
	for mv, want := range cases {
		if got := voltageToPercent(mv); got != want {
			t.Errorf("voltageToPercent(%d) = %d, want %d", mv, got, want)
		}
	}
}

func TestBuildReport(t *testing.T) {
	// buildReport(0x85, [1]) -> 64 bytes: [0x85, 1, crc_lo, crc_hi, 1, 0...]
	r := buildReport(0x85, []byte{1})
	if len(r) != 64 {
		t.Fatalf("buildReport length = %d, want 64", len(r))
	}
	if r[0] != 0x85 || r[1] != 1 {
		t.Fatalf("buildReport header = % X, want 85 01", r[:2])
	}
	// CRC recomputado sobre el paquete de 65 bytes con campo crc a cero.
	pkt := make([]byte, 65)
	pkt[1] = 0x85
	pkt[2] = 1
	pkt[5] = 1
	crc := crc16Xmodem(pkt)
	if r[2] != byte(crc&0xFF) || r[3] != byte(crc>>8) {
		t.Fatalf("buildReport crc = % X, want % X", r[2:4], []byte{byte(crc & 0xFF), byte(crc >> 8)})
	}
}

func TestOTACommand(t *testing.T) {
	got := otaCommand(0x31)
	want := []byte{0x50, 0x41, 0x06, 0, 1, 0, 0x31}
	if len(got) != len(want) {
		t.Fatalf("otaCommand length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("otaCommand = % X, want % X", got, want)
		}
	}
}
