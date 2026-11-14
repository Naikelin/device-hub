package fantech

import "testing"

func TestDecodeBCD(t *testing.T) {
	cases := map[byte]int{
		0x92: 92,
		0x50: 50,
		0x00: 0,
		0x99: 99,
	}
	for in, want := range cases {
		if got := decodeBCD(in); got != want {
			t.Errorf("decodeBCD(0x%02X) = %d, want %d", in, got, want)
		}
	}
}
