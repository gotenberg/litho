package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// hCodeCount is how many codes the bundled H CMap maps, measured from
// bcmaps/H.bcmap. Pinned so a short copy cannot pass: a validator is satisfied
// by /UseCMap pointing at a stream and never asks what the stream maps, so an
// empty or truncated embed would score exactly as well as a faithful one.
const hCodeCount = 6879

// embeddedBase follows the /UseCMap reference the fixer wrote and returns the
// stream it points at.
func embeddedBase(t *testing.T, alloc *writer.ObjAllocator) (ast.PdfStream, bool) {
	t.Helper()

	enc, ok := flattenedCMap(t, alloc)
	if !ok {
		return ast.PdfStream{}, false
	}
	ref, isRef := enc.Dict["UseCMap"].(ast.PdfIndirectRef)
	if !isRef {
		return ast.PdfStream{}, false
	}
	pending, ok := alloc.Pending()[ref.ObjectNum]
	if !ok {
		return ast.PdfStream{}, false
	}
	stream, ok := pending.(ast.PdfStream)
	return stream, ok
}

// TestFixCMapFlatten_EmbedsAPredefinedUseCMapUnderPart1 covers the repair
// ISO 19005-1, 6.3.3.3 asks for: H is a real ISO 32000-1 Table 118 CMap, so it
// cannot be dropped, and part 1 requires it embedded rather than named.
func TestFixCMapFlatten_EmbedsAPredefinedUseCMapUnderPart1(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, namedUseCMapPDF("H"))

	fixes, err := fixCMapFlatten(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixCMapFlatten: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("reported %d fixes, want 1: %v", len(fixes), fixes)
	}

	enc, ok := flattenedCMap(t, alloc)
	if !ok {
		t.Fatal("the encoding CMap was not rewritten")
	}
	if _, stillNamed := enc.Dict["UseCMap"].(ast.PdfName); stillNamed {
		t.Fatal("/UseCMap is still a name, so the CMap is still not embedded")
	}

	base, ok := embeddedBase(t, alloc)
	if !ok {
		t.Fatal("/UseCMap does not point at a stream this run wrote")
	}
	if typ, _ := base.Dict["Type"].(ast.PdfName); typ != "CMap" {
		t.Errorf("the embedded stream is /Type %q, want CMap", typ)
	}
	// The name must survive: veraPDF reads the REFERENCED CMap's name and
	// checks it against Table 118, where H belongs.
	if nm, _ := base.Dict["CMapName"].(ast.PdfName); nm != "H" {
		t.Errorf("the embedded stream is named %q, want H", nm)
	}

	data, err := base.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the embedded CMap: %v", err)
	}
	got, err := ParseCIDForTest(ctx, data)
	if err != nil {
		t.Fatalf("the embedded CMap does not parse: %v", err)
	}
	if len(got) != hCodeCount {
		t.Errorf("the embedded CMap maps %d codes, want %d: the copy is not faithful",
			len(got), hCodeCount)
	}
	// The tabulated CIDSystemInfo, not a fabricated one: ISO 19005-1, 6.3.3.1
	// compares it against the CIDFont's.
	sys, _ := base.Dict["CIDSystemInfo"].(ast.PdfDict)
	reg, ord, sup, _ := cmap.PredefinedCMapCIDSystemInfo("H", false)
	if s, _ := sys["Registry"].(ast.PdfString); string(s) != reg {
		t.Errorf("/Registry is %q, want %q", s, reg)
	}
	if s, _ := sys["Ordering"].(ast.PdfString); string(s) != ord {
		t.Errorf("/Ordering is %q, want %q", s, ord)
	}
	if n, _ := sys["Supplement"].(ast.PdfInt); int(n) != sup {
		t.Errorf("/Supplement is %v, want %d (the tabulated value)", sys["Supplement"], sup)
	}
}

// TestFixCMapFlatten_EmbedsOnlyUnderPart1 pins the profile gate. Parts 2 and
// later, and PDF/UA, PERMIT the Table 118 names by name, so embedding there
// rewrites a document for no compliance gain. Without this arm the gate could
// be deleted and every other test would still pass.
func TestFixCMapFlatten_EmbedsOnlyUnderPart1(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, profile := range []string{"PDF/A-2b", "PDF/A-2u", "PDF/A-4", "PDF/UA-1", "PDF/UA-2"} {
		doc, alloc := newTestDoc(t, namedUseCMapPDF("H"))

		fixes, err := fixCMapFlatten(ctx, alloc, doc, Options{}, nil, profile)
		if err != nil {
			t.Errorf("%s: fixCMapFlatten: %v", profile, err)
			continue
		}
		if len(fixes) != 0 {
			t.Errorf("%s: reported %v, but this profile permits the name", profile, fixes)
		}
		if _, written := flattenedCMap(t, alloc); written {
			t.Errorf("%s: rewrote the encoding CMap where the name is permitted", profile)
		}
	}
}

// TestFixCMapFlatten_EmbedsOnlyWhatLithoShips pins the other refusal. A name
// litho has no bundled data for cannot be embedded, because writing a CMap it
// cannot read would fabricate the mapping the font depends on. V is the
// interesting case: its own records hold only the vertical forms and it takes
// the rest from H through usecmap, which litho resolves, so it embeds.
func TestFixCMapFlatten_EmbedsOnlyWhatLithoShips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		useName string
		embed   bool
		why     string
	}{
		{"H", true, "bundled, uniform width"},
		{"V", true, "bundled, uniform width once its usecmap base H is resolved"},
		{"90ms-RKSJ-H", false, "mixed-width codespace would merge one- and two-byte codes"},
		{"Identity-H", false, "exempt from the embedding rule"},
		{"NotACMapName", false, "litho ships no such CMap"},
	} {
		doc, alloc := newTestDoc(t, namedUseCMapPDF(tc.useName))

		fixes, err := fixCMapFlatten(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
		if err != nil {
			t.Errorf("%s: fixCMapFlatten: %v", tc.useName, err)
			continue
		}
		_, written := flattenedCMap(t, alloc)
		if tc.embed && !written {
			t.Errorf("/UseCMap /%s was not embedded (%s)", tc.useName, tc.why)
		}
		if !tc.embed && written {
			t.Errorf("/UseCMap /%s was embedded, but %s: %v", tc.useName, tc.why, fixes)
		}
	}
}

// TestFixCMapFlatten_EmbedsAVerticalUseCMapWithItsWMode pins that the stream
// written for V says what V says: the 6879 codes it takes from H, and /WMode 1
// in both the dictionary and the program, which ISO 19005-2, 6.2.11.3.3
// requires identical.
func TestFixCMapFlatten_EmbedsAVerticalUseCMapWithItsWMode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, namedUseCMapPDF("V"))
	if _, err := fixCMapFlatten(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixCMapFlatten: %v", err)
	}
	base, ok := embeddedBase(t, alloc)
	if !ok {
		t.Fatal("/UseCMap /V was not embedded")
	}
	if w, _ := base.Dict["WMode"].(ast.PdfInt); w != 1 {
		t.Errorf("the embedded V stream's dictionary declares /WMode %v, want 1", base.Dict["WMode"])
	}
	data, err := base.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the embedded CMap: %v", err)
	}
	got, err := ParseCIDForTest(ctx, data)
	if err != nil {
		t.Fatalf("the embedded CMap does not parse: %v", err)
	}
	if len(got) != hCodeCount {
		t.Errorf("the embedded V maps %d codes, want the %d it takes from H", len(got), hCodeCount)
	}
	if !bytes.Contains(data, []byte("/WMode 1 def")) {
		t.Error("the embedded V program does not declare /WMode 1")
	}
}
