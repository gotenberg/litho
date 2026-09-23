package image

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/plugins"
)

// filteredImagePDF builds a one-page document whose page draws /Im0, an 8x8
// image carrying entries and data, plus any extra objects numbered from 5.
func filteredImagePDF(entries string, data []byte, extra ...string) []string {
	return append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /XObject << /Im0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 %s /Length %d >>\nstream\n%s\nendstream",
			entries, len(data), data),
	}, extra...)
}

// TestDecodeImage_UndecodableFilterIsRefusedEvenWhenLongEnough pins the silent
// corruption the refusal exists to stop.
//
// The filter pipeline skips a filter it cannot decode and hands back the
// still-encoded bytes. An 8x8 image with no /ColorSpace is read as RGB, which
// wants 192 bytes, so a codestream of 231 bytes passed the length check and its
// bytes came back as pixels with no error. litho/pdfs/jpx_2b.pdf holds seven
// images of exactly this shape.
func TestDecodeImage_UndecodableFilterIsRefusedEvenWhenLongEnough(t *testing.T) {
	t.Parallel()

	codestream := bytes.Repeat([]byte{0xFF, 0x4F, 0xFF, 0x51, 0x00, 0x2F, 0x00}, 33)
	ctx, doc, page := imagePDF(t, filteredImagePDF("/Filter /JPXDecode", codestream))

	_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err == nil {
		t.Fatal("DecodeImage read 231 still-encoded JPXDecode bytes as an 8x8 RGB picture")
	}
	if !errors.Is(err, filter.ErrUnsupportedFilter) {
		t.Errorf("refusal does not wrap ErrUnsupportedFilter: %v", err)
	}
	if !strings.Contains(err.Error(), "JPXDecode") {
		t.Errorf("refusal does not name the filter: %v", err)
	}
}

// TestDecodeImage_UndecodableFilterIsNamedNotBlamedOnTheData pins that a
// refusal names the filter litho lacks instead of reporting the data short.
//
// Before, a short codestream failed as "decoded data too short for RGB image",
// which points at the file. Each case checks the named filter and, where the
// chain also holds a decodable filter, that only the undecodable one is named.
func TestDecodeImage_UndecodableFilterIsNamedNotBlamedOnTheData(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		entries string
		extra   []string
		want    string
		notWant string
	}{
		{
			name:    "stencil behind an unknown filter",
			entries: "/ImageMask true /Filter /NoSuchStencilDecode",
			want:    "NoSuchStencilDecode",
		},
		{
			name:    "JPX behind a decodable filter",
			entries: "/ColorSpace /DeviceGray /BitsPerComponent 8 /Filter [/FlateDecode /JPXDecode]",
			want:    "JPXDecode",
			notWant: "FlateDecode",
		},
		{
			name:    "unknown filter name",
			entries: "/ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /NoSuchDecode",
			want:    "NoSuchDecode",
		},
		{
			// The decode path resolves an indirect /Filter, so the check must too.
			name:    "indirect /Filter",
			entries: "/ColorSpace /DeviceGray /BitsPerComponent 8 /Filter 5 0 R",
			extra:   []string{"/JPXDecode"},
			want:    "JPXDecode",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, doc, page := imagePDF(t, filteredImagePDF(tc.entries, []byte("short"), tc.extra...))
			_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
			if err == nil {
				t.Fatal("DecodeImage accepted an image it cannot decode")
			}
			if !errors.Is(err, filter.ErrUnsupportedFilter) {
				t.Errorf("refusal does not wrap ErrUnsupportedFilter: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal does not name %s: %v", tc.want, err)
			}
			if tc.notWant != "" && strings.Contains(err.Error(), tc.notWant) {
				t.Errorf("refusal names the decodable %s too: %v", tc.notWant, err)
			}
		})
	}
}

// stubPlugin decodes one filter name to fixed output.
type stubPlugin struct {
	name string
	out  []byte
}

func (p stubPlugin) FilterName() string { return p.name }

func (p stubPlugin) Decode(_ []byte, _ map[string]any) ([]byte, error) { return p.out, nil }

// TestDecodeImage_FilterARegisteredPluginHandlesIsNotRefused pins that the
// refusal is keyed on what the document can decode, not on the filter name: a
// registered plugin makes JPXDecode decodable, and its output must be used.
func TestDecodeImage_FilterARegisteredPluginHandlesIsNotRefused(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, filteredImagePDF("/Filter /JPXDecode", []byte("codestream")))
	doc.Filters = filter.NewDecoder([]plugins.FilterDecoder{
		stubPlugin{name: "JPXDecode", out: bytes.Repeat([]byte{0x80}, 8*8*3)},
	})

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage refused a filter a registered plugin decodes: %v", err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r>>8 != 0x80 || g>>8 != 0x80 || b>>8 != 0x80 {
		t.Errorf("pixel (0,0) = (%d,%d,%d), want the plugin's grey 128", r>>8, g>>8, b>>8)
	}
}
