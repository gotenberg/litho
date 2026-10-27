package remediation

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/models"
)

// codecJBIG2 is an embedded JBIG2 stream (ISO 32000-1, 7.4.7) of an 8x8
// diagonal band: a page information segment, an immediate lossless generic
// region coded with MMR (ITU-T T.88, 6.2.6) and an end of page segment, the
// data of the litho fixture inlinejpx_2b.pdf.
func codecJBIG2(t *testing.T) []byte {
	t.Helper()
	g4, err := hex.DecodeString("26bdbb76eddbb64d5380080080")
	if err != nil {
		t.Fatal(err)
	}
	segment := func(n uint32, typ byte, data []byte) []byte {
		b := binary.BigEndian.AppendUint32(nil, n)
		b = append(b, typ, 0, 1)
		b = binary.BigEndian.AppendUint32(b, uint32(len(data))) //nolint:gosec // test data, a few bytes
		return append(b, data...)
	}
	region := func(extra ...byte) []byte {
		b := binary.BigEndian.AppendUint32(nil, 8)
		b = binary.BigEndian.AppendUint32(b, 8)
		b = binary.BigEndian.AppendUint32(b, 0)
		b = binary.BigEndian.AppendUint32(b, 0)
		return append(b, extra...)
	}
	out := segment(0, 48, region(0, 0, 0))
	out = append(out, segment(1, 39, append(region(0, 1), g4...))...)
	return append(out, segment(2, 49, nil)...)
}

// codecJPX is a JP2 file around the three-component 8x8 codestream of the
// litho corpus builders, with one sRGB colour specification.
const codecJPX = "0000000c6a5020200d0a870a00000014667479706a703220000000006a703220" +
	"0000002d6a703268000000166968647200000008000000080003070700000000" +
	"000f636f6c72010001000000100000008f6a703263ff4fff51002f0000000000" +
	"0800000008000000000000000000000008000000080000000000000000000307" +
	"0101070101070101ff52000c00000001010004040001ff5c00044040ff90000a" +
	"00000000003e0001ff93c3e718115054af823d4800018484bfc7d418115054a2" +
	"7db690000309097fcfb43c14005caf850d1400000018484bff7fffd9"

// codecStreams are the objects of [codecPDF] that hold the inline image, each
// with the object whose /Resources its names resolve in.
var codecStreams = []struct {
	name          string
	objNum, owner int
}{
	{"page content", 4, 3},
	{"form XObject", 5, 5},
	{"tiling pattern cell", 7, 7},
	{"annotation appearance", 9, 9},
	{"Type 3 glyph", 10, 6},
}

// codecPDF builds the document of [inlineFilterPDF], drawing image in every
// kind of stream compliance judges inline images in, with a resource
// dictionary on every stream: formResources is the form XObject's /Resources
// entry, or none when empty.
func codecPDF(image, formResources string) []byte {
	img := "q 10 0 0 10 0 0 cm " + image + " Q"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Annots [8 0 R]"+
			" /Resources << /XObject << /X0 5 0 R >> /Font << /T3 6 0 R >> /Pattern << /P0 7 0 R >>"+
			" /ColorSpace << /CS0 [/Indexed /DeviceGray 1 <00FF>] >> >> >>",
		streamBody("", img+" /X0 Do q BT /T3 12 Tf 10 10 Td (a) Tj ET Q q /Pattern cs /P0 scn 0 0 50 50 re f Q"),
		streamBody("/Type /XObject /Subtype /Form /BBox [0 0 10 10] "+formResources, img),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]"+
			" /CharProcs << /a 10 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >>"+
			" /FirstChar 97 /LastChar 97 /Widths [1000] /Resources << >> >>",
		streamBody("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10]"+
			" /XStep 10 /YStep 10 /Resources << >>", img),
		"<< /Type /Annot /Subtype /Square /Rect [60 10 80 30] /F 4 /AP << /N 9 0 R >> >>",
		streamBody("/BBox [0 0 20 20] /Resources 11 0 R", img),
		streamBody("", "1000 0 d0 "+img),
		"<< /XObject << /LithoIm1 12 0 R >> >>",
		streamBody("/Type /XObject /Subtype /Form /BBox [0 0 1 1]", ""),
	)
}

// movedImage returns the image XObject the resources of the object owner
// name, read through the allocator.
func movedImage(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, owner int, name ast.PdfName) ast.PdfStream {
	t.Helper()
	ctx := context.Background()
	ref := ast.PdfIndirectRef{ObjectNum: owner}
	dict := liveDictValue(ctx, alloc, doc, ref)
	if s, ok := liveStream(ctx, alloc, doc, ref); ok {
		dict = s.Dict
	}
	res := liveDictValue(ctx, alloc, doc, dict["Resources"])
	xobj := liveDictValue(ctx, alloc, doc, res["XObject"])
	imgRef, isRef := xobj[name].(ast.PdfIndirectRef)
	img, ok := liveStream(ctx, alloc, doc, imgRef)
	if !isRef || !ok {
		t.Fatalf("object %d names no image %s: %v", owner, name, res)
	}
	return img
}

// TestFixInlineImageFilters_MovesCodecImages pins that an inline image that
// applies JPXDecode or JBIG2Decode, which ISO 19005-2/3, 6.1.10 and ISO
// 19005-4, 6.1.9 forbid, moves to an image XObject in every stream compliance
// judges it in, carrying its data byte for byte under its filters spelled in
// full, so it draws the samples it drew, and that the output fails the rule
// no more.
func TestFixInlineImageFilters_MovesCodecImages(t *testing.T) {
	t.Parallel()

	jbig2 := codecJBIG2(t)
	jpx, err := hex.DecodeString(codecJPX)
	if err != nil {
		t.Fatal(err)
	}
	jbig2Hex := []byte(strings.ToUpper(hex.EncodeToString(jbig2)) + ">")
	for _, tc := range []struct {
		name    string
		entries string
		data    []byte
		filter  ast.PdfObject
		want    ast.PdfDict
	}{
		{
			"JBIG2Decode", "/CS /G /BPC 1 /F /JBIG2Decode", jbig2, ast.PdfName("JBIG2Decode"),
			ast.PdfDict{"ColorSpace": ast.PdfName("DeviceGray"), "BitsPerComponent": ast.PdfInt(1)},
		},
		{
			"full keys", "/ColorSpace /DeviceGray /BitsPerComponent 1 /Filter /JBIG2Decode", jbig2, ast.PdfName("JBIG2Decode"),
			ast.PdfDict{"ColorSpace": ast.PdfName("DeviceGray"), "BitsPerComponent": ast.PdfInt(1)},
		},
		{
			"mis-cased", "/CS /G /BPC 1 /F /jbig2decode", jbig2, ast.PdfName("JBIG2Decode"),
			ast.PdfDict{"ColorSpace": ast.PdfName("DeviceGray")},
		},
		{
			"mask in a chain", "/IM true /F [/AHx /JBIG2Decode]", jbig2Hex,
			ast.PdfArray{ast.PdfName("ASCIIHexDecode"), ast.PdfName("JBIG2Decode")},
			ast.PdfDict{"ImageMask": ast.PdfBool(true)},
		},
		{
			"JPXDecode", "/CS /RGB /BPC 8 /F /JPXDecode", jpx, ast.PdfName("JPXDecode"),
			ast.PdfDict{"ColorSpace": ast.PdfName("DeviceRGB")},
		},
		{"JPXDecode reading its colour", "/F /JPXDecode", jpx, ast.PdfName("JPXDecode"), ast.PdfDict{}},
	} {
		for _, rule := range inlineFilterRules[1:] {
			t.Run(tc.name+" "+string(rule.profile), func(t *testing.T) {
				t.Parallel()
				ctx := context.Background()
				image := "BI /W 8 /H 8 " + tc.entries + " ID " + string(tc.data) + "\nEI"
				doc, alloc := newTestDoc(t, codecPDF(image, "/Resources << >>"))
				errs, err := compliance.ValidatePDFA(ctx, doc, rule.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				vs := failures(toViolations(errs), rule.rule)
				if len(vs) != len(codecStreams) {
					t.Fatalf("control: the source fails %s t%d %d times, want %d", rule.rule.Clause, rule.rule.TestNumber, len(vs), len(codecStreams))
				}

				fixes, err := fixInlineImageFilters(ctx, alloc, doc, Options{}, vs, string(rule.profile))
				if err != nil {
					t.Fatalf("fixInlineImageFilters: %v", err)
				}
				if len(fixes) != 1 || !strings.Contains(fixes[0], "moved 5 inline image") {
					t.Errorf("fixes = %q, want the 5 inline images reported moved", fixes)
				}
				for _, s := range codecStreams {
					// The annotation appearance's resources already name a
					// LithoIm1, so its image takes the next name.
					name := ast.PdfName("LithoIm1")
					if s.objNum == 9 {
						name = "LithoIm2"
					}
					body := pendingStreamBytes(t, alloc, s.objNum)
					if bytes.Contains(body, []byte("BI")) || !bytes.Contains(body, []byte("/"+string(name)+" Do")) {
						t.Errorf("%s: %q, want the image painted by Do of %s", s.name, body, name)
					}
					img := movedImage(t, alloc, doc, s.owner, name)
					got, err := img.Bytes(ctx)
					if err != nil || !bytes.Equal(got, tc.data) {
						t.Errorf("%s: the image carries %q (%v), want the inline data", s.name, got, err)
					}
					if !reflect.DeepEqual(img.Dict["Filter"], tc.filter) {
						t.Errorf("%s: /Filter %v, want %v", s.name, img.Dict["Filter"], tc.filter)
					}
					for k, v := range tc.want {
						if !reflect.DeepEqual(img.Dict[k], v) {
							t.Errorf("%s: /%s %v, want %v", s.name, k, img.Dict[k], v)
						}
					}
					if img.Dict["Width"] != ast.PdfInt(8) || img.Dict["Height"] != ast.PdfInt(8) || img.Dict["Subtype"] != ast.PdfName("Image") {
						t.Errorf("%s: dictionary %v, want an 8x8 image", s.name, img.Dict)
					}
				}

				after, _ := revalidatePDFA(t, doc, alloc, rule.profile)
				for _, v := range failures(after, rule.rule) {
					t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
				}
			})
		}
	}
}

// TestFixInlineImageFilters_LeavesWhatItCannotMove pins that an image whose
// move cannot be proven stays inline, its finding a residual, and leaves no
// image object behind: a form XObject with no /Resources resolves its names in
// the page's (ISO 32000-1, 7.8.3), a /CS gives the image XObject the colour
// space the stream's own resources name and no other, LZWDecode before the
// codec would carry on into a stream that forbids it too, and data that does
// not decode proves nothing.
func TestFixInlineImageFilters_LeavesWhatItCannotMove(t *testing.T) {
	t.Parallel()

	jbig2 := string(codecJBIG2(t))
	all := []int{4, 5, 7, 9, 10}
	for _, tc := range []struct {
		name          string
		image, formRs string
		stays         []int
		pageCS        ast.PdfObject
	}{
		{"form without resources", "BI /W 8 /H 8 /CS /G /BPC 1 /F /JBIG2Decode ID " + jbig2 + "\nEI", "", []int{5}, ast.PdfName("DeviceGray")},
		{
			"colour space only the page names", "BI /W 8 /H 8 /CS /CS0 /BPC 1 /F /JBIG2Decode ID " + jbig2 + "\nEI", "/Resources << >>",
			[]int{5, 7, 9, 10},
			ast.PdfArray{ast.PdfName("Indexed"), ast.PdfName("DeviceGray"), ast.PdfInt(1), ast.PdfString("\x00\xff")},
		},
		{"unknown colour space", "BI /W 8 /H 8 /CS /Nope /BPC 1 /F /JBIG2Decode ID " + jbig2 + "\nEI", "/Resources << >>", all, nil},
		{"LZW before the codec", "BI /W 8 /H 8 /CS /G /BPC 1 /F [/LZW /JBIG2Decode] ID " + jbig2 + "\nEI", "/Resources << >>", all, nil},
		{"undecodable data", "BI /W 8 /H 8 /CS /G /BPC 1 /F /JBIG2Decode ID xyz\nEI", "/Resources << >>", all, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, alloc := newTestDoc(t, codecPDF(tc.image, tc.formRs))
			rule := compliance.Rule_19005_2_6_1_10_t01
			errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			vs := failures(toViolations(errs), rule)
			if _, err := fixInlineImageFilters(ctx, alloc, doc, Options{}, vs, string(models.PDFA2b)); err != nil {
				t.Fatalf("fixInlineImageFilters: %v", err)
			}
			images := 0
			for _, obj := range alloc.Pending() {
				if s, ok := obj.(ast.PdfStream); ok && s.Dict["Subtype"] == ast.PdfName("Image") {
					images++
				}
			}
			if want := len(codecStreams) - len(tc.stays); images != want {
				t.Errorf("the allocator holds %d images, want %d", images, want)
			}
			for _, objNum := range tc.stays {
				if _, ok := alloc.Pending()[objNum]; ok {
					t.Errorf("object %d was rewritten, want it left as it was", objNum)
				}
			}
			if tc.pageCS != nil {
				if cs := movedImage(t, alloc, doc, 3, "LithoIm1").Dict["ColorSpace"]; !reflect.DeepEqual(cs, tc.pageCS) {
					t.Errorf("the page's image has /ColorSpace %v, want %v", cs, tc.pageCS)
				}
			}
			after, _ := revalidatePDFA(t, doc, alloc, models.PDFA2b)
			if got := len(failures(after, rule)); got != len(tc.stays) {
				t.Errorf("the output fails 6.1.10 t1 %d times, want %d", got, len(tc.stays))
			}
		})
	}
}

// TestMoveInlineImage_KeepsPart1Images pins that part 1, whose ISO 19005-1,
// 6.1.10 forbids LZWDecode alone, moves no JPXDecode or JBIG2Decode image.
func TestMoveInlineImage_KeepsPart1Images(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := []byte("q BI /W 8 /H 8 /CS /G /BPC 1 /F /JBIG2Decode ID " + string(codecJBIG2(t)) + "\nEI Q")
	res := newResourceEdit(writer.NewObjAllocator(0), nil, resourceOwner{
		load:  func() ast.PdfObject { return ast.PdfDict{} },
		store: func(ast.PdfDict) bool { return true },
	})
	out, n, moved := reencodeInlineImages(ctx, src, 1, filter.NewDecoder(nil, limits.Default()), res)
	if n != 0 || moved != 0 || !bytes.Equal(out, src) {
		t.Errorf("part 1 rewrote %d and moved %d: %q", n, moved, out)
	}
	ops, err := content.ParseStreamData(ctx, src)
	if err != nil || len(ops) != 3 {
		t.Fatalf("control: %d operations, %v", len(ops), err)
	}
}
