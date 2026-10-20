package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// TestFixOutputIntent_CMYKIntentCarriesAUsablePrinterProfile pins the CMYK
// output intent remediation adds: a version 2 printer profile (ISO 19005-1,
// 6.2.2) that converts colour, read by litho's engine as DeviceCMYK reads by
// default, under the registered condition it characterises.
//
// The profile it replaced was a lut8 table into XYZ whose every entry was
// 128, so every CMYK colour converted to one pale grey, and litho's engine
// could not build a transform from it at all.
func TestFixOutputIntent_CMYKIntentCarriesAUsablePrinterProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	))
	v := compliance.Violation{
		Rule:    compliance.Rule{Clause: "6.2.4.3", TestNumber: 3},
		Context: "DeviceCMYK used without OutputIntent",
	}
	if _, err := fixOutputIntent(ctx, alloc, doc, Options{}, []compliance.Violation{v}, "PDF/A-2b"); err != nil {
		t.Fatalf("fixOutputIntent: %v", err)
	}

	var intent ast.PdfDict
	for _, obj := range alloc.Pending() {
		if d, ok := obj.(ast.PdfDict); ok && d["Type"] == ast.PdfName("OutputIntent") {
			intent = d
		}
	}
	if intent == nil {
		t.Fatal("no OutputIntent allocated")
	}
	if intent["OutputConditionIdentifier"] != ast.PdfString(cmykCondition) || intent["RegistryName"] != ast.PdfString(cmykRegistry) {
		t.Errorf("condition %v in registry %v, want %q in %q",
			intent["OutputConditionIdentifier"], intent["RegistryName"], cmykCondition, cmykRegistry)
	}
	ref, ok := intent["DestOutputProfile"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("DestOutputProfile = %v", intent["DestOutputProfile"])
	}
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("DestOutputProfile reads object %d, which is %T, want a pending stream",
			ref.ObjectNum, alloc.Pending()[ref.ObjectNum])
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		t.Fatalf("profile bytes: %v", err)
	}
	if n, _ := stream.Dict["N"].(ast.PdfInt); n != 4 || data[8] != 2 || string(data[12:20]) != "prtrCMYK" {
		t.Fatalf("/N %v, header %q", stream.Dict["N"], data[8:20])
	}
	p, err := icc.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tr, err := p.NewTransform(icc.RelativeColorimetric)
	if err != nil {
		t.Fatalf("NewTransform: %v", err)
	}
	builtin, err := icc.BuiltinCMYK(icc.RelativeColorimetric)
	if err != nil {
		t.Fatalf("BuiltinCMYK: %v", err)
	}
	for _, cmyk := range [][]byte{{0, 0, 0, 0}, {255, 0, 0, 0}, {0, 255, 0, 0}, {0, 0, 255, 0}, {0, 0, 0, 255}} {
		r, g, b := tr.Convert(cmyk)
		wr, wg, wb := builtin.Convert(cmyk)
		if r != wr || g != wg || b != wb {
			t.Errorf("CMYK %v reads %d,%d,%d, DeviceCMYK %d,%d,%d", cmyk, r, g, b, wr, wg, wb)
		}
	}
}
