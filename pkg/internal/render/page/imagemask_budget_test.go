package page

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// maskedRed is a page that paints a 1x1 red image over the whole page
// through mask, the image's /SMask or /Mask entry pointing at object 6.
func maskedRed(entry, mask string) fixture {
	return fixture{
		page:    "/Resources << /XObject << /Im 5 0 R >> >>",
		content: "q 100 0 0 100 0 0 cm /Im Do Q",
		objs: []string{
			stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /"+entry+" 6 0 R",
				"\xff\x00\x00"),
			mask,
		},
	}
}

// TestImageMaskPastBudget checks that a mask past the image budget still
// masks (ISO 32000-1, 11.6.5.3 and 8.9.6.3), read at the resolution it is
// drawn at, where leaving it out painted the image opaque.
//
// # Corpus evidence
//
//   - pdfjs issue16263.pdf: a 2x2 image under a 34862x4332 one-bit soft mask
//     painted 40 black bars where poppler 26.09.0 and PDFium draw arrows.
func TestImageMaskPastBudget(t *testing.T) {
	t.Parallel()

	// 200x100 samples: the left half 0, the right half opaque. Under a
	// budget of 10,000 pixels the mask is refused whole.
	row := strings.Repeat("\x00", 100) + strings.Repeat("\xff", 100)
	lim := limits.Limits{MaxImagePixels: 10_000}
	for _, tc := range []struct {
		name, entry, mask string
	}{
		{"soft mask", "SMask", stream("/Type /XObject /Subtype /Image /Width 200 /Height 100 /ColorSpace /DeviceGray /BitsPerComponent 8",
			strings.Repeat(row, 100))},
		{"stencil mask", "Mask", stream("/Type /XObject /Subtype /Image /Width 200 /Height 100 /ImageMask true /Decode [1 0]",
			strings.Repeat(strings.Repeat("\x00", 12)+"\x0f"+strings.Repeat("\xff", 12), 100))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, pg := open(t, maskedRed(tc.entry, tc.mask).bytes(), lim)
			c, report, err := Render(context.Background(), doc, pg, maskedRed(tc.entry, tc.mask).options())
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if report.Unsupported[Skipped] != 0 {
				t.Errorf("the image was skipped: %v", report.Unsupported)
			}
			expect(t, c.Image(), 3, map[[2]int]rgb{{20, 50}: paper, {80, 50}: red})
		})
	}
}

// TestStencilImagePastBudget checks that an image mask (ISO 32000-1,
// 8.9.6.2) past the image budget paints the fill colour through its samples
// read at the resolution it is drawn at, where it was left out.
//
// # Corpus evidence
//
//   - pdfjs issue8076.pdf: a 16667x16667 CCITT stencil drew nothing, and
//     drawn it matches poppler 26.09.0.
func TestStencilImagePastBudget(t *testing.T) {
	t.Parallel()
	f := fixture{
		page:    "/Resources << /XObject << /M 5 0 R >> >>",
		content: "1 0 0 rg q 100 0 0 100 0 0 cm /M Do Q",
		objs: []string{
			stream("/Type /XObject /Subtype /Image /Width 200 /Height 100 /ImageMask true /Decode [1 0]",
				strings.Repeat(strings.Repeat("\x00", 12)+"\x0f"+strings.Repeat("\xff", 12), 100)),
		},
	}
	doc, pg := open(t, f.bytes(), limits.Limits{MaxImagePixels: 10_000})
	c, report, err := Render(context.Background(), doc, pg, f.options())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if report.Unsupported[Skipped] != 0 {
		t.Errorf("the image mask was skipped: %v", report.Unsupported)
	}
	expect(t, c.Image(), 3, map[[2]int]rgb{{20, 50}: paper, {80, 50}: red})
}

// TestImageMaskUnreadableFailsClosed checks that an image whose mask is
// present but cannot be read is not painted: painting it opaque shows what
// the mask hides (ISO 32000-1, 8.9.6 and 11.6.5.3).
func TestImageMaskUnreadableFailsClosed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, entry, mask string
	}{
		{"soft mask of an unknown filter", "SMask",
			stream("/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /VendorDecode", "\x00\x00\x00\x00")},
		{"soft mask holding no sample", "SMask",
			stream("/Type /XObject /Subtype /Image /Width 64 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8", "")},
		{"stencil mask holding no sample", "Mask",
			stream("/Type /XObject /Subtype /Image /Width 64 /Height 2 /ImageMask true", "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			img, report := draw(t, maskedRed(tc.entry, tc.mask))
			if report.Unsupported[Skipped] == 0 {
				t.Errorf("the image was not reported skipped: %v", report.Unsupported)
			}
			expect(t, img, 0, map[[2]int]rgb{{20, 50}: paper, {80, 50}: paper})
		})
	}

	t.Run("control: /SMask /None masks nothing", func(t *testing.T) {
		t.Parallel()
		f := maskedRed("SMask", "/None")
		f.objs[0] = strings.Replace(f.objs[0], "6 0 R", "/None", 1)
		img, _ := draw(t, f)
		expect(t, img, 0, map[[2]int]rgb{{20, 50}: red, {80, 50}: red})
	})
}

// TestMattedSoftMaskTakesImageDimensions checks that a soft mask carrying a
// /Matte whose /Height is no integer still masks: ISO 32000-1, 11.6.5.3
// gives a matted soft mask its image's dimensions, and the decoder unmattes
// the image with the same reading.
//
// # Corpus evidence
//
//   - pdfjs issue19611.pdf: the matted soft mask of IM19 states /Height
//     /Length; PDF.js masks the image with it.
func TestMattedSoftMaskTakesImageDimensions(t *testing.T) {
	t.Parallel()
	img, report := draw(t, fixture{
		page:    "/Resources << /XObject << /Im 5 0 R >> >>",
		content: "q 100 0 0 100 0 0 cm /Im Do Q",
		objs: []string{
			stream("/Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /SMask 6 0 R",
				"\x80\x00\x00\xff\x00\x00"),
			stream("/Type /XObject /Subtype /Image /Width 2 /Height /Length /ColorSpace /DeviceGray /BitsPerComponent 8 /Matte [0 0 0]",
				"\x80\xff"),
		},
	})
	if report.Unsupported[Skipped] != 0 {
		t.Errorf("the image was skipped: %v", report.Unsupported)
	}
	// The left sample is red preblended with black at half opacity: unmatted
	// and composited at that opacity over white, it is pink, where painted
	// opaque it stays red.
	expect(t, img, 3, map[[2]int]rgb{{20, 50}: {255, 127, 127}, {80, 50}: red})
}
