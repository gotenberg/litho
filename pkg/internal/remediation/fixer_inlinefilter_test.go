package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// inlineFilterSamples are the samples of the 4x2 grey inline images the tests
// draw.
const inlineFilterSamples = "\x00\x40\x80\xff\xff\x80\x40\x00"

// lzwInline is an inline image whose samples, [inlineFilterSamples], are
// compressed with LZW and whose dictionary names the filter with filterEntry.
func lzwInline(t *testing.T, filterEntry string) string {
	t.Helper()
	return "BI /W 4 /H 2 /BPC 8 /CS /G " + filterEntry + " ID " + string(lzwBody(t, inlineFilterSamples)) + " EI"
}

// inlineFilterStreams are the objects of [inlineFilterPDF] that hold an LZW
// inline image.
var inlineFilterStreams = []struct {
	name   string
	objNum int
}{
	{"page content", 4},
	{"form XObject", 5},
	{"tiling pattern cell", 7},
	{"annotation appearance", 9},
	{"Type 3 glyph", 10},
}

// inlineFilterPDF builds a one-page document drawing image, an inline image,
// in every kind of stream compliance judges the inline image rules in: the
// page's content, the form X0 it draws, the glyph a of the Type 3 font T3 it
// shows, the cell of the tiling pattern P0 it fills with, and the normal
// appearance of a Square annotation.
func inlineFilterPDF(image string) []byte {
	img := "q 10 0 0 10 0 0 cm " + image + " Q"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Annots [8 0 R]"+
			" /Resources << /XObject << /X0 5 0 R >> /Font << /T3 6 0 R >> /Pattern << /P0 7 0 R >> >> >>",
		streamBody("", img+" /X0 Do q BT /T3 12 Tf 10 10 Td (a) Tj ET Q q /Pattern cs /P0 scn 0 0 50 50 re f Q"),
		streamBody("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", img),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]"+
			" /CharProcs << /a 10 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >>"+
			" /FirstChar 97 /LastChar 97 /Widths [1000] /Resources << >> >>",
		streamBody("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10]"+
			" /XStep 10 /YStep 10 /Resources << >>", img),
		"<< /Type /Annot /Subtype /Square /Rect [60 10 80 30] /F 4 /AP << /N 9 0 R >> >>",
		streamBody("/BBox [0 0 20 20]", img),
		streamBody("", "1000 0 d0 "+img),
	)
}

// inlineFilterRules are the rules an inline image with a forbidden filter
// breaks, one for each PDF/A part.
var inlineFilterRules = []struct {
	profile models.PDFAProfile
	rule    compliance.Rule
}{
	{models.PDFA1b, compliance.Rule_19005_1_6_1_10_t02},
	{models.PDFA2b, compliance.Rule_19005_2_6_1_10_t01},
	{models.PDFA3b, compliance.Rule_19005_3_6_1_10_t01},
	{models.PDFA4, compliance.Rule_19005_4_6_1_9_t01},
}

// inlineImageSamples returns the decoded data of every inline image of data,
// a content stream, in stream order, read as the repair reads it.
func inlineImageSamples(t *testing.T, data []byte) [][]byte {
	t.Helper()
	ctx := context.Background()
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.ForRepair())
	if err != nil {
		t.Fatalf("parsing %q: %v", data, err)
	}
	dec := filter.NewDecoder(nil, limits.Default())
	var out [][]byte
	for i, op := range ops {
		if op.Operator != "BI" {
			continue
		}
		dict := op.Operands[0].(ast.PdfDict)
		bi, ok := inlineImageStart(data[ranges[i].Start:ranges[i].End])
		if !ok {
			t.Fatalf("no BI in %q", data[ranges[i].Start:ranges[i].End])
		}
		end, ok := inlineImageEnd(data, ranges[i])
		if !ok {
			t.Fatalf("no EI in %q", data[ranges[i].Start:ranges[i].End])
		}
		image := data[ranges[i].Start+bi : end]
		start, ok := inlineImageDataStart(ctx, image, dict)
		if !ok {
			t.Fatalf("no data in %q", image)
		}
		names := inlineImageFilterNames(dict)
		_, parmsKey, _ := inlineFilterKeys(dict)
		parms, ok := inlineDecodeParms(dict[parmsKey], len(names))
		if !ok {
			t.Fatalf("parameters of %v", dict)
		}
		samples, ok := decodeInlineData(ctx, dec, image[start:len(image)-len("EI")], canonicalNames(names), parms)
		if !ok {
			t.Fatalf("the image %q does not decode", image)
		}
		out = append(out, samples)
	}
	return out
}

// TestFixInlineImageFilters_ReencodesLZW pins that the inline-filter fixer
// answers the finding for an LZW inline image in every stream compliance
// judges it in, for every part and every spelling, and that each image draws
// the samples it drew.
//
// ISO 19005-1, 6.1.10, ISO 19005-2/3, 6.1.10 and ISO 19005-4, 6.1.9 forbid
// LZWDecode in an inline image's /F, and no fixer rewrote one: the LZW fixer
// re-encodes stream objects, whose dictionary and data are apart, while an
// inline image's sit together inside a content stream.
func TestFixInlineImageFilters_ReencodesLZW(t *testing.T) {
	t.Parallel()

	for _, spelling := range []string{"/F /LZW", "/F /LZWDecode", "/Filter /LZWDecode", "/F [/LZW]"} {
		for _, tc := range inlineFilterRules {
			t.Run(spelling+" "+string(tc.profile), func(t *testing.T) {
				t.Parallel()
				ctx := context.Background()
				src := lzwInline(t, spelling)
				doc, alloc := newTestDoc(t, inlineFilterPDF(src))
				errs, err := compliance.ValidatePDFA(ctx, doc, tc.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				vs := failures(toViolations(errs), tc.rule)
				if len(vs) != len(inlineFilterStreams) {
					t.Fatalf("control: the source fails %s t%d %d times, want %d", tc.rule.Clause, tc.rule.TestNumber, len(vs), len(inlineFilterStreams))
				}

				fixes, err := fixInlineImageFilters(ctx, alloc, doc, Options{}, vs, string(tc.profile))
				if err != nil {
					t.Fatalf("fixInlineImageFilters: %v", err)
				}
				if len(fixes) != 1 || !strings.Contains(fixes[0], "5 inline image") {
					t.Errorf("fixes = %q, want the 5 inline images reported", fixes)
				}
				for _, s := range inlineFilterStreams {
					body := pendingStreamBytes(t, alloc, s.objNum)
					samples := inlineImageSamples(t, body)
					if len(samples) != 1 || string(samples[0]) != inlineFilterSamples {
						t.Errorf("%s: the image draws %q, want %q", s.name, samples, inlineFilterSamples)
					}
					if bytes.Contains(body, []byte("LZW")) {
						t.Errorf("%s: still names LZW: %q", s.name, body)
					}
				}

				after, _ := revalidatePDFA(t, doc, alloc, tc.profile)
				for _, v := range failures(after, tc.rule) {
					t.Errorf("the output still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
				}
			})
		}
	}
}

// TestReencodeInlineImages_KeepsTheFiltersAfterLZW pins that a chain decodes
// only through its last forbidden filter: the filters before it go with it,
// the ones after it stay with their parameters, and the image draws the
// samples it drew.
func TestReencodeInlineImages_KeepsTheFiltersAfterLZW(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// [/AHx /LZW /RL]: hex of the LZW code of the RunLength data of the
	// samples, a literal run of 8 bytes and the EOD.
	rl := "\x07" + inlineFilterSamples + "\x80"
	data := hex.EncodeToString(lzwBody(t, rl)) + ">"
	src := "q BI /W 4 /H 2 /BPC 8 /CS /G /F [/AHx /LZW /RL] /DP [null << /EarlyChange 1 >> null] ID " + data + " EI Q"
	want := inlineImageSamples(t, []byte(src))
	if len(want) != 1 || string(want[0]) != inlineFilterSamples {
		t.Fatalf("control: the source draws %q", want)
	}

	out, n := reencodeInlineImages(ctx, []byte(src), 2, filter.NewDecoder(nil, limits.Default()))
	if n != 1 {
		t.Fatalf("rewrote %d images, want 1: %q", n, out)
	}
	ops, err := content.ParseStreamData(ctx, out)
	if err != nil || len(ops) != 3 {
		t.Fatalf("validation reads %v (%v) in %q", ops, err, out)
	}
	dict := ops[1].Operands[0].(ast.PdfDict)
	if got := fmt.Sprint(dict["F"]); got != "[/Fl /RL]" && got != fmt.Sprint(ast.PdfArray{ast.PdfName("Fl"), ast.PdfName("RL")}) {
		t.Errorf("/F = %v, want [/Fl /RL]", dict["F"])
	}
	if _, ok := dict["DP"]; ok {
		t.Errorf("/DP = %v, want none: no kept filter takes parameters", dict["DP"])
	}
	if got := inlineImageSamples(t, out); len(got) != 1 || !bytes.Equal(got[0], want[0]) {
		t.Errorf("the image draws %q, want %q", got, want)
	}
}

// TestReencodeInlineImages_PartDecidesTheForbiddenNames pins that a name
// misspelled by case is repaired where the part forbids it: ISO 19005-2,
// 6.1.10 forbids a value outside ISO 32000-1, Table 6, which /Lzw is (names
// are case-sensitive, 7.3.5), while ISO 19005-1, 6.1.10 names LZWDecode alone.
func TestReencodeInlineImages_PartDecidesTheForbiddenNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dec := filter.NewDecoder(nil, limits.Default())

	src := []byte("q " + lzwInline(t, "/F /Lzw") + " Q")
	if out, n := reencodeInlineImages(ctx, src, 1, dec); n != 0 || !bytes.Equal(out, src) {
		t.Errorf("part 1 rewrote %d images: %q", n, out)
	}
	out, n := reencodeInlineImages(ctx, src, 2, dec)
	if n != 1 {
		t.Fatalf("part 2 rewrote %d images, want 1", n)
	}
	if got := inlineImageSamples(t, out); len(got) != 1 || string(got[0]) != inlineFilterSamples {
		t.Errorf("the image draws %q", got)
	}

	// A name only misspelled by case is renamed, its data kept.
	ahx := []byte("q BI /W 4 /H 2 /BPC 8 /CS /G /F /ahx ID " + hex.EncodeToString([]byte(inlineFilterSamples)) + "> EI Q")
	out, n = reencodeInlineImages(ctx, ahx, 4, dec)
	if n != 1 || !bytes.Contains(out, []byte("/F /AHx /H 2 /W 4 ID "+hex.EncodeToString([]byte(inlineFilterSamples))+">")) {
		t.Errorf("rewrote %d images as %q, want /ahx renamed /AHx", n, out)
	}
}

// TestReencodeInlineImages_LeavesWhatItCannotProve pins that an image whose
// rewrite cannot be shown to draw the same pixels keeps its bytes: an unknown
// filter, damaged data, an image codec before the LZW, and a dictionary that
// writes the filter key both ways.
func TestReencodeInlineImages_LeavesWhatItCannotProve(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dec := filter.NewDecoder(nil, limits.Default())
	lzwData := string(lzwBody(t, inlineFilterSamples))

	for name, src := range map[string]string{
		"unknown filter":   "BI /W 4 /H 2 /BPC 8 /CS /G /F [/Foo /LZW] ID " + lzwData + " EI",
		"damaged data":     "BI /W 4 /H 2 /BPC 8 /CS /G /F [/AHx /LZW] ID zz> EI",
		"codec before LZW": "BI /W 4 /H 2 /BPC 8 /CS /G /F [/DCT /LZW] ID " + lzwData + " EI",
		"both keys":        "BI /W 4 /H 2 /BPC 8 /CS /G /F /LZW /Filter /LZW ID " + lzwData + " EI",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := []byte("q " + src + " Q")
			if out, n := reencodeInlineImages(ctx, in, 2, dec); n != 0 || !bytes.Equal(out, in) {
				t.Errorf("rewrote %d images: %q", n, out)
			}
		})
	}
}

// TestReencodeInlineImages_StatesTheNewLength pins that a dictionary stating
// its data length, /L (ISO 32000-2, 8.9.7, Table 91), states the length of
// the data written, which veraPDF reads before it looks for the EI.
func TestReencodeInlineImages_StatesTheNewLength(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lzwData := lzwBody(t, inlineFilterSamples)
	src := []byte(fmt.Sprintf("q BI /W 4 /H 2 /BPC 8 /CS /G /F /LZW /L %d ID %s EI Q", len(lzwData), lzwData))

	out, n := reencodeInlineImages(ctx, src, 2, filter.NewDecoder(nil, limits.Default()))
	if n != 1 {
		t.Fatalf("rewrote %d images, want 1", n)
	}
	ops, err := content.ParseStreamData(ctx, out)
	if err != nil || len(ops) != 3 {
		t.Fatalf("validation reads %v (%v)", ops, err)
	}
	length, ok := ops[1].Operands[0].(ast.PdfDict)["L"].(ast.PdfInt)
	if !ok {
		t.Fatalf("no /L in %v", ops[1].Operands[0])
	}
	if !bytes.Contains(out, []byte(fmt.Sprintf("ID %s\nEI", out[bytes.Index(out, []byte(" ID "))+4:][:length]))) {
		t.Errorf("/L %d does not span the data of %q", length, out)
	}
}

// TestEncodeInlineData_WrapsDataHoldingEI pins that FlateDecode data that
// holds "EI", which a reader scanning for the end of the data would stop at,
// is written in ASCIIHexDecode, whose alphabet holds no I (ISO 32000-1,
// 7.4.2).
func TestEncodeInlineData_WrapsDataHoldingEI(t *testing.T) {
	t.Parallel()

	// Find samples whose deflate data holds "EI": the encoder is
	// deterministic, so the search always ends at the same one.
	rng := rand.New(rand.NewPCG(1, 2))
	var samples []byte
	for i := 0; i < 1<<20 && samples == nil; i++ {
		candidate := make([]byte, 16)
		for j := range candidate {
			candidate[j] = byte(rng.Uint32())
		}
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		if _, err := w.Write(candidate); err != nil {
			t.Fatalf("deflate: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("deflate: %v", err)
		}
		if bytes.Contains(buf.Bytes(), []byte("EI")) {
			samples = candidate
		}
	}
	if samples == nil {
		t.Fatal("no deflate data holding EI found")
	}

	names, data, ok := encodeInlineData(samples)
	if !ok || fmt.Sprint(names) != fmt.Sprint([]ast.PdfName{"AHx", "Fl"}) {
		t.Fatalf("encoded with %v (%v), want [AHx Fl]", names, ok)
	}
	if bytes.Contains(data, []byte("I")) {
		t.Errorf("the data holds an I: %q", data)
	}
	back, ok := decodeInlineChain(context.Background(), filter.NewDecoder(nil, limits.Default()), data, names, make([]ast.PdfObject, 2))
	if !ok || !bytes.Equal(back, samples) {
		t.Errorf("decodes to %q, want %q", back, samples)
	}
}

// TestRemediatePDFA_ClearsTheInlineImageFilterRule pins the end-to-end
// result: a PDF/A remediation of a document whose inline images are LZW writes
// an output that fails no inline image filter rule, at every part.
func TestRemediatePDFA_ClearsTheInlineImageFilterRule(t *testing.T) {
	t.Parallel()

	for _, tc := range inlineFilterRules {
		t.Run(string(tc.profile), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, alloc := newTestDoc(t, inlineFilterPDF(lzwInline(t, "/F /LZW")))
			report, err := RemediatePDFA(ctx, doc, alloc, tc.profile, Options{})
			if err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			for _, v := range failures(toViolations(report.Residual), tc.rule) {
				t.Errorf("residual %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
			}
			var out bytes.Buffer
			if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
				t.Fatalf("FullRewrite: %v", err)
			}
			reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			errs, err := compliance.ValidatePDFA(ctx, reread, tc.profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			for _, v := range failures(toViolations(errs), tc.rule) {
				t.Errorf("the output fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
			}
		})
	}
}
