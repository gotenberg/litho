package content

import (
	"context"
	"strings"
	"testing"
)

// bufferedPage returns the content of a one-page fixture measured with
// veraPDF 1.30 at PDF/A-2b: a DeviceRGB fill, 5050 bytes of comments and one
// of pad bytes, then body.
func bufferedPage(pad int, body string) string {
	return "1 0 0 rg 0 0 10 10 re f\n" +
		strings.Repeat("%"+strings.Repeat("p", 99)+"\n", 50) +
		"%" + strings.Repeat("p", pad) + "\n" + body +
		strings.Repeat("%"+strings.Repeat("t", 99)+"\n", 50)
}

// TestInlineImagesFollowVeraPDFBuffer pins that a parse without an option
// reads no operation of a stream in which veraPDF moves back past its buffer
// ([veraBuffer]), and that the other readings read it. Measured with veraPDF
// 1.30 at PDF/A-2b: the fixtures that lose the stream report no DeviceRGB and
// no DeviceGray, the others both.
func TestInlineImagesFollowVeraPDFBuffer(t *testing.T) {
	t.Parallel()

	const img = "q BI /W 1 /H 1 /BPC 8 /CS /G ID x EI "
	comment := "%" + strings.Repeat("c", 1200) + "\nQ\n"
	chain := func(a, b int) string {
		return "q BI /W 1 /H 1 /BPC 8 /CS /G ID " + strings.Repeat("x", a) +
			" EI\nBI /W 1 /H 1 /BPC 8 /CS /G ID " + strings.Repeat("y", b) + " EI\nQ\n"
	}
	for _, tc := range []struct {
		name string
		data string
		lost bool
	}{
		{"a comment read as the buffer drops its first bytes", bufferedPage(19, img+comment), true},
		{"a comment read one byte later", bufferedPage(20, img+comment), false},
		{"a comment read before the buffer drops them", bufferedPage(352, img+comment), false},
		{"a comment read one byte later again", bufferedPage(353, img+comment), true},
		{"a dictionary", bufferedPage(0, img+"<< "+strings.Repeat("/A 1 ", 240)+">> pop Q\n"), true},
		{"a short comment", bufferedPage(0, img+"%c\nQ\n"), false},
		{"a second image far longer", bufferedPage(0, chain(1, 3000)), true},
		{"a second image far shorter", bufferedPage(0, chain(3000, 1)), true},
		{"a second image as long", bufferedPage(0, chain(1, 1)), false},
		{"a short stream", img + comment, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ops, err := ParseStreamData(context.Background(), []byte(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			if lost := len(ops) == 0; lost != tc.lost {
				t.Errorf("read %d operations, want the stream lost: %t", len(ops), tc.lost)
			}
			for name, opt := range map[string]Option{"ForRepair": ForRepair(), "WholeStream": WholeStream(), "SplitGluedOperators": SplitGluedOperators()} {
				if ops, err := ParseStreamData(context.Background(), []byte(tc.data), opt); err != nil || len(ops) == 0 {
					t.Errorf("%s: read %d operations, %v", name, len(ops), err)
				}
			}
		})
	}
}

// TestVeraBufferHolds pins what the buffer holds as a reading moves on: the
// first 1024 bytes and the half buffer before them, then, past three quarters
// of the buffer, 1024 bytes behind the position, 513 bytes more at a time,
// until it holds the end of the stream.
func TestVeraBufferHolds(t *testing.T) {
	t.Parallel()

	b := newVeraBuffer(100_000)
	b.readTo(512)
	if !b.holds(0) || b.base != -veraBufferHalf {
		t.Fatalf("base %d after 512 bytes, want %d", b.base, -veraBufferHalf)
	}
	b.readTo(513)
	if b.base != 513-veraBufferHalf || b.holds(513-veraBufferHalf-1) {
		t.Fatalf("base %d after 513 bytes, want %d", b.base, 513-veraBufferHalf)
	}
	b.readTo(10_000)
	if want := 513*(10_000/513) - veraBufferHalf; b.base != want {
		t.Fatalf("base %d after 10000 bytes, want %d", b.base, want)
	}
	if b.holds(b.base+veraBufferSize) || !b.holds(b.base+veraBufferSize-1) {
		t.Error("the buffer holds a byte past its end, or not its last")
	}

	short := newVeraBuffer(3000)
	short.readTo(3000)
	if !short.end || !short.holds(short.base+veraBufferSize) {
		t.Errorf("a buffer that read a 3000-byte stream does not hold its end: base %d", short.base)
	}
	if whole := newVeraBuffer(1000); !whole.end || !whole.holds(0) {
		t.Error("a buffer over a 1000-byte stream does not hold it whole")
	}
}
