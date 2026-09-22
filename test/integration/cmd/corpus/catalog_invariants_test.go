package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// catalogFactsPDF builds a one-page document that carries the two
// document-level catalog entries on demand.
//
// The objects always exist; only the catalog's reference to them varies. That
// keeps the two variants structurally identical, so a difference in the facts
// can only come from the catalog keys and not from a difference in the file
// shape.
//
// The xref offsets are computed rather than guessed, for the reason
// xfaFormFixturePDF gives in cmd/litho: a stale offset makes the file
// unopenable and fails the test for a reason unrelated to what it asserts.
func catalogFactsPDF(withCollection, withLexicon bool) []byte {
	const pls = `<?xml version="1.0"?><lexicon version="1.0"><lexeme>` +
		`<grapheme>tomato</grapheme><phoneme>t&#x259;ˈmɑːtoʊ</phoneme></lexeme></lexicon>`

	catalog := "<< /Type /Catalog /Pages 2 0 R"
	if withCollection {
		catalog += " /Collection 4 0 R"
	}
	if withLexicon {
		catalog += " /PronunciationLexicon [5 0 R]"
	}
	catalog += " >>"

	objs := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Collection /View /D >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(pls), pls),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return buf.Bytes()
}

// openCatalogFactsPDF opens a fixture and fails the test if it cannot be read.
//
// A fixture litho cannot open would otherwise leave every fact at its zero
// value, and the assertions below would pass for the wrong reason.
func openCatalogFactsPDF(t *testing.T, ctx context.Context, data []byte) *pdf.Reader {
	t.Helper()
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("fixture is not readable, so the test would assert nothing: %v", err)
	}
	// The close error is discarded on purpose: the reader was just proved
	// openable, nothing is written through it, and failing a catalog test on a
	// close would report the wrong defect.
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// TestReadCatalogFacts_ReadsEachSideFromItsOwnDocument is the only test that
// exercises readCatalogFacts itself.
//
// Everything else in this package hands Compare a hand-built catalogFacts, so
// the predicate and the metrics are covered while the code that actually reads
// the two documents is not. The corpus cannot close that gap: no file among
// the 5171 carries /Collection or /PronunciationLexicon, so the portfolio and
// lexicon branches never execute there.
//
// The pair is deliberately ASYMMETRIC and asserted in BOTH directions. Passing
// the same document as src and out would behave identically if the function
// read both facts from one side, so only the mirrored case can catch a swap.
func TestReadCatalogFacts_ReadsEachSideFromItsOwnDocument(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	rich := openCatalogFactsPDF(t, ctx, catalogFactsPDF(true, true))
	bare := openCatalogFactsPDF(t, ctx, catalogFactsPDF(false, false))

	got := readCatalogFacts(ctx, rich, bare)

	// Vacuity guard: if the fixture does not drive the predicates, every
	// assertion below would hold trivially at zero.
	if !got.InPortfolio || got.InLexicons == 0 {
		t.Fatalf("fixture does not exercise the readers: InPortfolio=%v InLexicons=%d",
			got.InPortfolio, got.InLexicons)
	}

	if got.OutPortfolio {
		t.Error("OutPortfolio true for a document with no /Collection; a side was read twice")
	}
	if got.OutLexicons != 0 {
		t.Errorf("OutLexicons = %d for a document with no /PronunciationLexicon", got.OutLexicons)
	}
	if got.InLexicons != 1 {
		t.Errorf("InLexicons = %d, want 1", got.InLexicons)
	}

	// Mirrored: the same two documents, swapped. This is what fails if the
	// function reads a fact from the wrong reader.
	rev := readCatalogFacts(ctx, bare, rich)
	if rev.InPortfolio {
		t.Error("InPortfolio true when the bare document is the input")
	}
	if !rev.OutPortfolio {
		t.Error("OutPortfolio false when the portfolio is the output")
	}
	if rev.InLexicons != 0 {
		t.Errorf("InLexicons = %d when the bare document is the input", rev.InLexicons)
	}
	if rev.OutLexicons != 1 {
		t.Errorf("OutLexicons = %d when the lexicon document is the output", rev.OutLexicons)
	}
}

// TestReadCatalogFacts_DrivesTheDroppedPortfolioVerdict runs the portfolio
// branch end to end, from real PDF bytes through to the verdict.
//
// Until now that branch had never executed anywhere: not in a test, because
// every other test builds catalogFacts by hand, and not in the corpus, because
// no fixture carries /Collection. portfolio_checked has been 0 on every run.
func TestReadCatalogFacts_DrivesTheDroppedPortfolioVerdict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	withPortfolio := openCatalogFactsPDF(t, ctx, catalogFactsPDF(true, false))
	without := openCatalogFactsPDF(t, ctx, catalogFactsPDF(false, false))

	facts := readCatalogFacts(ctx, withPortfolio, without)
	reason := checkCatalogInvariants("merge", facts)
	if reason == "" {
		t.Fatal("dropping /Collection produced no violation")
	}
	if !strings.Contains(reason, "/Collection") {
		t.Errorf("reason: got %q, want it to name /Collection", reason)
	}

	m := catalogInvariantMetrics(facts)
	if m["portfolio_checked"] != 1 || m["portfolio_dropped"] != 1 {
		t.Errorf("metrics: checked=%d dropped=%d, want 1 and 1",
			m["portfolio_checked"], m["portfolio_dropped"])
	}

	// Control: carrying it through reports nothing.
	kept := readCatalogFacts(ctx, withPortfolio, withPortfolio)
	if r := checkCatalogInvariants("merge", kept); r != "" {
		t.Errorf("a preserved portfolio was reported: %q", r)
	}
}

// TestReadCatalogFacts_DrivesTheDroppedLexiconVerdict does the same for the
// lexicon branch, the other counter that has never left zero.
func TestReadCatalogFacts_DrivesTheDroppedLexiconVerdict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	withLexicon := openCatalogFactsPDF(t, ctx, catalogFactsPDF(false, true))
	without := openCatalogFactsPDF(t, ctx, catalogFactsPDF(false, false))

	facts := readCatalogFacts(ctx, withLexicon, without)
	reason := checkCatalogInvariants("split", facts)
	if reason == "" {
		t.Fatal("dropping /PronunciationLexicon produced no violation")
	}
	if !strings.Contains(reason, "input 1, output 0") {
		t.Errorf("reason: got %q, want the input/output counts", reason)
	}

	m := catalogInvariantMetrics(facts)
	if m["lexicons_checked"] != 1 || m["lexicons_dropped"] != 1 {
		t.Errorf("metrics: checked=%d dropped=%d, want 1 and 1",
			m["lexicons_checked"], m["lexicons_dropped"])
	}

	kept := readCatalogFacts(ctx, withLexicon, withLexicon)
	if r := checkCatalogInvariants("split", kept); r != "" {
		t.Errorf("a preserved lexicon was reported: %q", r)
	}
}
