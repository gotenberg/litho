package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// shadingPatternIntentPDF builds a one-page document whose sole output intent
// carries the sRGB profile, over a page that paints DeviceCMYK and the shading
// pattern resources names: the state the second pass of a remediation meets
// once the first replaced a CMYK intent with an sRGB one to cover DeviceRGB.
// Objects 1 to 7 are the catalog, the page tree, the page, the intent, its
// profile, the content and a one-in four-out tint transform; extra objects
// start at 8.
func shadingPatternIntentPDF(resources string, extra ...string) []byte {
	icc := sRGBICCProfile
	const content = "0 0 0 1 k 0 0 10 10 re f /Pattern cs /P0 scn 20 20 50 50 re f"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /Resources " + resources + " >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) /DestOutputProfile 5 0 R >>",
		fmt.Sprintf("<< /N 3 /Length %d >>\nstream\n%s\nendstream", len(icc), icc),
		contentObj(content),
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [1 0 0 0] /N 1 >>",
	}
	return assemblePDF(append(objs, extra...))
}

// TestFixOutputIntent_GivesAShadingPatternTheDefaultSpace pins that the
// Default colour space remediation injects beside a new sRGB intent reaches
// the shading of a shading pattern too, which has no resources of its own for
// a Default to apply from (ISO 32000-1, 8.7.4.1): its device family becomes
// the Default's ICCBased space, as the space itself, a Separation or DeviceN
// alternate, a /Colorants entry or a process space, while a family the Default
// does not cover stays.
//
// veraPDF 1.30.2 judges a shading pattern's device family against the output
// intent alone, so the shading kept ISO 19005-2, 6.2.4.3 t3 after the intent
// turned sRGB.
func TestFixOutputIntent_GivesAShadingPatternTheDefaultSpace(t *testing.T) {
	t.Parallel()

	const sep = "[/Separation /Spot /DeviceCMYK 7 0 R]"
	const shadingDict = "/ShadingType 2 /Coords [0 0 1 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >>"
	const mesh = "0 0 0 0 0 0 10 0 0 0 5 10 0"
	cases := []struct {
		name string
		pdf  []byte
		// csAt reads the shading's colour space as the output will carry it.
		csAt []ast.PdfName
		obj  int
		// keep is a device family that must survive the rewrite.
		keep ast.PdfName
	}{
		{
			name: "an indirect Separation shading of an indirect pattern",
			pdf: shadingPatternIntentPDF("<< /Pattern << /P0 8 0 R >> >>",
				"<< /PatternType 2 /Shading 9 0 R >>",
				"<< "+shadingDict+" /ColorSpace "+sep+" >>"),
			obj: 9,
		},
		{
			name: "a direct DeviceCMYK shading of a direct pattern",
			pdf: shadingPatternIntentPDF("<< /Pattern << /P0 << /PatternType 2 /Shading << " +
				"/ShadingType 2 /Coords [0 0 1 0] /Function 7 0 R /ColorSpace /DeviceCMYK >> >> >> >>"),
			csAt: []ast.PdfName{"Pattern", "P0", "Shading", "ColorSpace"},
		},
		{
			name: "a DeviceN shading whose colorant and process space are DeviceCMYK",
			pdf: shadingPatternIntentPDF("<< /Pattern << /P0 8 0 R >> >>",
				"<< /PatternType 2 /Shading << "+shadingDict+" /ColorSpace [/DeviceN [/Spot] /DeviceCMYK 7 0 R "+
					"<< /Colorants << /Spot "+sep+" >> /Process << /ColorSpace /DeviceCMYK /Components [/Cyan /Magenta /Yellow /Black] >> >>] >> >>"),
			csAt: []ast.PdfName{"Pattern", "P0", "Shading", "ColorSpace"},
		},
		{
			name: "a mesh shading stream",
			pdf: shadingPatternIntentPDF("<< /Pattern << /P0 8 0 R >> >>",
				"<< /PatternType 2 /Shading 9 0 R >>",
				fmt.Sprintf("<< /ShadingType 4 /ColorSpace %s /BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 8 "+
					"/Decode [0 1 0 1 0 1] /Function 7 0 R /Length %d >>\nstream\n%s\nendstream", sep, len(mesh), mesh)),
			obj: 9,
		},
		{
			name: "a Separation over DeviceRGB",
			pdf: shadingPatternIntentPDF("<< /Pattern << /P0 8 0 R >> >>",
				"<< /PatternType 2 /Shading 9 0 R >>",
				"<< "+shadingDict+" /ColorSpace [/Separation /Spot /DeviceRGB << /FunctionType 2 /Domain [0 1] "+
					"/C0 [0 0 0] /C1 [1 0 0] /N 1 >>] >>"),
			obj:  9,
			keep: "DeviceRGB",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, err := document.NewDocument(ctx, bytes.NewReader(tc.pdf), int64(len(tc.pdf)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
			violations := []compliance.Violation{{
				Rule:    compliance.Rule{Clause: "6.2.4.3"},
				Context: "page 1: DeviceCMYK used without DefaultCMYK or CMYK OutputIntent",
			}}
			if _, err := fixOutputIntent(ctx, alloc, doc, Options{}, violations, "PDF/A-2b"); err != nil {
				t.Fatalf("fixOutputIntent: %v", err)
			}

			pageRes := liveDictValue(ctx, alloc, doc, livePageResources(t, alloc, doc))
			defaultCMYK, _ := liveDictValue(ctx, alloc, doc, pageRes["ColorSpace"])["DefaultCMYK"].(ast.PdfArray)
			if len(defaultCMYK) != 2 {
				t.Fatalf("control: the page carries no /DefaultCMYK to give the shading: %v", pageRes)
			}

			var cs ast.PdfObject
			if tc.obj > 0 {
				obj := liveObject(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: tc.obj})
				switch sh := obj.(type) {
				case ast.PdfDict:
					cs = sh["ColorSpace"]
				case ast.PdfStream:
					cs = sh.Dict["ColorSpace"]
					body, err := sh.Bytes(ctx)
					if err != nil || string(body) != mesh {
						t.Errorf("the mesh body is %q (%v), want it kept", body, err)
					}
				}
			} else {
				cur := pageRes
				for _, key := range tc.csAt[:len(tc.csAt)-1] {
					cur = liveDictValue(ctx, alloc, doc, cur[key])
				}
				cs = cur[tc.csAt[len(tc.csAt)-1]]
			}

			names := spaceNames(ctx, alloc, doc, cs, 0)
			if names["DeviceCMYK"] {
				t.Errorf("the shading's colour space %v still holds DeviceCMYK", cs)
			}
			if tc.keep != "" {
				if !names[tc.keep] {
					t.Errorf("the shading's colour space %v lost %s, which no Default covers", cs, tc.keep)
				}
				if names["ICCBased"] {
					t.Errorf("the shading's colour space %v gained an ICCBased space it had no family for", cs)
				}
				return
			}
			if !names["ICCBased"] {
				t.Errorf("the shading's colour space %v does not hold the Default's ICCBased space %v", cs, defaultCMYK)
			}
		})
	}
}

// livePageResources returns the /Resources value of the page at object 3 as
// the output will carry it.
func livePageResources(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) ast.PdfObject {
	t.Helper()

	page := resolveFromAllocOrDoc(context.Background(), alloc, doc, ast.PdfIndirectRef{ObjectNum: 3})
	if page == nil {
		t.Fatal("the page at object 3 does not resolve")
	}
	return page["Resources"]
}

// spaceNames collects every name a colour space holds, at any depth, reading
// each reference as the output will carry it.
func spaceNames(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, depth int) map[ast.PdfName]bool {
	out := map[ast.PdfName]bool{}
	if depth > maxDefaultSpaceNesting {
		return out
	}
	switch v := obj.(type) {
	case ast.PdfName:
		out[v] = true
	case ast.PdfIndirectRef:
		switch r := liveObject(ctx, alloc, doc, v).(type) {
		case ast.PdfName, ast.PdfArray, ast.PdfDict:
			for k := range spaceNames(ctx, alloc, doc, r, depth+1) {
				out[k] = true
			}
		}
	case ast.PdfArray:
		for _, e := range v {
			for k := range spaceNames(ctx, alloc, doc, e, depth+1) {
				out[k] = true
			}
		}
	case ast.PdfDict:
		for _, e := range v {
			for k := range spaceNames(ctx, alloc, doc, e, depth+1) {
				out[k] = true
			}
		}
	}
	return out
}

// TestInjectDefaultRGB_GivesAShadingPatternTheDefaultSpace pins the other
// direction: a /DefaultRGB injected beside a CMYK intent gives a DeviceRGB
// shading of a shading pattern its ICCBased space too, since no Default
// reaches that shading either (ISO 32000-1, 8.7.4.1).
func TestInjectDefaultRGB_GivesAShadingPatternTheDefaultSpace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, shadingDeviceNPDF("<< /Pattern << /P0 7 0 R >> >>", false,
		"<< /PatternType 2 /Shading << /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 1 0] /Function 6 0 R >> >>"))
	if n := injectTestDefaultRGB(t, alloc, doc); n == 0 {
		t.Fatal("control: output-intent patched no page")
	}
	pattern := liveObject(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 7})
	dict, _ := pattern.(ast.PdfDict)
	shading, _ := dict["Shading"].(ast.PdfDict)
	names := spaceNames(ctx, alloc, doc, shading["ColorSpace"], 0)
	if names["DeviceRGB"] || !names["ICCBased"] {
		t.Errorf("the shading's colour space is %v, want the /DefaultRGB ICCBased space", shading["ColorSpace"])
	}
}
