package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// shadingDeviceN is a DeviceN over two spot colorants with no /Colorants, the
// shape ISO 19005-2, 6.2.4.4 t1 fails.
const shadingDeviceN = "[/DeviceN [/SpotA /SpotB] /DeviceCMYK 6 0 R]"

// shadingDeviceNPDF builds a page whose resources, or the /Pages node's when
// inherit is set, hold resources, with object 6 the DeviceN's tint transform
// and extra appended as objects 7 onward.
func shadingDeviceNPDF(resources string, inherit bool, extra ...string) []byte {
	pages := "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources " + resources + " >>"
	if inherit {
		pages = "<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources " + resources + " >>"
		page = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>"
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		pages,
		page,
		contentObj("/Pattern cs /P0 scn 0 0 10 10 re f /Sh0 sh"),
		"<< /Type /Nothing >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 0 0 1] /N 1 >>",
	}
	return assemblePDF(append(objs, extra...))
}

// liveShadingColourSpace follows path from the /Resources the page at object 3
// resolves its names against, each element a key of the dictionary or stream
// dictionary before it, and returns the colour space array it ends on as the
// output will carry it.
func liveShadingColourSpace(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, path ...ast.PdfName) ast.PdfArray {
	t.Helper()

	ctx := context.Background()
	holder, ok := resourcesHolder(ctx, alloc, doc, pageTreeEntries(ctx, alloc, doc)[3])
	if !ok {
		t.Fatal("the page resolves no /Resources")
	}
	cur := liveDictValue(ctx, alloc, doc, holder.dict["Resources"])
	for i, key := range path {
		obj := cur[key]
		if i == len(path)-1 {
			return liveArray(ctx, alloc, doc, obj)
		}
		if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
			if s, isStream := liveStream(ctx, alloc, doc, ref); isStream {
				cur = s.Dict
				continue
			}
		}
		cur = liveDictValue(ctx, alloc, doc, obj)
		if cur == nil {
			t.Fatalf("%v: /%s did not resolve", path, key)
		}
	}
	return nil
}

// TestFixDeviceNColorants_ReachesShadings pins that the /Colorants repair
// reaches a DeviceN a shading uses, through a shading pattern or the /Shading
// resources an sh operator paints.
//
// ISO 19005-2, 6.2.4.4 t1 holds every DeviceN a page paints with to the
// /Colorants rule, and compliance judges each one a colour link reaches, a
// shading's among them. The repair walked the page's /ColorSpace resources
// alone, so a shading's DeviceN kept its finding through remediation.
//
// # Corpus evidence
//
//   - pdfjs issue17065.pdf: the DeviceN of the shading pattern its page
//     inherits kept 6.2.4.4 t1 after a PDF/A-2b remediation, on litho and
//     veraPDF alike.
func TestFixDeviceNColorants_ReachesShadings(t *testing.T) {
	t.Parallel()

	const shading = "<< /ShadingType 2 /ColorSpace " + shadingDeviceN + " /Coords [0 0 1 0] /Function 6 0 R >>"
	for _, tc := range []struct {
		name string
		data []byte
		path []ast.PdfName
	}{
		{
			name: "inherited pattern, direct shading",
			data: shadingDeviceNPDF("<< /Pattern << /P0 7 0 R >> >>", true,
				"<< /PatternType 2 /Shading "+shading+" >>"),
			path: []ast.PdfName{"Pattern", "P0", "Shading", "ColorSpace"},
		},
		{
			name: "direct pattern",
			data: shadingDeviceNPDF("<< /Pattern << /P0 << /PatternType 2 /Shading "+shading+" >> >> >>", false),
			path: []ast.PdfName{"Pattern", "P0", "Shading", "ColorSpace"},
		},
		{
			name: "indirect shading",
			data: shadingDeviceNPDF("<< /Shading << /Sh0 7 0 R >> >>", false, shading),
			path: []ast.PdfName{"Shading", "Sh0", "ColorSpace"},
		},
		{
			name: "direct shading",
			data: shadingDeviceNPDF("<< /Shading << /Sh0 "+shading+" >> >>", false),
			path: []ast.PdfName{"Shading", "Sh0", "ColorSpace"},
		},
		{
			name: "indirect colour space",
			data: shadingDeviceNPDF("<< /Shading << /Sh0 << /ShadingType 2 /ColorSpace 7 0 R /Coords [0 0 1 0] /Function 6 0 R >> >> >>", false,
				shadingDeviceN),
			path: []ast.PdfName{"Shading", "Sh0", "ColorSpace"},
		},
		{
			name: "shading stream",
			data: shadingDeviceNPDF("<< /Shading << /Sh0 7 0 R >> >>", false,
				"<< /ShadingType 4 /ColorSpace "+shadingDeviceN+" /BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 8"+
					" /Decode [0 1 0 1 0 1 0 1] /Length 4 >>\nstream\n\x00\x00\x00\x00\nendstream"),
			path: []ast.PdfName{"Shading", "Sh0", "ColorSpace"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, tc.data)
			if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
				t.Fatalf("fixDeviceNColorants: %v", err)
			}
			cs := liveShadingColourSpace(t, alloc, doc, tc.path...)
			if name, _ := cs[0].(ast.PdfName); name != "DeviceN" {
				t.Fatalf("%v is %v, want the DeviceN", tc.path, cs)
			}
			if !hasColorants(cs) {
				t.Fatalf("the shading's DeviceN has no /Colorants: %v", cs)
			}
			attrs, _ := cs[4].(ast.PdfDict)
			colorants := liveDictValue(ctx, alloc, doc, attrs["Colorants"])
			for _, spot := range []ast.PdfName{"SpotA", "SpotB"} {
				if colorants[spot] == nil {
					t.Errorf("/Colorants lacks /%s: %v", spot, colorants)
				}
			}
		})
	}
}

// TestFixDeviceNColorants_KeepsAShadingStreamsBody pins that repairing the
// DeviceN of a shading stream carries its data across.
func TestFixDeviceNColorants_KeepsAShadingStreamsBody(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, shadingDeviceNPDF("<< /Shading << /Sh0 7 0 R >> >>", false,
		"<< /ShadingType 4 /ColorSpace "+shadingDeviceN+" /BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 8"+
			" /Decode [0 1 0 1 0 1 0 1] /Length 4 >>\nstream\n\x00\x01\x02\x03\nendstream"))
	if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}
	s, ok := alloc.Pending()[7].(ast.PdfStream)
	if !ok {
		t.Fatalf("the shading is %T, want a pending stream", alloc.Pending()[7])
	}
	body, err := doc.DecodeStream(ctx, s)
	if err != nil || string(body) != "\x00\x01\x02\x03" {
		t.Errorf("the shading's data is %q (%v), want %q", body, err, "\x00\x01\x02\x03")
	}
}

// TestFixDeviceNColorants_ReusesAShadingsSeparation pins that a colorant a
// shading's DeviceN lacks takes the Separation another shading paints with
// under the same name.
//
// ISO 19005-2, 6.2.4.4 t2 requires every Separation of one name to share an
// alternate space and a tint transform, so a degenerate transform invented for
// a name a shading already defines trades t1 for t2.
func TestFixDeviceNColorants_ReusesAShadingsSeparation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, shadingDeviceNPDF(
		"<< /Shading << /Sh0 << /ShadingType 2 /ColorSpace "+shadingDeviceN+" /Coords [0 0 1 0] /Function 6 0 R >>"+
			" /Sh1 << /ShadingType 2 /ColorSpace [/Separation /SpotA /DeviceCMYK 7 0 R] /Coords [0 0 1 0] /Function 7 0 R >> >> >>",
		false,
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 0 0] /N 1 >>"))
	if _, err := fixDeviceNColorants(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixDeviceNColorants: %v", err)
	}
	cs := liveShadingColourSpace(t, alloc, doc, "Shading", "Sh0", "ColorSpace")
	if !hasColorants(cs) {
		t.Fatalf("the shading's DeviceN has no /Colorants: %v", cs)
	}
	attrs, _ := cs[4].(ast.PdfDict)
	spotA, _ := liveDictValue(ctx, alloc, doc, attrs["Colorants"])["SpotA"].(ast.PdfArray)
	if len(spotA) < 4 || spotA[3] != (ast.PdfIndirectRef{ObjectNum: 7}) {
		t.Errorf("/SpotA = %v, want the Separation /Sh1 paints with, over tint transform 7", spotA)
	}
}
