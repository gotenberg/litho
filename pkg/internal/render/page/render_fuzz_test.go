package page

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// fuzzResources are the resources of the page [FuzzRender] draws: an image,
// a stencil mask, a form, a transparency group with a soft mask, a shading,
// tiling and shading patterns, a spot colour and graphics states with blend
// modes, constant alpha and a luminosity soft mask, objects 5 onwards.
const fuzzResources = "/Resources << " +
	"/XObject << /Im 5 0 R /Mk 6 0 R /Fm 7 0 R /Gp 8 0 R >> " +
	"/Shading << /Sh 9 0 R >> " +
	"/Pattern << /Tp 10 0 R /Sp << /PatternType 2 /Shading 9 0 R >> >> " +
	"/ColorSpace << /Spot [/Separation /Gold /DeviceCMYK << /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 0.3 0.9 0.1] /N 1 >>] >> " +
	"/ExtGState << /Mul << /BM /Multiply /ca 0.5 /CA 0.5 >> " +
	"/Sm << /SMask << /S /Luminosity /G 8 0 R >> >> /Ko << /TK false /BM /Difference >> >> " +
	"/Font << /F << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>"

// fuzzObjects are the objects [fuzzResources] names.
func fuzzObjects() []string {
	return []string{
		stream("/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8", "\xff\x00\x00\x00\xff\x00\x00\x00\xff\x80\x80\x80"),
		stream("/Type /XObject /Subtype /Image /Width 2 /Height 2 /ImageMask true /Decode [1 0]", "\x80\x40"),
		stream("/Type /XObject /Subtype /Form /BBox [0 0 50 50]", "0 0 1 rg 0 0 25 25 re f"),
		stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /I true /K true /CS /DeviceCMYK >>", "0.5 g 10 10 80 80 re f /Mul gs 1 0 0 rg 20 20 40 40 re f"),
		"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 100 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> /Extend [true true] >>",
		stream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << >>", "1 0 0 rg 0 0 5 5 re f"),
	}
}

// fuzzContent is a content stream that reaches every resource of
// [fuzzResources], the seed [FuzzRender] mutates.
const fuzzContent = "q /Mul gs 1 0 0 rg 0 0 50 50 re f Q " +
	"q 100 0 0 100 0 0 cm /Im Do Q q 0 1 0 rg 50 0 0 50 50 0 cm /Mk Do Q " +
	"q /Sm gs /Fm Do Q /Gp Do q 10 10 80 80 re W n /Sh sh Q " +
	"/Pattern cs /Tp scn 0 50 50 50 re f /Sp scn 50 50 50 50 re f " +
	"/Spot cs 0.7 scn 20 20 m 80 20 l 50 80 l h f* " +
	"q 3 w 1 J 1 j [4 2] 0 d 0 0 m 100 100 l S Q " +
	"BT /F 12 Tf 7 Tr 10 10 Td (Hi) Tj ET /Ko gs BI /W 1 /H 1 /CS /G /BPC 8 ID \x80 EI"

// FuzzRender draws a page whose content stream is the fuzzed bytes, over a
// fixed set of resources, in sRGB or for a CMYK device and through every
// page boundary, on a small pixel budget. The renderer never panics.
func FuzzRender(f *testing.F) {
	f.Add([]byte(fuzzContent), uint8(0))
	f.Add([]byte(fuzzContent[:len(fuzzContent)/2]), uint8(9))
	objs := fuzzObjects()
	f.Fuzz(func(t *testing.T, content []byte, mode uint8) {
		fx := fixture{page: "/CropBox [0 0 90 90] /BleedBox [-5 -5 95 95] " + fuzzResources, content: string(content), objs: objs}
		data := fx.bytes()
		ctx := context.Background()
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, document.WithLimits(limits.Limits{MaxRenderPixels: 1 << 15}))
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		catalog, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			t.Fatalf("ResolveCatalog: %v", err)
		}
		pages, err := document.ResolvePages(ctx, doc, catalog)
		if err != nil || len(pages) == 0 {
			t.Fatalf("ResolvePages: %d pages, %v", len(pages), err)
		}
		opts := fx.options()
		opts.Box = Box(mode % 5)
		opts.CMYK = mode&8 != 0
		opts.GroupSpace = mode&16 != 0
		res, _, err := RenderGroup(ctx, doc, pages[0], opts)
		if err != nil {
			return
		}
		if opts.CMYK {
			_, err = res.CMYK(ctx)
		} else {
			_, err = res.Canvas(ctx)
		}
		if err != nil {
			t.Fatalf("reading the page: %v", err)
		}
	})
}
