package compliance

import "testing"

func TestParseICCHeader(t *testing.T) {
	t.Parallel()

	t.Run("valid RGB header", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, 128)
		data[8] = 4 // version 4
		copy(data[12:16], "mntr")
		copy(data[16:20], "RGB ")
		hdr, ok := parseICCHeader(data)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if hdr.deviceClass != "mntr" {
			t.Errorf("deviceClass = %q, want %q", hdr.deviceClass, "mntr")
		}
		if hdr.colorSpace != "RGB " {
			t.Errorf("colorSpace = %q, want %q", hdr.colorSpace, "RGB ")
		}
		if hdr.version != 4 {
			t.Errorf("version = %d, want 4", hdr.version)
		}
	})

	t.Run("valid CMYK printer", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, 20)
		data[8] = 2
		copy(data[12:16], "prtr")
		copy(data[16:20], "CMYK")
		hdr, ok := parseICCHeader(data)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if hdr.deviceClass != "prtr" || hdr.colorSpace != "CMYK" || hdr.version != 2 {
			t.Errorf("unexpected header: %+v", hdr)
		}
	})

	t.Run("too short", func(t *testing.T) {
		t.Parallel()
		_, ok := parseICCHeader(make([]byte, 19))
		if ok {
			t.Error("expected ok=false for short data")
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		_, ok := parseICCHeader(nil)
		if ok {
			t.Error("expected ok=false for nil data")
		}
	})
}

func TestExpectedComponentCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cs   string
		want int
	}{
		{"GRAY", 1},
		{"RGB ", 3},
		{"CMYK", 4},
		{"Lab ", 0},
		{"", 0},
		{"XYZ ", 0},
	}
	for _, tc := range tests {
		t.Run(tc.cs, func(t *testing.T) {
			t.Parallel()
			if got := expectedComponentCount(tc.cs); got != tc.want {
				t.Errorf("expectedComponentCount(%q) = %d, want %d", tc.cs, got, tc.want)
			}
		})
	}
}
