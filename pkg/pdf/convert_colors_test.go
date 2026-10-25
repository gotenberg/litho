package pdf

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// recolorPDF draws one page in DeviceRGB, with the output intents intents
// (catalog entries, "" for none), the objects from 5 on being extra.
func recolorPDF(intents string, extra ...string) []byte {
	return colorFixturePDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R " + intents + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R >>",
		colorStream("", "1 0 0 rg 0 0 50 50 re f"),
	}, extra...))
}

// fakeRecolor is an engine that rewrites each page's content stream to paint
// in DeviceCMYK, and records the request it was given.
type fakeRecolor struct {
	req   recolorRequest
	calls int
	err   error
}

func (f *fakeRecolor) engine(ctx context.Context, alloc *writer.ObjAllocator, _ *document.Document, pages []*document.Page, req recolorRequest) (recolorResult, error) {
	f.calls++
	f.req = req
	if f.err != nil {
		return recolorResult{}, f.err
	}
	if err := ctx.Err(); err != nil {
		return recolorResult{}, err
	}
	data := []byte("0 1 1 0 k 0 0 50 50 re f")
	for _, p := range pages {
		ref, ok := p.Dict["Contents"].(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		alloc.AllocAt(ref.ObjectNum, ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(data))}, data))
	}
	return recolorResult{operators: len(pages), keptSpots: []string{"Gold", "Blue", "Gold"}}, nil
}

// openColorPDF opens data, closing it when the test ends.
func openColorPDF(t *testing.T, data []byte, opts ...OpenOption) *Reader {
	t.Helper()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// convertAndWrite runs ConvertColors through f on data and writes the
// output.
func convertAndWrite(t *testing.T, r *Reader, f *fakeRecolor, target ColorTarget, opts ...ColorOption) (*ColorReport, []byte) {
	t.Helper()
	ctx := context.Background()
	w := NewWriter(r)
	t.Cleanup(func() { _ = w.Close() })
	w.recolor = f.engine
	report, err := w.ConvertColors(ctx, target, opts...)
	if err != nil {
		t.Fatalf("ConvertColors: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return report, out.Bytes()
}

// outputIntents returns the decoded output intent dictionaries of data's
// catalog, each with its subtype and decoded profile.
func outputIntents(t *testing.T, data []byte) []struct {
	subtype, condition string
	profile            []byte
	n                  int
} {
	t.Helper()
	ctx := context.Background()
	r := openColorPDF(t, data)
	ref, ok := mutations.CatalogRef(ctx, r.doc)
	if !ok {
		t.Fatal("no catalog")
	}
	catalog, err := document.ResolveDict(ctx, r.doc, ref)
	if err != nil {
		t.Fatal(err)
	}
	arr, err := document.ResolveArray(ctx, r.doc, catalog["OutputIntents"])
	if err != nil {
		return nil
	}
	var out []struct {
		subtype, condition string
		profile            []byte
		n                  int
	}
	for _, e := range arr {
		d, err := document.ResolveDict(ctx, r.doc, e)
		if err != nil {
			t.Fatal(err)
		}
		s, _ := document.ResolveName(ctx, r.doc, d["S"])
		cond, _ := document.ResolveObject(ctx, r.doc, d["OutputConditionIdentifier"])
		condStr, _ := cond.(ast.PdfString)
		stream, err := document.ResolveStream(ctx, r.doc, d["DestOutputProfile"])
		if err != nil {
			t.Fatal(err)
		}
		profile, err := r.doc.DecodeStream(ctx, stream)
		if err != nil {
			t.Fatal(err)
		}
		n, _ := document.ResolveInt(ctx, r.doc, stream.Dict["N"])
		out = append(out, struct {
			subtype, condition string
			profile            []byte
			n                  int
		}{string(s), string(condStr), profile, int(n)})
	}
	return out
}

// TestConvertColors_CMYKTargetAddsTheIntent pins the default CMYK target: the
// engine is asked for DeviceCMYK through the built-in printer profile at
// relative colorimetric, the output paints in DeviceCMYK, and a GTS_PDFA1
// intent of that profile is added.
func TestConvertColors_CMYKTargetAddsTheIntent(t *testing.T) {
	t.Parallel()
	f := &fakeRecolor{}
	report, out := convertAndWrite(t, openColorPDF(t, recolorPDF("")), f, CMYKColorTarget())

	if f.req.family != "DeviceCMYK" || f.req.components != 4 || f.req.intent != icc.RelativeColorimetric {
		t.Errorf("request = %+v, want DeviceCMYK, 4 components, relative colorimetric", f.req)
	}
	if !bytes.Equal(f.req.profile, icc.CMYKPrinterProfile()) {
		t.Error("request profile is not the built-in printer profile")
	}
	if !f.req.blackPointCompensation || f.req.convertSpots {
		t.Errorf("request options = %+v, want compensation on and spots kept", f.req)
	}
	if report.Target != "DeviceCMYK" || report.OutputIntent != "added" || report.ConvertedOperators != 1 {
		t.Errorf("report = %+v", report)
	}
	if !slices.Equal(report.KeptSpotColorants, []string{"Blue", "Gold"}) {
		t.Errorf("kept spots = %q, want sorted and unique", report.KeptSpotColorants)
	}

	intents := outputIntents(t, out)
	if len(intents) != 1 || intents[0].subtype != "GTS_PDFA1" || intents[0].n != 4 ||
		intents[0].condition != builtinCMYKCondition || !bytes.Equal(intents[0].profile, icc.CMYKPrinterProfile()) {
		t.Fatalf("intents = %+v", intents)
	}
	uses, err := openColorPDF(t, out).ExtractColorSpaces(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range uses {
		if u.Family != "DeviceCMYK" {
			t.Errorf("output uses %s", describeUse(u))
		}
	}
}

// TestConvertColors_KeepsPDFXIntent pins the wave-62 rule: a PDF/X intent
// stays as it was, the GTS_PDFA1 intent takes the target's profile.
func TestConvertColors_KeepsPDFXIntent(t *testing.T) {
	t.Parallel()
	cmyk := string(icc.CMYKPrinterProfile())
	srgb := string(icc.SRGBProfile())
	data := recolorPDF("/OutputIntents [5 0 R 6 0 R]",
		"<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (FOGRA39) /DestOutputProfile 7 0 R >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) /DestOutputProfile 8 0 R >>",
		colorStream("/N 4", cmyk),
		colorStream("/N 3", srgb),
	)
	report, out := convertAndWrite(t, openColorPDF(t, data), &fakeRecolor{}, GrayColorTarget())
	if report.OutputIntent != "replaced" || !slices.Equal(report.KeptOutputIntents, []string{"GTS_PDFX"}) {
		t.Errorf("report = %+v", report)
	}
	intents := outputIntents(t, out)
	if len(intents) != 2 {
		t.Fatalf("intents = %+v", intents)
	}
	if intents[0].subtype != "GTS_PDFX" || intents[0].condition != "FOGRA39" || intents[0].profile == nil || string(intents[0].profile) != cmyk {
		t.Errorf("PDF/X intent changed: %+v", intents[0])
	}
	if intents[1].subtype != "GTS_PDFA1" || intents[1].n != 1 || !bytes.Equal(intents[1].profile, icc.SGrayProfile()) {
		t.Errorf("PDF/A intent = %+v, want the grey profile", intents[1])
	}
}

// TestConvertColors_OutputIntentTarget pins the default target: the PDF/X
// intent's profile, in the device family of its colour space, the intents
// untouched; and the refusal of a document with none.
func TestConvertColors_OutputIntentTarget(t *testing.T) {
	t.Parallel()
	cmyk := icc.CMYKPrinterProfile()
	data := recolorPDF("/OutputIntents [5 0 R 6 0 R]",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) /DestOutputProfile 8 0 R >>",
		"<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (SWOP) /DestOutputProfile 7 0 R >>",
		colorStream("/N 4", string(cmyk)),
		colorStream("/N 3", string(icc.SRGBProfile())),
	)
	f := &fakeRecolor{}
	report, out := convertAndWrite(t, openColorPDF(t, data), f, ColorTarget{})
	if f.req.family != "DeviceCMYK" || !bytes.Equal(f.req.profile, cmyk) {
		t.Errorf("request = %s over %d bytes, want DeviceCMYK over the PDF/X profile", f.req.family, len(f.req.profile))
	}
	if report.OutputIntent != "" {
		t.Errorf("output intent = %q, want untouched", report.OutputIntent)
	}
	if got := outputIntents(t, out); len(got) != 2 || got[0].subtype != "GTS_PDFA1" || got[0].n != 3 {
		t.Errorf("intents = %+v", got)
	}

	w := NewWriter(openColorPDF(t, recolorPDF("")))
	t.Cleanup(func() { _ = w.Close() })
	w.recolor = (&fakeRecolor{}).engine
	if _, err := w.ConvertColors(context.Background(), OutputIntentColorTarget()); !errors.Is(err, ErrNoOutputIntent) {
		t.Errorf("no intent: err = %v, want ErrNoOutputIntent", err)
	}
}

// TestConvertColors_Options pins how the options reach the engine and the
// intent: WithDefaultCMYKProfile names the CMYK target's profile, and
// WithoutOutputIntent leaves the catalog alone.
func TestConvertColors_Options(t *testing.T) {
	t.Parallel()
	custom := icc.CMYKPrinterProfile()
	custom[len(custom)-1] ^= 1 // a profile other than the built-in, as bytes
	r := openColorPDF(t, recolorPDF(""), WithDefaultCMYKProfile(custom))
	f := &fakeRecolor{}
	report, out := convertAndWrite(t, r, f, CMYKColorTarget(),
		WithColorIntent(IntentPerceptual), WithBlackPointCompensation(), WithSpotConversion(), WithoutOutputIntent())
	if !bytes.Equal(f.req.profile, custom) || f.req.intent != icc.Perceptual || !f.req.blackPointCompensation || !f.req.convertSpots {
		t.Errorf("request = %+v", f.req)
	}
	if report.OutputIntent != "" || report.KeptSpotColorants != nil {
		t.Errorf("report = %+v, want no intent and no kept spots", report)
	}
	if got := outputIntents(t, out); got != nil {
		t.Errorf("intents = %+v, want none", got)
	}

	_, out = convertAndWrite(t, openColorPDF(t, recolorPDF(""), WithDefaultCMYKProfile(custom)), &fakeRecolor{}, CMYKColorTarget())
	if got := outputIntents(t, out); len(got) != 1 || got[0].condition != customCondition || !bytes.Equal(got[0].profile, custom) {
		t.Errorf("intents = %+v, want the caller's profile as a Custom condition", got)
	}
}

// TestConvertColors_BlackPointCompensationByDefault pins that the conversion
// compensates black points unless WithoutBlackPointCompensation is given, and
// that the deprecated WithBlackPointCompensation changes nothing.
func TestConvertColors_BlackPointCompensationByDefault(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		opts []ColorOption
		want bool
	}{
		{"default", nil, true},
		{"deprecated opt-in", []ColorOption{WithBlackPointCompensation()}, true},
		{"opt-out", []ColorOption{WithoutBlackPointCompensation()}, false},
		{"opt-out wins over the no-op", []ColorOption{WithoutBlackPointCompensation(), WithBlackPointCompensation()}, false},
	} {
		f := &fakeRecolor{}
		convertAndWrite(t, openColorPDF(t, recolorPDF("")), f, CMYKColorTarget(), tc.opts...)
		if f.req.blackPointCompensation != tc.want {
			t.Errorf("%s: blackPointCompensation = %t, want %t", tc.name, f.req.blackPointCompensation, tc.want)
		}
	}
}

// TestConvertColors_ICCTarget pins the ICCBased target and the refusal of a
// profile litho cannot convert to.
func TestConvertColors_ICCTarget(t *testing.T) {
	t.Parallel()
	f := &fakeRecolor{}
	report, _ := convertAndWrite(t, openColorPDF(t, recolorPDF("")), f, ICCColorTarget(icc.SRGBProfile()))
	if f.req.family != "ICCBased" || f.req.components != 3 || report.OutputIntent != "added" {
		t.Errorf("request %s/%d, report %+v", f.req.family, f.req.components, report)
	}

	for name, target := range map[string]ColorTarget{
		"not a profile": ICCColorTarget([]byte("not an ICC profile")),
		"empty":         ICCColorTarget(nil),
	} {
		w := NewWriter(openColorPDF(t, recolorPDF("")))
		t.Cleanup(func() { _ = w.Close() })
		w.recolor = f.engine
		if _, err := w.ConvertColors(context.Background(), target); !errors.Is(err, ErrInvalidColorTarget) {
			t.Errorf("%s: err = %v, want ErrInvalidColorTarget", name, err)
		}
	}
	w := NewWriter(openColorPDF(t, recolorPDF("")))
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.ConvertColors(context.Background(), RGBColorTarget(), WithColorIntent(RenderingIntent(9))); !errors.Is(err, ErrInvalidColorTarget) {
		t.Errorf("unknown intent: err = %v, want ErrInvalidColorTarget", err)
	}
}

// TestConvertColors_RefusalLeavesTheWriter pins that a refusal, the built-in
// engine's included, leaves the Writer as it was, and that a cancelled
// context is returned as it is.
func TestConvertColors_RefusalLeavesTheWriter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// An image whose samples do not decode, which the built-in engine
	// cannot convert.
	src := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources << /XObject << /Im0 5 0 R >> >> >>",
		colorStream("", "1 0 0 rg /Im0 Do"),
		colorStream("/Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode", "not flate"),
	})

	w := NewWriter(openColorPDF(t, src))
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.ConvertColors(ctx, CMYKColorTarget()); !errors.Is(err, ErrColorConversionRefused) {
		t.Fatalf("built-in engine: err = %v, want ErrColorConversionRefused", err)
	}
	w.recolor = (&fakeRecolor{err: errors.New("unsupported construct")}).engine
	if _, err := w.ConvertColors(ctx, CMYKColorTarget()); !errors.Is(err, ErrColorConversionRefused) {
		t.Fatalf("refusing engine: err = %v, want ErrColorConversionRefused", err)
	}
	if w.forceRewrite || w.rewriteOverrides != nil {
		t.Error("a refused conversion changed the Writer")
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), src) {
		t.Error("a refused conversion changed the output")
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	w2 := NewWriter(openColorPDF(t, src))
	t.Cleanup(func() { _ = w2.Close() })
	w2.recolor = (&fakeRecolor{}).engine
	if _, err := w2.ConvertColors(cancelled, CMYKColorTarget()); !errors.Is(err, context.Canceled) || errors.Is(err, ErrColorConversionRefused) {
		t.Errorf("cancelled: err = %v, want context.Canceled alone", err)
	}
}

// TestConvertColors_Composition pins the edit order: an edit queued before
// the conversion reaches the output with it, and a conversion after a
// remediation is refused like any edit.
func TestConvertColors_Composition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := NewWriter(openColorPDF(t, recolorPDF("")))
	t.Cleanup(func() { _ = w.Close() })
	w.recolor = (&fakeRecolor{}).engine
	w.SetMetadata(Metadata{Title: "Converted"})
	if _, err := w.ConvertColors(ctx, CMYKColorTarget()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatal(err)
	}
	meta, err := openColorPDF(t, out.Bytes()).Metadata(ctx)
	if err != nil || meta["Title"] != "Converted" {
		t.Errorf("metadata = %+v, %v, want the title queued before the conversion", meta, err)
	}
	if got := outputIntents(t, out.Bytes()); len(got) != 1 {
		t.Errorf("intents = %+v", got)
	}

	w2 := NewWriter(openColorPDF(t, recolorPDF("")))
	t.Cleanup(func() { _ = w2.Close() })
	w2.remediation = "RemediatePDFA"
	if _, err := w2.ConvertColors(ctx, CMYKColorTarget()); !errors.Is(err, ErrEditAfterRemediation) {
		t.Errorf("after a remediation: err = %v, want ErrEditAfterRemediation", err)
	}
}

// TestConvertColors_NoOpLeavesTheWriter pins that a conversion that changes
// nothing, with no intent to set, forces no rewrite.
func TestConvertColors_NoOpLeavesTheWriter(t *testing.T) {
	t.Parallel()
	w := NewWriter(openColorPDF(t, recolorPDF("")))
	t.Cleanup(func() { _ = w.Close() })
	w.recolor = func(context.Context, *writer.ObjAllocator, *document.Document, []*document.Page, recolorRequest) (recolorResult, error) {
		return recolorResult{}, nil
	}
	report, err := w.ConvertColors(context.Background(), CMYKColorTarget(), WithoutOutputIntent())
	if err != nil {
		t.Fatal(err)
	}
	if report.Target != "DeviceCMYK" || w.forceRewrite {
		t.Errorf("report %+v, forceRewrite %v", report, w.forceRewrite)
	}
}
