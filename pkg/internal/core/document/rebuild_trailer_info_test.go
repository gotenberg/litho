package document

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// stubLastTrailerPDF builds a damaged incremental update whose LAST trailer is
// a stub carrying only /Size and /ID, while the real trailer earlier in the
// file states /Info, /Root and /Prev. Both startxref offsets are unusable, so
// the reader falls back to a rebuild.
//
// This is the shape of govdocs thread_100_100982.pdf and thread_100_100983.pdf,
// whose first startxref is a malformed 0 and whose second points into the file
// body rather than at an xref table.
func stubLastTrailerPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
			"3 0 obj\n<< /ModDate (D:20200101000000Z) /Creator (Acme) >>\nendobj\n" +
			"trailer<< /Size 4 /Info 3 0 R /Root 1 0 R /Prev 0 /ID [<aa> <aa>] >>\nstartxref\n0\n%%EOF\n" +
			"trailer<< /Size 4 /ID [<aa> <aa>] >>\nstartxref\n173\n%%EOF\n")
}

// noTrailerPDF has parseable objects but no trailer keyword at all, so the
// rebuild synthesizes one. Nothing may be recovered into a trailer litho
// invented.
func noTrailerPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n" +
			"3 0 obj\n<< /ModDate (D:20200101000000Z) /Creator (Acme) >>\nendobj\n" +
			"startxref\n99999\n%%EOF\n")
}

// TestRebuild_RecoversInfoFromEarlierTrailer pins that a rebuild whose
// recovered trailer lacks /Info takes it from the newest earlier trailer that
// states one.
//
// The xref CHAIN path already carries /Root and /Info forward across sections
// with [mergeTrailerKeys]; the rebuild path did not, so a file whose last
// trailer is a stub lost an /Info it plainly declares. ISO 19005-4, 6.1.3 t4
// and t5 read that entry, and litho reported neither while veraPDF reported
// both on govdocs thread_100_100982.pdf.
//
// Only /Info is recovered, and only when a real trailer was found. The
// synthesized arm is the guard that matters: litho must not furnish a trailer
// it invented with entries the file never linked, which is the same principle
// [Document.TrailerSynthesized] already enforces for /ID.
func TestRebuild_RecoversInfoFromEarlierTrailer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a stub last trailer takes /Info from the earlier one", func(t *testing.T) {
		t.Parallel()
		data := stubLastTrailerPDF()
		doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		if !doc.Rebuilt {
			t.Fatalf("fixture must exercise the rebuild path, got Rebuilt=false")
		}
		if doc.TrailerSynthesized {
			t.Fatalf("a real trailer exists, so it must not be synthesized")
		}
		infoObj, ok := doc.Index.Trailer["Info"]
		if !ok {
			t.Fatalf("/Info was not recovered; trailer = %v", doc.Index.Trailer)
		}
		ref, ok := infoObj.(ast.PdfIndirectRef)
		if !ok || ref.ObjectNum != 3 {
			t.Fatalf("/Info = %v, want a reference to object 3", infoObj)
		}
		info, err := ResolveDict(ctx, doc, infoObj)
		if err != nil || info == nil {
			t.Fatalf("recovered /Info does not resolve: %v", err)
		}
		if _, hasCreator := info["Creator"]; !hasCreator {
			t.Errorf("resolved /Info = %v, want the dictionary carrying /Creator", info)
		}
	})

	t.Run("a synthesized trailer gains nothing", func(t *testing.T) {
		t.Parallel()
		data := noTrailerPDF()
		doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		if !doc.TrailerSynthesized {
			t.Fatalf("fixture must exercise the synthesized path, got TrailerSynthesized=false")
		}
		if _, hasInfo := doc.Index.Trailer["Info"]; hasInfo {
			t.Errorf("a trailer litho invented must not gain /Info, got %v", doc.Index.Trailer)
		}
	})

	t.Run("a healthy file is untouched", func(t *testing.T) {
		t.Parallel()
		data := minimalPDF()
		doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		if doc.Rebuilt {
			t.Errorf("a valid xref must not rebuild")
		}
		if _, hasInfo := doc.Index.Trailer["Info"]; hasInfo {
			t.Errorf("the fixture declares no /Info, so none may appear: %v", doc.Index.Trailer)
		}
	})
}

// trailerBudgetPDF places a stub trailer keyword inside the chunk overlap
// region the trailer scan uses, so the scan sights that one keyword twice, and
// follows it with enough further stubs that exactly [maxRecoveredTrailers]
// distinct trailers exist. The /Info trailer is the oldest of them, so it sits
// precisely at the budget edge: it is reachable only when a repeated sighting
// does not spend a slot.
func trailerBudgetPDF(t *testing.T) []byte {
	t.Helper()

	// Mirrors the overlap in [Document.findTrailerInfoRef]. A keyword is read
	// by two consecutive chunks when it starts within the trailing overlap of
	// one chunk yet still ends before that chunk does.
	const scanOverlap = 16

	const keyword = "trailer"
	start := rebuildChunkSize - scanOverlap
	if start+len(keyword) > rebuildChunkSize {
		t.Fatalf("overlap %d cannot double-sight a %d-byte keyword", scanOverlap, len(keyword))
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	buf.WriteString("3 0 obj\n<< /ModDate (D:20200101000000Z) /Creator (Acme) >>\nendobj\n")
	buf.WriteString("trailer<< /Size 4 /Info 3 0 R /Root 1 0 R >>\n")
	if buf.Len() > start {
		t.Fatalf("prefix of %d bytes already runs past the overlap window at %d", buf.Len(), start)
	}
	buf.Write(bytes.Repeat([]byte(" "), start-buf.Len()))

	for range maxRecoveredTrailers - 1 {
		buf.WriteString("trailer<< /Size 4 /ID [<aa> <aa>] >>\n")
	}
	buf.WriteString("startxref\n0\n%%EOF\n")
	return buf.Bytes()
}

// TestRebuild_TrailerBudgetCountsDistinctTrailers pins that a trailer keyword
// read by two consecutive chunks spends one slot of [maxRecoveredTrailers],
// not two. The scan overlaps its chunks so a boundary cannot split a keyword,
// which means a keyword inside the overlap is seen twice; counting both
// sightings pushed the oldest trailer out of range and lost the /Info only it
// declares.
func TestRebuild_TrailerBudgetCountsDistinctTrailers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := trailerBudgetPDF(t)
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if !doc.Rebuilt {
		t.Fatalf("fixture must exercise the rebuild path, got Rebuilt=false")
	}
	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		t.Fatalf("/Info lost across %d distinct trailers; trailer = %v", maxRecoveredTrailers, doc.Index.Trailer)
	}
	ref, ok := infoObj.(ast.PdfIndirectRef)
	if !ok || ref.ObjectNum != 3 {
		t.Fatalf("/Info = %v, want a reference to object 3", infoObj)
	}
}

// TestRebuild_TrailerInfoScanHonoursCancellation pins that the whole-file
// trailer scan stops once the caller cancels. The scan reads every chunk of the
// document, so on a large file an unbounded run would keep working long after
// the request that wanted it went away.
func TestRebuild_TrailerInfoScanHonoursCancellation(t *testing.T) {
	t.Parallel()

	data := stubLastTrailerPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	// Control: the same document does offer a recoverable /Info under a live
	// context, so the cancelled arm below reports cancellation rather than an
	// unrelated failure to find a trailer.
	if _, ok := doc.findTrailerInfoRef(context.Background()); !ok {
		t.Fatalf("fixture must offer a recoverable /Info under a live context")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := doc.findTrailerInfoRef(cancelled); ok {
		t.Errorf("a cancelled context must stop the trailer scan")
	}
}
