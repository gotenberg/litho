package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestExtGStateTransparency1ScopedToDrawnStates pins that ISO 19005-1 §6.4
// judges the graphics states a page sets, not the ones its resource dictionary
// merely lists. veraPDF builds its PDExtGState from the gs operator that selects
// it, which is why its context reads operators[N]/extGState[0], so a state
// nothing selects is reported nowhere.
//
// This is the rule [drawnResources] already states for ISO 19005-2 §6.2.10 and
// ISO 19005-4 §6.2.9, applied to the part 1 walk.
//
// # Corpus evidence
//
//   - pdfjs issue20914.pdf: declares /GDIModeMask with /BM /Darken and never
//     sets it, while a form applies /Transparent. veraPDF reports the alpha of
//     the second and says nothing about the blend mode of the first, while
//     litho surveyed the resource dictionary and reported both.
//   - pdfjs issue6741.pdf: the same shape.
func TestExtGStateTransparency1ScopedToDrawnStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// build lays out a page whose resources declare /GSblend (a forbidden blend
	// mode) and /GSplain, and whose content stream is the caller's.
	build := func(content string) []Violation {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
				" /Resources << /ExtGState << /GSblend 5 0 R /GSplain 6 0 R >> >> >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /Type /ExtGState /BM /Darken >>",
			"<< /Type /ExtGState /BM /Normal >>",
		)
		return checkTransparencyPart1(ctx, doc, pageCtxsFor(ctx, doc, cat))
	}

	t.Run("a state the content never sets is out of scope", func(t *testing.T) {
		t.Parallel()
		if vs := build("/GSplain gs 0 0 10 10 re f"); hasRule(vs, Rule_19005_1_6_4_t04) {
			t.Errorf("an ExtGState no gs selects must not report §6.4 t4, got %v", vs)
		}
	})

	t.Run("a state the content sets is judged", func(t *testing.T) {
		t.Parallel()
		if vs := build("/GSblend gs 0 0 10 10 re f"); !hasRule(vs, Rule_19005_1_6_4_t04) {
			t.Errorf("a blend mode the page selects must report §6.4 t4, got %v", vs)
		}
	})

	t.Run("a page that sets nothing reports nothing", func(t *testing.T) {
		t.Parallel()
		if vs := build("0 0 10 10 re f"); hasRule(vs, Rule_19005_1_6_4_t04) {
			t.Errorf("a page selecting no graphics state must report nothing, got %v", vs)
		}
	})
}

// TestExtGStateTransparency1UnreadableContentStaysUnscoped pins the safe
// direction: when the content cannot be read, a /Contents that does not
// resolve or content past the page budget, there is no drawn set, and
// [drawnResources.setsExtGState] admits every name rather than silently
// emptying the survey. A page litho cannot read must not be called
// transparency-free.
func TestExtGStateTransparency1UnreadableContentStaysUnscoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]"+
			" /Resources << /ExtGState << /GSblend 4 0 R >> >> >>",
		"<< /Type /ExtGState /BM /Darken >>",
	)
	pages := pageCtxsFor(ctx, doc, cat)
	// What collectPageContexts records for a /Contents it cannot read.
	pages[0].parseFailed = true
	pages[0].contentUnreadable = true

	vs := checkTransparencyPart1(ctx, doc, pages)
	if !hasRule(vs, Rule_19005_1_6_4_t04) {
		t.Errorf("unreadable content must leave the survey unscoped, got %v", vs)
	}
}

// TestExtGStateTransparency1NonStreamContentsDrawsNothing pins that a /Contents
// that is no stream nor array is no content rather than unreadable content:
// ISO 32000-1, 7.7.3.3, Table 30 allows only those two, and veraPDF 1.30 reads
// no operator from any other (PDPage.initializeContents). Measured at
// PDF/A-1b with a dictionary, null and an integer as the /Contents of a page
// whose resources name an ExtGState with /BM /Darken: none fails ISO 19005-1,
// 6.4 t4, which the same page drawing the state with gs fails once.
func TestExtGStateTransparency1NonStreamContentsDrawsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, contents := range []string{"5 0 R", "null", "7"} {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents "+contents+
				" /Resources << /ExtGState << /GSblend 4 0 R >> >> >>",
			"<< /Type /ExtGState /BM /Darken >>",
			"<< /NotAStream true >>",
		)
		if vs := checkTransparencyPart1(ctx, doc, pageCtxsFor(ctx, doc, cat)); hasRule(vs, Rule_19005_1_6_4_t04) {
			t.Errorf("/Contents %s draws nothing, got %v", contents, vs)
		}
	}
}
