package skydds

import "testing"

func TestNormalizeDrainBufferBytes(t *testing.T) {
	if got := normalizeDrainBufferBytes(0); got != DefaultDrainBufferBytes {
		t.Fatalf("zero should fallback default: got=%d want=%d", got, DefaultDrainBufferBytes)
	}
	if got := normalizeDrainBufferBytes(-1); got != DefaultDrainBufferBytes {
		t.Fatalf("negative should fallback default: got=%d want=%d", got, DefaultDrainBufferBytes)
	}
	if got := normalizeDrainBufferBytes(8192); got != 8192 {
		t.Fatalf("positive should keep value: got=%d", got)
	}
}
