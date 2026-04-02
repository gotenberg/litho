package compliance

import (
	"encoding/binary"
	"testing"
)

func TestIsJP2Container(t *testing.T) {
	t.Parallel()

	sig := []byte{0x00, 0x00, 0x00, 0x0C, 0x6A, 0x50, 0x20, 0x20, 0x0D, 0x0A, 0x87, 0x0A}

	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"valid signature", sig, true},
		{"valid with trailing data", append(sig, 0x00, 0x00), true},
		{"too short", sig[:11], false},
		{"empty", nil, false},
		{"wrong byte 4", []byte{0x00, 0x00, 0x00, 0x0C, 0xFF, 0x50, 0x20, 0x20, 0x0D, 0x0A, 0x87, 0x0A}, false},
		{"all zeros", make([]byte, 12), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isJP2Container(tc.data); got != tc.want {
				t.Errorf("isJP2Container(%v) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

func TestParseJP2Boxes(t *testing.T) {
	t.Parallel()

	t.Run("single box", func(t *testing.T) {
		t.Parallel()
		// Length=12, type='test', 4 bytes payload
		data := make([]byte, 12)
		binary.BigEndian.PutUint32(data[0:4], 12)
		binary.BigEndian.PutUint32(data[4:8], 0x74657374) // 'test'
		data[8], data[9], data[10], data[11] = 0xAA, 0xBB, 0xCC, 0xDD
		boxes := parseJP2Boxes(data)
		if len(boxes) != 1 {
			t.Fatalf("expected 1 box, got %d", len(boxes))
		}
		if boxes[0].boxType != 0x74657374 {
			t.Errorf("unexpected box type: %x", boxes[0].boxType)
		}
		if len(boxes[0].payload) != 4 {
			t.Errorf("expected 4 bytes payload, got %d", len(boxes[0].payload))
		}
	})

	t.Run("two boxes", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, 24)
		// Box 1: length=12, type=0x11111111
		binary.BigEndian.PutUint32(data[0:4], 12)
		binary.BigEndian.PutUint32(data[4:8], 0x11111111)
		// Box 2: length=12, type=0x22222222
		binary.BigEndian.PutUint32(data[12:16], 12)
		binary.BigEndian.PutUint32(data[16:20], 0x22222222)
		boxes := parseJP2Boxes(data)
		if len(boxes) != 2 {
			t.Fatalf("expected 2 boxes, got %d", len(boxes))
		}
	})

	t.Run("zero length box extends to end", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, 16)
		binary.BigEndian.PutUint32(data[0:4], 0) // length 0 = rest of data
		binary.BigEndian.PutUint32(data[4:8], 0x74657374)
		boxes := parseJP2Boxes(data)
		if len(boxes) != 1 {
			t.Fatalf("expected 1 box, got %d", len(boxes))
		}
		if len(boxes[0].payload) != 8 {
			t.Errorf("expected 8 bytes payload, got %d", len(boxes[0].payload))
		}
	})

	t.Run("truncated data", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, 8)
		binary.BigEndian.PutUint32(data[0:4], 100) // claims 100 bytes but only 8 available
		binary.BigEndian.PutUint32(data[4:8], 0x74657374)
		boxes := parseJP2Boxes(data)
		if len(boxes) != 0 {
			t.Errorf("expected 0 boxes for truncated data, got %d", len(boxes))
		}
	})

	t.Run("empty data", func(t *testing.T) {
		t.Parallel()
		boxes := parseJP2Boxes(nil)
		if len(boxes) != 0 {
			t.Errorf("expected 0 boxes, got %d", len(boxes))
		}
	})
}

func TestParseCOLR(t *testing.T) {
	t.Parallel()

	t.Run("method 1 with enumCS", func(t *testing.T) {
		t.Parallel()
		// meth=0x01, prec=0, approx=0x01, enumCS=16 (sRGB)
		payload := []byte{0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x10}
		ci, ok := parseCOLR(payload)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if ci.meth != 0x01 || ci.approx != 0x01 || ci.enumCS != 16 {
			t.Errorf("unexpected colrInfo: %+v", ci)
		}
	})

	t.Run("method 2 no enumCS", func(t *testing.T) {
		t.Parallel()
		payload := []byte{0x02, 0x00, 0x00}
		ci, ok := parseCOLR(payload)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if ci.meth != 0x02 || ci.enumCS != 0 {
			t.Errorf("unexpected colrInfo: %+v", ci)
		}
	})

	t.Run("too short", func(t *testing.T) {
		t.Parallel()
		_, ok := parseCOLR([]byte{0x01, 0x00})
		if ok {
			t.Error("expected ok=false for too-short payload")
		}
	})
}

func TestParseJ2KSIZMarker(t *testing.T) {
	t.Parallel()

	t.Run("valid 3-component image", func(t *testing.T) {
		t.Parallel()
		// Build minimal valid J2K with SOC + SIZ marker.
		// Need at least compStart + csiz*3 = 42 + 3*3 = 51 bytes.
		data := make([]byte, 51)
		data[0] = 0xFF
		data[1] = 0x4F // SOC
		data[2] = 0xFF
		data[3] = 0x51 // SIZ
		// Csiz at offset 40 = 3 components
		binary.BigEndian.PutUint16(data[40:42], 3)
		// Component entries at offset 42: Ssiz(1) XRsiz(1) YRsiz(1) per component
		data[42] = 0x07 // Ssiz=7 -> bit depth 8
		data[43] = 1    // XRsiz
		data[44] = 1    // YRsiz
		data[45] = 0x07
		data[46] = 1
		data[47] = 1
		data[48] = 0x07
		data[49] = 1
		data[50] = 1

		numComp, ssiz, err := parseJ2KSIZMarker(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if numComp != 3 {
			t.Errorf("expected 3 components, got %d", numComp)
		}
		if len(ssiz) != 3 {
			t.Errorf("expected 3 ssiz entries, got %d", len(ssiz))
		}
	})

	t.Run("truncated data", func(t *testing.T) {
		t.Parallel()
		data := make([]byte, 30) // too short for Csiz
		_, _, err := parseJ2KSIZMarker(data)
		if err == nil {
			t.Error("expected error for truncated data")
		}
	})
}

func TestCheckBitDepths(t *testing.T) {
	t.Parallel()

	rule := Rule{Specification: "test", Clause: "6.2", TestNumber: 5, Description: "bit depth"}

	t.Run("all same valid depth", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		// Ssiz 0x07 -> bit depth (0x07 & 0x7F)+1 = 8
		checkBitDepths(&results, []byte{0x07, 0x07, 0x07}, "Im0", 1, rule)
		if len(results) != 0 {
			t.Errorf("expected no violations, got %d", len(results))
		}
	})

	t.Run("inconsistent depths", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		checkBitDepths(&results, []byte{0x07, 0x0F}, "Im0", 1, rule)
		if len(results) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(results))
		}
	})

	t.Run("depth out of range", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		// 0x7F & 0x7F + 1 = 128, exceeds 38
		checkBitDepths(&results, []byte{0x7F}, "Im0", 1, rule)
		if len(results) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(results))
		}
	})

	t.Run("empty ssiz", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		checkBitDepths(&results, nil, "Im0", 1, rule)
		if len(results) != 0 {
			t.Errorf("expected no violations for empty ssiz, got %d", len(results))
		}
	})
}
