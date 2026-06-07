package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// fontPDF builds a single-page PDF and appends the given font/stream object
// bodies after the page (objects 4, 5, ...). The fonts need not be referenced
// by the page: the fixer walks every indirect object.
func fontPDF(fonts ...string) []byte {
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	bodies = append(bodies, fonts...)
	return buildStructPDF(bodies...)
}

func TestMatchCIDToGIDMap(t *testing.T) {
	t.Parallel()
	// The CIDToGIDMap rule fires across PDF/A-2/3/4 and PDF/UA-1/2 with
	// per-part clauses; the description is the only shared key.
	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_2_6_2_11_3_2_t01,
		compliance.Rule_19005_3_6_2_11_3_2_t01,
		compliance.Rule_19005_4_6_2_10_3_2_t01,
		compliance.Rule_14289_1_7_21_3_2_t01,
	} {
		if !matchCIDToGIDMap(compliance.Violation{Rule: rule}) {
			t.Errorf("must match CIDToGIDMap rule %s", rule.Clause)
		}
	}
	// The sibling CIDSystemInfo rule must not be claimed.
	if matchCIDToGIDMap(compliance.Violation{Rule: compliance.Rule_14289_1_7_21_3_1_t01}) {
		t.Error("must not match the CIDSystemInfo rule")
	}
	// And the dedicated CIDSystemInfo matcher must not claim the CIDToGIDMap rule.
	if matchCIDSystemInfo(compliance.Violation{Rule: compliance.Rule_19005_2_6_2_11_3_2_t01}) {
		t.Error("the CIDSystemInfo fixer must not claim the CIDToGIDMap rule")
	}
}

func TestFixCIDToGIDMap_AddsWhenAbsent(t *testing.T) {
	pdf := fontPDF("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Sans >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixCIDToGIDMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2")
	if err != nil {
		t.Fatalf("fixCIDToGIDMap: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if got := pendingDict(t, alloc, 4)["CIDToGIDMap"]; got != ast.PdfName("Identity") {
		t.Errorf("/CIDToGIDMap: got %v, want /Identity", got)
	}
}

func TestFixCIDToGIDMap_NoopWhenIdentity(t *testing.T) {
	pdf := fontPDF("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Sans /CIDToGIDMap /Identity >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixCIDToGIDMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2")
	if err != nil {
		t.Fatalf("fixCIDToGIDMap: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a conformant /Identity needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixCIDToGIDMap_PreservesStream(t *testing.T) {
	// A CIDToGIDMap stream is a real CID-to-glyph mapping and must never be
	// clobbered, even though it satisfies the rule.
	pdf := fontPDF(
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Sans /CIDToGIDMap 5 0 R >>",
		"<< /Length 4 >>\nstream\nXXXX\nendstream",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixCIDToGIDMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2")
	if err != nil {
		t.Fatalf("fixCIDToGIDMap: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a CIDToGIDMap stream must be left untouched: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixCIDToGIDMap_NormalisesInvalidName(t *testing.T) {
	// A name other than Identity is invalid; the only conformant name repair is
	// Identity (the spec default).
	pdf := fontPDF("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Sans /CIDToGIDMap /Custom >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixCIDToGIDMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2")
	if err != nil {
		t.Fatalf("fixCIDToGIDMap: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if got := pendingDict(t, alloc, 4)["CIDToGIDMap"]; got != ast.PdfName("Identity") {
		t.Errorf("invalid name /CIDToGIDMap: got %v, want /Identity", got)
	}
}

func TestFixCIDToGIDMap_NormalisesInvalidType(t *testing.T) {
	// A numeric CIDToGIDMap is neither a stream nor a name; normalise to Identity.
	pdf := fontPDF("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Sans /CIDToGIDMap 0 >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixCIDToGIDMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2")
	if err != nil {
		t.Fatalf("fixCIDToGIDMap: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	if got := pendingDict(t, alloc, 4)["CIDToGIDMap"]; got != ast.PdfName("Identity") {
		t.Errorf("invalid type /CIDToGIDMap: got %v, want /Identity", got)
	}
}

func TestFixCIDToGIDMap_IndirectIdentityName(t *testing.T) {
	// /CIDToGIDMap given as an indirect reference to the name Identity already
	// conforms once resolved and must not be rewritten.
	pdf := fontPDF(
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Sans /CIDToGIDMap 5 0 R >>",
		"/Identity",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixCIDToGIDMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2")
	if err != nil {
		t.Fatalf("fixCIDToGIDMap: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("an indirect /Identity needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixCIDToGIDMap_SkipsUnreadableEntry(t *testing.T) {
	// /CIDToGIDMap is an indirect reference to a non-existent object. The
	// resolve fails, and an unreadable entry is left alone rather than guessed
	// at, so the font is not rewritten.
	pdf := fontPDF("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Sans /CIDToGIDMap 99 0 R >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixCIDToGIDMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2")
	if err != nil {
		t.Fatalf("fixCIDToGIDMap: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("an unreadable /CIDToGIDMap must be skipped: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixCIDToGIDMap_SkipsCIDFontType0(t *testing.T) {
	// CIDFontType0 does not use CIDToGIDMap, so it is never touched.
	pdf := fontPDF("<< /Type /Font /Subtype /CIDFontType0 /BaseFont /ABCDEF+Sans >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixCIDToGIDMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2")
	if err != nil {
		t.Fatalf("fixCIDToGIDMap: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("CIDFontType0 must be skipped: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixCIDToGIDMap_MultiFont(t *testing.T) {
	pdf := fontPDF(
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Sans >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /GHIJKL+Serif /CIDToGIDMap /Bad >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixCIDToGIDMap(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2")
	if err != nil {
		t.Fatalf("fixCIDToGIDMap: %v", err)
	}
	if len(descs) != 1 || !strings.Contains(descs[0], "2 CIDFontType2") {
		t.Fatalf("expected both fonts fixed, got %v", descs)
	}
	for _, n := range []int{4, 5} {
		if got := pendingDict(t, alloc, n)["CIDToGIDMap"]; got != ast.PdfName("Identity") {
			t.Errorf("font %d /CIDToGIDMap: got %v, want /Identity", n, got)
		}
	}
}
