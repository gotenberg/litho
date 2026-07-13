package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestScanFormXObjectCS_DeviceGroupCSCountsAsUsage covers §6.2.4.3: a Form
// XObject whose transparency /Group /CS is a device colour space is itself a
// device-colour usage (veraPDF PDDeviceRGB at /Group/colorSpace), coverable only
// by a matching OutputIntent. A group with no device /CS is not.
func TestScanFormXObjectCS_DeviceGroupCSCountsAsUsage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	scan := func(t *testing.T, group string) deviceCSUsage {
		t.Helper()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog >>",
			"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] "+group+" /Length 0 >>\nstream\nendstream",
		)
		stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
		if err != nil {
			t.Fatalf("resolve form: %v", err)
		}
		return scanFormXObjectCS(ctx, doc, stream, 0, map[string]bool{}, 4)
	}

	if !scan(t, "/Group << /S /Transparency /CS /DeviceRGB >>").rgb {
		t.Error("DeviceRGB transparency group /CS must count as DeviceRGB usage")
	}
	if scan(t, "/Group << /S /Transparency >>").rgb {
		t.Error("a transparency group without a device /CS must not count as DeviceRGB usage")
	}
}

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

func TestICCVersionExceedsCeiling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		version int
		part    int
		want    bool
	}{
		// PDF/A-1 (PDF 1.4) predates ICC v4: v2 passes, v4 fails (veraPDF < 3.0).
		{"part1 v2 ok", 2, 1, false},
		{"part1 v4 fails", 4, 1, true},
		{"part1 v3 fails", 3, 1, true},
		// PDF/A-2/3/4 admit ICC v4; reject only v5+ (veraPDF < 5.0).
		{"part2 v4 ok", 4, 2, false},
		{"part3 v4 ok", 4, 3, false},
		{"part4 v4 ok", 4, 4, false},
		{"part2 v5 fails", 5, 2, true},
		{"part4 v5 fails", 5, 4, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := iccVersionExceedsCeiling(tc.version, tc.part); got != tc.want {
				t.Errorf("iccVersionExceedsCeiling(%d, %d) = %v, want %v", tc.version, tc.part, got, tc.want)
			}
		})
	}
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

func TestMarkDeviceCS(t *testing.T) {
	t.Parallel()

	t.Run("DeviceRGB", func(t *testing.T) {
		t.Parallel()
		var u deviceCSUsage
		markDeviceCS("DeviceRGB", &u)
		if !u.rgb || u.cmyk || u.gray {
			t.Errorf("unexpected usage: %+v", u)
		}
	})

	t.Run("DeviceCMYK", func(t *testing.T) {
		t.Parallel()
		var u deviceCSUsage
		markDeviceCS("DeviceCMYK", &u)
		if u.rgb || !u.cmyk || u.gray {
			t.Errorf("unexpected usage: %+v", u)
		}
	})

	t.Run("DeviceGray", func(t *testing.T) {
		t.Parallel()
		var u deviceCSUsage
		markDeviceCS("DeviceGray", &u)
		if u.rgb || u.cmyk || !u.gray {
			t.Errorf("unexpected usage: %+v", u)
		}
	})

	t.Run("other", func(t *testing.T) {
		t.Parallel()
		var u deviceCSUsage
		markDeviceCS("ICCBased", &u)
		if u.rgb || u.cmyk || u.gray {
			t.Errorf("unexpected usage for non-device CS: %+v", u)
		}
	})
}

func TestBytesEqual(t *testing.T) {
	t.Parallel()

	t.Run("equal", func(t *testing.T) {
		t.Parallel()
		if !bytesEqual([]byte{1, 2, 3}, []byte{1, 2, 3}) {
			t.Error("expected true for identical slices")
		}
	})

	t.Run("different length", func(t *testing.T) {
		t.Parallel()
		if bytesEqual([]byte{1, 2}, []byte{1, 2, 3}) {
			t.Error("expected false for different lengths")
		}
	})

	t.Run("different content", func(t *testing.T) {
		t.Parallel()
		if bytesEqual([]byte{1, 2, 3}, []byte{1, 2, 4}) {
			t.Error("expected false for different content")
		}
	})

	t.Run("both nil", func(t *testing.T) {
		t.Parallel()
		if !bytesEqual(nil, nil) {
			t.Error("expected true for nil slices")
		}
	})
}

func TestICCProfilesEqual(t *testing.T) {
	t.Parallel()

	t.Run("different lengths", func(t *testing.T) {
		t.Parallel()
		if iccProfilesEqual(make([]byte, 100), make([]byte, 101)) {
			t.Error("expected false for different lengths")
		}
	})

	t.Run("short profiles compared as bytes", func(t *testing.T) {
		t.Parallel()
		a := []byte{1, 2, 3}
		b := []byte{1, 2, 3}
		if !iccProfilesEqual(a, b) {
			t.Error("expected true for identical short profiles")
		}
	})

	t.Run("identical profiles with zeroed ID", func(t *testing.T) {
		t.Parallel()
		a := make([]byte, 128)
		b := make([]byte, 128)
		copy(a, b)
		if !iccProfilesEqual(a, b) {
			t.Error("expected true for identical profiles")
		}
	})
}

func TestSameIndirectRef(t *testing.T) {
	t.Parallel()

	t.Run("same ref", func(t *testing.T) {
		t.Parallel()
		a := ast.PdfIndirectRef{ObjectNum: 10, GenNum: 0}
		b := ast.PdfIndirectRef{ObjectNum: 10, GenNum: 0}
		if !sameIndirectRef(a, b) {
			t.Error("expected true for same ref")
		}
	})

	t.Run("different ref", func(t *testing.T) {
		t.Parallel()
		a := ast.PdfIndirectRef{ObjectNum: 10}
		b := ast.PdfIndirectRef{ObjectNum: 11}
		if sameIndirectRef(a, b) {
			t.Error("expected false for different refs")
		}
	})

	t.Run("non-ref objects", func(t *testing.T) {
		t.Parallel()
		if sameIndirectRef(ast.PdfName("X"), ast.PdfName("X")) {
			t.Error("expected false for non-ref objects")
		}
	})
}
