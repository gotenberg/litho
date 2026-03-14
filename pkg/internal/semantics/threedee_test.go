package semantics

import (
	"testing"
)

func TestDetect3DFromMagic_PRC(t *testing.T) {
	data := []byte("PRC\x00\x01\x02\x03")
	got := detect3DFromMagic(data)
	if got != "PRC" {
		t.Errorf("got %q, want PRC", got)
	}
}

func TestDetect3DFromMagic_U3D(t *testing.T) {
	data := []byte("\x55\x33\x44\x00\x01\x02\x03")
	got := detect3DFromMagic(data)
	if got != "U3D" {
		t.Errorf("got %q, want U3D", got)
	}
}

func TestDetect3DFromMagic_TooShort(t *testing.T) {
	data := []byte("\x00\x01")
	got := detect3DFromMagic(data)
	if got != "U3D" {
		t.Errorf("got %q, want U3D (default for short data)", got)
	}
}

func TestDetect3DFromMagic_Empty(t *testing.T) {
	got := detect3DFromMagic(nil)
	if got != "U3D" {
		t.Errorf("got %q, want U3D (default for empty data)", got)
	}
}
