package image

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/limits"
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
// wants 192 bytes, so 231 encoded bytes passed the length check and came back
// as pixels with no error.
//
// # Corpus evidence
//
//   - litho jpx_2b.pdf: held seven JPXDecode images of exactly this shape
//     while JPXDecode needed a plugin.
func TestDecodeImage_UndecodableFilterIsRefusedEvenWhenLongEnough(t *testing.T) {
	t.Parallel()

	encoded := bytes.Repeat([]byte{0xFF, 0x4F, 0xFF, 0x51, 0x00, 0x2F, 0x00}, 33)
	ctx, doc, page := imagePDF(t, filteredImagePDF("/Filter /NoSuchDecode", encoded))

	_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err == nil {
		t.Fatal("DecodeImage read 231 still-encoded bytes as an 8x8 RGB picture")
	}
	if !errors.Is(err, filter.ErrUnsupportedFilter) {
		t.Errorf("refusal does not wrap ErrUnsupportedFilter: %v", err)
	}
	if !strings.Contains(err.Error(), "NoSuchDecode") {
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
			name:    "unknown filter behind a decodable filter",
			entries: "/ColorSpace /DeviceGray /BitsPerComponent 8 /Filter [/FlateDecode /NoSuchDecode]",
			want:    "NoSuchDecode",
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
			extra:   []string{"/NoSuchDecode"},
			want:    "NoSuchDecode",
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
// registered plugin makes a filter decodable, and its output must be used.
func TestDecodeImage_FilterARegisteredPluginHandlesIsNotRefused(t *testing.T) {
	t.Parallel()

	ctx, doc, page := imagePDF(t, filteredImagePDF("/Filter /VendorDecode", []byte("encoded")))
	doc.Filters = filter.NewDecoder([]plugins.FilterDecoder{
		stubPlugin{name: "VendorDecode", out: bytes.Repeat([]byte{0x80}, 8*8*3)},
	}, limits.Limits{})

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage refused a filter a registered plugin decodes: %v", err)
	}
	r, g, b, _ := img.At(0, 0).RGBA()
	if r>>8 != 0x80 || g>>8 != 0x80 || b>>8 != 0x80 {
		t.Errorf("pixel (0,0) = (%d,%d,%d), want the plugin's grey 128", r>>8, g>>8, b>>8)
	}
}

// decliningPlugin claims one filter name and declines every stream.
type decliningPlugin struct{ name string }

func (p decliningPlugin) FilterName() string { return p.name }

func (decliningPlugin) Decode(_ []byte, _ map[string]any) ([]byte, error) {
	return nil, plugins.ErrUnsupportedFilter
}

// TestDecodeImage_FilterAPluginDeclinesIsRefused pins the other side of the
// plugin contract: the refusal above lets a claimed filter through, so a
// plugin that declines a filter litho has no decoder for must fail the
// decode. Skipping the filter instead would hand the 192 still-encoded bytes
// back as an 8x8 RGB picture.
func TestDecodeImage_FilterAPluginDeclinesIsRefused(t *testing.T) {
	t.Parallel()

	encoded := bytes.Repeat([]byte{0x80}, 8*8*3)
	ctx, doc, page := imagePDF(t, filteredImagePDF("/Filter /VendorDecode", encoded))
	doc.Filters = filter.NewDecoder([]plugins.FilterDecoder{decliningPlugin{name: "VendorDecode"}}, limits.Limits{})

	_, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err == nil {
		t.Fatal("DecodeImage read a stream its plugin declined as a picture")
	}
	if !errors.Is(err, filter.ErrUnsupportedFilter) {
		t.Errorf("refusal does not wrap ErrUnsupportedFilter: %v", err)
	}
}

// jpxPart2Header opens a JPEG 2000 codestream whose Rsiz of 8000h signals
// ISO/IEC 15444-2 capabilities, which the JPXDecode decoder refuses.
var jpxPart2Header = []byte{
	0xFF, 0x4F, 0xFF, 0x51, 0x00, 0x29, 0x80, 0x00,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x09,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x09,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x01, 0x07, 0x01, 0x01,
}

// jbig2NecessaryExtension is a JBIG2 page followed by an extension segment
// flagged necessary (T.88, 7.4.14), which the JBIG2Decode decoder refuses.
func jbig2NecessaryExtension() []byte {
	page := binary.BigEndian.AppendUint32(nil, 0)
	page = append(page, 48, 0, 1)
	page = binary.BigEndian.AppendUint32(page, 19)
	page = append(page, 0, 0, 0, 8, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	ext := binary.BigEndian.AppendUint32(nil, 1)
	ext = append(ext, 62, 0, 1)
	ext = binary.BigEndian.AppendUint32(ext, 4)
	return append(page, append(ext, 0xA0, 0, 0, 1)...)
}

// TestDecodeImage_UnsupportedFeatureIsRefused pins that an image whose
// built-in decoder refuses a feature it lacks is refused like one behind a
// filter no decoder knows, naming the filter.
//
// The pipeline skips such a filter and hands its input on, and the refusal
// above keyed on filter names alone, so each of these 8x8 one-bit images came
// back as a picture read from codestream bytes, or from encrypted ones.
func TestDecodeImage_UnsupportedFeatureIsRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		filter, entries string
		data            []byte
	}{
		{"JPXDecode", "/Filter /JPXDecode", jpxPart2Header},
		{"JBIG2Decode", "/Filter /JBIG2Decode", jbig2NecessaryExtension()},
		{"CCITTFaxDecode", "/Filter /CCITTFaxDecode /DecodeParms << /K -1 /Columns 8 /Rows 8 >>",
			[]byte{0x03, 0xC0, 0, 0, 0, 0, 0, 0}},
		{"Crypt", "/Filter /Crypt /DecodeParms << /Name /OtherCF >>", []byte("still encrypted")},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			t.Parallel()

			entries := "/ColorSpace /DeviceGray /BitsPerComponent 1 " + tc.entries
			ctx, doc, page := imagePDF(t, filteredImagePDF(entries, tc.data))
			img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
			if err == nil {
				t.Fatalf("DecodeImage read a stream %s refuses as a %v picture", tc.filter, img.Bounds())
			}
			if !errors.Is(err, filter.ErrUnsupportedFilter) {
				t.Errorf("refusal does not wrap ErrUnsupportedFilter: %v", err)
			}
			if !strings.Contains(err.Error(), tc.filter) {
				t.Errorf("refusal does not name %s: %v", tc.filter, err)
			}
		})
	}
}
