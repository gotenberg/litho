package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// deviceNFixture assembles objs as objects 1..n and appends the tint
// transform as object n+1. Every DEVN in an object stands for a DeviceN space
// one colourant past the PDF/A-1 cap, so the trim must rewrite it under
// PDF/A-1b.
func deviceNFixture(objs ...string) []byte {
	space := fmt.Sprintf("[/DeviceN [/Cyan /Magenta /Yellow /Black /Spot1 /Spot2 /Spot3 /Spot4 /Spot5] /DeviceCMYK %d 0 R]", len(objs)+1)
	out := make([]string, 0, len(objs)+1)
	for _, o := range objs {
		out = append(out, strings.ReplaceAll(o, "DEVN", space))
	}
	out = append(out, "<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0.1 0 0 0] /N 1 >>")
	return assemblePDF(out)
}

// deviceNStreamObj renders a stream object whose dictionary entries are dict.
func deviceNStreamObj(dict, body string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(body), body)
}

// reachableDeviceN returns the colourant count of every DeviceN space the
// output reaches from the catalog, reading each object through the allocator
// as the writer serialises it. /Parent is not followed, so the walk only
// descends.
func reachableDeviceN(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) []int {
	var counts []int
	seen := make(map[int]bool)
	var walk func(obj ast.PdfObject, depth int)
	walk = func(obj ast.PdfObject, depth int) {
		if depth > maxPageTreeDepth {
			return
		}
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if seen[ref.ObjectNum] {
				return
			}
			seen[ref.ObjectNum] = true
			live, pending := alloc.Pending()[ref.ObjectNum]
			if !pending {
				resolved, err := doc.Resolve(ctx, ref)
				if err != nil {
					return
				}
				live = resolved
			}
			obj = live
		}
		switch v := obj.(type) {
		case ast.PdfArray:
			if len(v) >= 2 && v[0] == ast.PdfName("DeviceN") {
				counts = append(counts, len(liveArray(ctx, alloc, doc, v[1])))
			}
			for _, e := range v {
				walk(e, depth+1)
			}
		case ast.PdfDict:
			for _, k := range slices.Sorted(maps.Keys(v)) {
				if k != "Parent" {
					walk(v[k], depth+1)
				}
			}
		case ast.PdfStream:
			walk(v.Dict, depth+1)
		}
	}
	walk(ast.PdfIndirectRef{ObjectNum: 1}, 0)
	return counts
}

const (
	deviceNCatalog = "<< /Type /Catalog /Pages 2 0 R >>"
	deviceNPages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
)

// TestTrimDeviceNColorants_ReachesEverySiteTheValidatorJudges pins that the
// trim reaches the DeviceN spaces compliance.checkSeparationDeviceN judges
// under ISO 19005-1 6.1.12 test 9 below the page's own /ColorSpace: in form
// XObjects, nested ones included, coloured tiling patterns, shadings and
// shading patterns, resources inherited from the page tree (ISO 32000-1,
// 7.7.3.4) and annotation appearance streams.
//
// The trim walked the page's own /Resources /ColorSpace only, so each of these
// kept its nine colourants and its test 9 violation in aggressive mode, in
// litho and in veraPDF. Every level is inline in some rows and indirect in
// others, so each write-back path is exercised.
func TestTrimDeviceNColorants_ReachesEverySiteTheValidatorJudges(t *testing.T) {
	t.Parallel()

	page := func(extra string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " + extra + " >>"
	}
	for _, tc := range []struct {
		name string
		pdf  []byte
	}{
		{"form XObject with inline resources", deviceNFixture(deviceNCatalog, deviceNPages,
			page("/Resources << /XObject << /Fm0 4 0 R >> >>"),
			deviceNStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /ColorSpace << /CS0 DEVN >> >>", "/CS0 cs"),
		)},
		{"nested form with indirect resources", deviceNFixture(deviceNCatalog, deviceNPages,
			page("/Resources << /XObject << /Fm0 4 0 R >> >>"),
			deviceNStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /XObject << /Fm1 5 0 R >> >>", "/Fm1 Do"),
			deviceNStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources 6 0 R", "/CS0 cs"),
			"<< /ColorSpace << /CS0 DEVN >> >>",
		)},
		{"coloured tiling pattern", deviceNFixture(deviceNCatalog, deviceNPages,
			page("/Resources << /Pattern << /P0 4 0 R >> >>"),
			deviceNStreamObj("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << /ColorSpace << /CS0 DEVN >> >>", "/CS0 cs"),
		)},
		{"indirect shading", deviceNFixture(deviceNCatalog, deviceNPages,
			page("/Resources << /Shading << /Sh0 4 0 R >> >>"),
			"<< /ShadingType 2 /Coords [0 0 1 0] /ColorSpace DEVN /Function 5 0 R >>",
		)},
		{"inline shading pattern", deviceNFixture(deviceNCatalog, deviceNPages,
			page("/Resources << /Pattern << /P0 << /PatternType 2 /Shading << /ShadingType 2 /Coords [0 0 1 0] /ColorSpace DEVN >> >> >> >>"),
		)},
		{"resources inherited from the page tree", deviceNFixture(deviceNCatalog,
			"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /ColorSpace << /CS0 DEVN >> >> >>",
			page(""),
		)},
		{"annotation appearance stream", deviceNFixture(deviceNCatalog, deviceNPages,
			page("/Annots [4 0 R]"),
			"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /AP << /N 5 0 R >> >>",
			deviceNStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /ColorSpace << /CS0 DEVN >> >>", "/CS0 cs"),
		)},
		{"annotation appearance subdictionary", deviceNFixture(deviceNCatalog, deviceNPages,
			page("/Annots [4 0 R]"),
			"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /AP << /D << /On 5 0 R >> >> >>",
			deviceNStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /ColorSpace << /CS0 DEVN >> >>", "/CS0 cs"),
		)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, tc.pdf)
			if got := reachableDeviceN(ctx, alloc, doc); !slices.Equal(got, []int{pdfA1DeviceNComponents + 1}) {
				t.Fatalf("precondition: the fixture must reach one DeviceN of %d colourants, got %v", pdfA1DeviceNComponents+1, got)
			}

			fixes := runDeviceNTrim(t, ctx, alloc, doc)

			if got := reachableDeviceN(ctx, alloc, doc); !slices.Equal(got, []int{pdfA1DeviceNComponents}) {
				t.Errorf("the output reaches DeviceN spaces of %v colourants, want [%d]; fixes = %v", got, pdfA1DeviceNComponents, fixes)
			}
			want := []string{fmt.Sprintf("reduced 1 DeviceN colour space(s) to %d components", pdfA1DeviceNComponents)}
			if !slices.Equal(fixes, want) {
				t.Errorf("fixes = %v, want %v", fixes, want)
			}
		})
	}
}

// TestTrimDeviceNColorants_LeavesWhatTheValidatorDoesNotJudge pins the two
// DeviceN sites the trim leaves alone, as compliance.checkSeparationDeviceN
// does for test 9: an image XObject's colour space, whose samples carry one
// component per colourant (ISO 32000-1, 8.9.5.2) so a trim would scramble the
// image, and the resources of an uncoloured tiling pattern, whose cell
// specifies no colour of its own (8.7.3.3).
func TestTrimDeviceNColorants_LeavesWhatTheValidatorDoesNotJudge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, deviceNFixture(deviceNCatalog, deviceNPages,
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im0 4 0 R >> /Pattern << /P0 5 0 R >> >> >>",
		deviceNStreamObj("/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace DEVN", "123456789"),
		deviceNStreamObj("/Type /Pattern /PatternType 1 /PaintType 2 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << /ColorSpace << /CS0 DEVN >> >>", "0 0 5 5 re f"),
	))

	fixes := runDeviceNTrim(t, ctx, alloc, doc)

	if len(fixes) != 0 {
		t.Errorf("fixes = %v, want none", fixes)
	}
	if pending := alloc.Pending(); len(pending) != 0 {
		t.Errorf("the trim wrote objects %v, want none", slices.Sorted(maps.Keys(pending)))
	}
}

// TestTrimDeviceNColorants_FormCycleTerminatesAndKeepsTheBody pins that a form
// whose /Resources names the form itself is walked once, and that the
// rewritten form keeps its content stream byte for byte.
func TestTrimDeviceNColorants_FormCycleTerminatesAndKeepsTheBody(t *testing.T) {
	t.Parallel()

	const body = "/CS0 cs 1 1 1 1 1 1 1 1 1 scn 0 0 5 5 re f /Fm0 Do"
	ctx := context.Background()
	doc, alloc := newTestDoc(t, deviceNFixture(deviceNCatalog, deviceNPages,
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Fm0 4 0 R >> >> >>",
		deviceNStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /ColorSpace << /CS0 DEVN >> /XObject << /Fm0 4 0 R >> >>", body),
	))

	fixes := runDeviceNTrim(t, ctx, alloc, doc)

	if got := reachableDeviceN(ctx, alloc, doc); !slices.Equal(got, []int{pdfA1DeviceNComponents}) {
		t.Errorf("the output reaches DeviceN spaces of %v colourants, want [%d]; fixes = %v", got, pdfA1DeviceNComponents, fixes)
	}
	form, ok := alloc.Pending()[4].(ast.PdfStream)
	if !ok {
		t.Fatalf("the form was not rewritten at object 4: %T", alloc.Pending()[4])
	}
	raw, err := form.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the rewritten form: %v", err)
	}
	if string(raw) != body {
		t.Errorf("the rewritten form's body is %q, want %q", raw, body)
	}
}

// TestTrimDeviceNColorants_KeepsOutputIntentsFormAndPatternWrites pins that
// the trim below the page starts from the forms and patterns the earlier
// fixers left. The peer is the real output-intent fixer, which precedes
// aggressive-limits: adding an sRGB OutputIntent, it injects /DefaultCMYK into
// the /Resources of every form XObject and pattern it reaches and writes each
// stream back at its own object number, with the /ColorSpace dictionary
// inlined. A trim built from the document's streams would drop that
// /DefaultCMYK, and one reading the document's /ColorSpace would trim the
// dictionary the output no longer references.
func TestTrimDeviceNColorants_KeepsOutputIntentsFormAndPatternWrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, deviceNFixture(deviceNCatalog, deviceNPages,
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Fm0 4 0 R >> /Pattern << /P0 5 0 R >> >> >>",
		deviceNStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /ColorSpace 6 0 R >>", "/CS0 cs"),
		deviceNStreamObj("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << /ColorSpace 7 0 R >>", "/CS0 cs"),
		"<< /CS0 DEVN >>",
		"<< /CS0 DEVN >>",
	))

	if _, err := fixOutputIntent(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixOutputIntent: %v", err)
	}
	liveCS := func(objNum int) ast.PdfDict {
		stream, ok := alloc.Pending()[objNum].(ast.PdfStream)
		if !ok {
			return nil
		}
		return liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, stream.Dict["Resources"])["ColorSpace"])
	}
	for _, objNum := range []int{4, 5} {
		if _, has := liveCS(objNum)["DefaultCMYK"]; !has {
			t.Fatalf("control: output-intent wrote no /DefaultCMYK into object %d, so this test proves nothing", objNum)
		}
	}

	fixes := runDeviceNTrim(t, ctx, alloc, doc)

	for _, objNum := range []int{4, 5} {
		cs := liveCS(objNum)
		if _, has := cs["DefaultCMYK"]; !has {
			t.Errorf("object %d lost the /DefaultCMYK output-intent wrote: the trim rebuilt it from the document; /ColorSpace = %v", objNum, cs)
		}
		arr := liveArray(ctx, alloc, doc, cs["CS0"])
		if len(arr) < 2 {
			t.Errorf("object %d's /CS0 is %v, want the DeviceN space", objNum, cs["CS0"])
			continue
		}
		if n := len(liveArray(ctx, alloc, doc, arr[1])); n != pdfA1DeviceNComponents {
			t.Errorf("object %d's /CS0 declares %d colourants, want %d; fixes = %v", objNum, n, pdfA1DeviceNComponents, fixes)
		}
	}
}
