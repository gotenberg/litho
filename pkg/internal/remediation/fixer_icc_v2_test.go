package remediation

import (
	"compress/zlib"
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/models"
)

// iccTestProfile assembles an ICC profile of the given header fields and
// tags, in the order given.
func iccTestProfile(version uint32, class, space string, tags ...[]byte) []byte {
	header := make([]byte, 128)
	binary.BigEndian.PutUint32(header[8:], version)
	copy(header[12:], class)
	copy(header[16:], space)
	copy(header[20:], "XYZ ")
	copy(header[36:], "acsp")
	copy(header[68:], []byte{0, 0, 0xF6, 0xD6, 0, 1, 0, 0, 0, 0, 0xD3, 0x2D})
	table := binary.BigEndian.AppendUint32(nil, uint32(len(tags)))
	var body []byte
	for _, tag := range tags {
		table = append(table, tag[:4]...)
		table = binary.BigEndian.AppendUint32(table, uint32(128+4+12*len(tags)+len(body)))
		table = binary.BigEndian.AppendUint32(table, uint32(len(tag)-4))
		body = append(body, tag[4:]...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	out := append(append(header, table...), body...)
	binary.BigEndian.PutUint32(out, uint32(len(out)))
	return out
}

// s15Test encodes s15Fixed16Numbers.
func s15Test(vs ...float64) []byte {
	var b []byte
	for _, v := range vs {
		b = binary.BigEndian.AppendUint32(b, uint32(int32(math.Round(v*65536))))
	}
	return b
}

// sGrayV4Profile is a version 4.2 grey display profile of the shape pdfjs
// issue19532.pdf carries in its output intent: an English mluc description,
// a D50 white with a Bradford adaptation from D65, and the sRGB tone curve as
// a parametric curve. Each tag is its signature followed by its data.
func sGrayV4Profile() []byte {
	mluc := []byte("descmluc\x00\x00\x00\x00\x00\x00\x00\x01\x00\x00\x00\x0cenUS\x00\x00\x00\x08\x00\x00\x00\x1c\x00s\x00G\x00r\x00y")
	wtpt := append([]byte("wtptXYZ \x00\x00\x00\x00"), s15Test(0.9642, 1, 0.8249)...)
	chad := append([]byte("chadsf32\x00\x00\x00\x00"), s15Test(1.047882, 0.022918, -0.050217, 0.029586, 0.990478, -0.017075, -0.009247, 0.015075, 0.751678)...)
	trc := append([]byte("kTRCpara\x00\x00\x00\x00\x00\x03\x00\x00"), s15Test(2.4, 1/1.055, 0.055/1.055, 1/12.92, 0.04045)...)
	return iccTestProfile(0x04200000, "mntr", "GRAY", mluc, wtpt, chad, trc)
}

// grayIntentPDF is a one-page document whose GTS_PDFA1 output intent (object
// 4) carries profile, Flate-encoded, as object 5 with /N n.
func grayIntentPDF(t *testing.T, profile []byte, n int) []byte {
	t.Helper()
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sGry) /DestOutputProfile 5 0 R >>",
		iccObj(deflateAt(t, profile, zlib.BestCompression), n),
	})
}

// outputProfileViolations returns the ISO 19005-1, 6.2.2 test 1 violations
// of data at PDF/A-1b.
func outputProfileViolations(t *testing.T, doc *document.Document) []compliance.Violation {
	t.Helper()
	errs, err := compliance.ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	var out []compliance.Violation
	for _, e := range errs {
		if v, ok := e.(compliance.Violation); ok && v.Rule.Clause == "6.2.2" && v.Rule.TestNumber == 1 {
			out = append(out, v)
		}
	}
	return out
}

// TestRemediatePDFA1_ReencodesV4OutputProfileAsV2 pins that a PDF/A-1
// remediation re-encodes an output intent's version 4 grey profile as version
// 2 in place, with the colours it converts unchanged, and that the output
// passes ISO 19005-1, 6.2.2 t1. The fixer accepted major versions 2 and 4 in
// every part, so the violation survived every remediation.
func TestRemediatePDFA1_ReencodesV4OutputProfileAsV2(t *testing.T) {
	t.Parallel()

	src := sGrayV4Profile()
	data := grayIntentPDF(t, src, 1)
	doc, _ := newTestDoc(t, data)
	if len(outputProfileViolations(t, doc)) == 0 {
		t.Fatal("control: the version 4 profile does not fail 6.2.2 t1 at PDF/A-1b")
	}

	out := remediateAndReopen(t, data, models.PDFA1b, Options{})
	if vs := outputProfileViolations(t, out); len(vs) != 0 {
		t.Fatalf("after remediation: %v", vs)
	}

	ctx := context.Background()
	catRef, _ := out.CatalogRef(ctx)
	cat, err := document.ResolveDict(ctx, out, catRef)
	if err != nil {
		t.Fatal(err)
	}
	intents, err := document.ResolveArray(ctx, out, cat["OutputIntents"])
	if err != nil || len(intents) != 1 {
		t.Fatalf("output intents %v, %v; want the one intent", intents, err)
	}
	intent, err := document.ResolveDict(ctx, out, intents[0])
	if err != nil {
		t.Fatal(err)
	}
	stream, err := document.ResolveStream(ctx, out, intent["DestOutputProfile"])
	if err != nil {
		t.Fatalf("the intent names no profile stream: %v", err)
	}
	got, err := out.DecodeStream(ctx, stream)
	if err != nil {
		t.Fatalf("decoding the profile: %v", err)
	}
	if got[8] != 2 || string(got[16:20]) != "GRAY" {
		t.Fatalf("profile version %d, colour space %q; want a version 2 grey profile", got[8], got[16:20])
	}
	if n, _ := stream.Dict["N"].(ast.PdfInt); n != 1 {
		t.Errorf("/N = %v, want 1", stream.Dict["N"])
	}
	before, err := icc.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	after, err := icc.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	tb, err := before.NewTransform(icc.RelativeColorimetric)
	if err != nil {
		t.Fatal(err)
	}
	ta, err := after.NewTransform(icc.RelativeColorimetric)
	if err != nil {
		t.Fatal(err)
	}
	for v := range 256 {
		r1, g1, b1 := tb.Convert([]byte{byte(v)})
		r2, g2, b2 := ta.Convert([]byte{byte(v)})
		if r1 != r2 || g1 != g2 || b1 != b2 {
			t.Fatalf("grey %d: %d,%d,%d before, %d,%d,%d after", v, r1, g1, b1, r2, g2, b2)
		}
	}
}

// TestFixCorruptICC_KeepsV4ProfileAfterPDFA1 pins that the re-encoding is a
// PDF/A-1 repair alone: ISO 19005-2, 6.2.3 admits a version 4 profile.
func TestFixCorruptICC_KeepsV4ProfileAfterPDFA1(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, grayIntentPDF(t, sGrayV4Profile(), 1))
	fixes, err := fixCorruptICC(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixCorruptICC: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("PDF/A-2b: fixes %v, pending %v; want the version 4 profile kept", fixes, alloc.Pending())
	}
}

// TestFixCorruptICC_KeepsUnconvertibleV4Profile pins that a version 4
// profile whose colour lives in a table version 2 has no type for stays as it
// is at PDF/A-1: the intent names its printing condition, and neither a
// resampled table nor a stock profile is that characterization.
func TestFixCorruptICC_KeepsUnconvertibleV4Profile(t *testing.T) {
	t.Parallel()

	atob := []byte("A2B0mAB \x00\x00\x00\x00\x04\x03\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	profile := iccTestProfile(0x04200000, "prtr", "CMYK", atob)
	doc, alloc := newTestDoc(t, grayIntentPDF(t, profile, 4))
	fixes, err := fixCorruptICC(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixCorruptICC: %v", err)
	}
	if len(fixes) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("fixes %v, pending %v; want the profile kept", fixes, alloc.Pending())
	}
}

// TestFixCorruptICC_ReplacesGreyProfileWithGrey pins that replacing an
// unusable grey destination profile keeps the grey colour space, with the
// generated version 2 grey profile. replaceICCProfile wrote the sRGB profile
// with /N 3, which turned a grey intent into an RGB one.
func TestFixCorruptICC_ReplacesGreyProfileWithGrey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	v5 := sGrayV4Profile()
	v5[8] = 5
	doc, alloc := newTestDoc(t, grayIntentPDF(t, v5, 1))
	fixes, err := fixCorruptICC(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixCorruptICC: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("control: the version 5 profile was not replaced")
	}
	oi, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatalf("output intent is %T, want a pending dict", alloc.Pending()[4])
	}
	ref, _ := oi["DestOutputProfile"].(ast.PdfIndirectRef)
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("replacement is %T, want a stream", alloc.Pending()[ref.ObjectNum])
	}
	data, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := stream.Dict["N"].(ast.PdfInt); n != 1 || string(data[16:20]) != "GRAY" || data[8] != 2 {
		t.Errorf("replacement /N %v, colour space %q, version %d; want a version 2 grey profile, /N 1", stream.Dict["N"], data[16:20], data[8])
	}
}
