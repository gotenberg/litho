package writer

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// firstDiff describes where two outputs first differ, so a failure names the
// change rather than printing two byte counts. A version bump keeps the length
// identical ("%PDF-1.4" and "%PDF-1.5" are the same size), which made the
// obvious message read "204 vs 204 bytes" and say nothing at all.
func firstDiff(a, b string) string {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return fmt.Sprintf("at byte %d: %q vs %q",
				i, a[max(0, i-12):min(len(a), i+12)], b[max(0, i-12):min(len(b), i+12)])
		}
	}
	if len(a) != len(b) {
		return fmt.Sprintf("same first %d bytes, then lengths %d vs %d", n, len(a), len(b))
	}
	return "identical"
}

// Object streams and cross-reference streams are PDF 1.5 constructs, and the
// rewriter used to emit both under whatever version the source declared. A
// 1.4 document written with packing came out as "%PDF-1.4" carrying an
// /ObjStm and a /Type /XRef, which a 1.4 reader cannot parse at all and which
// litho's own PDF/A-1 check reports as using xref streams (ISO 19005-1,
// 6.1.4).
//
// Nothing caught it: the suite, the linter and qpdf all check that the bytes
// parse, and every modern reader ignores the header version. None of them
// compares what a file declares against what it contains.

// flattenStreams returns out with every inflatable stream payload appended, so
// an assertion can still find a dictionary that packing moved inside a
// compressed object stream.
func flattenStreams(t *testing.T, out string) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(out)

	rest := out
	for {
		i := strings.Index(rest, "stream\r\n")
		if i < 0 {
			break
		}
		rest = rest[i+len("stream\r\n"):]
		j := strings.Index(rest, "\r\nendstream")
		if j < 0 {
			break
		}
		payload := rest[:j]
		rest = rest[j:]

		zr, err := zlib.NewReader(strings.NewReader(payload))
		if err != nil {
			continue // not a Flate payload; nothing to read here
		}
		inflated, err := io.ReadAll(zr)
		if cerr := zr.Close(); cerr != nil {
			t.Logf("closing inflater: %v", cerr)
		}
		if err != nil {
			continue
		}
		sb.Write(inflated)
	}
	return sb.String()
}

// containsEntry reports whether s carries the given dictionary entry, with or
// without the separating space, so the assertion does not depend on the
// serializer's spacing.
func containsEntry(s, key, value string) bool {
	return strings.Contains(s, key+" "+value) || strings.Contains(s, key+value)
}

// rewriteTo writes the document cfg describes and returns the bytes.
func rewriteTo(t *testing.T, cfg RewriteConfig) string {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteRewrite(context.Background(), &buf, cfg); err != nil {
		t.Fatalf("WriteRewrite: %v", err)
	}
	return buf.String()
}

// TestWriteRewrite_PackedOutputDeclaresPDF15 is the regression: a packed file
// must declare the version its own contents require.
//
// # Corpus evidence
//
//   - All corpora: the corpus gate missed the undeclared PDF 1.5 constructs,
//     because it checks only that the bytes parse.
func TestWriteRewrite_PackedOutputDeclaresPDF15(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})
	substantialDicts(alloc, 12)

	out := rewriteTo(t, RewriteConfig{
		Version:           "1.4",
		Allocator:         alloc,
		Trailer:           ast.PdfDict{"Root": catalog},
		PackObjectStreams: true,
	})

	// Assert packing actually happened first. Without this the version check
	// below would pass on a file that simply never packed.
	if !strings.Contains(out, "/Type /ObjStm") {
		t.Fatalf("nothing was packed, so this proves nothing about the version")
	}
	if !strings.HasPrefix(out, "%PDF-1.5\n") {
		t.Errorf("packed output declares %q, want %%PDF-1.5; it carries PDF 1.5 constructs",
			out[:min(len(out), 12)])
	}
}

// TestWriteRewrite_DeclinedPackingKeepsTheSourceVersion is the control that
// separates this fix from a naive one. Raising the version whenever packing is
// REQUESTED would pass the regression above and still be wrong: a document too
// small to benefit is written unpacked, needs nothing from 1.5, and must come
// out exactly as an uncompacted rewrite would write it.
func TestWriteRewrite_DeclinedPackingKeepsTheSourceVersion(t *testing.T) {
	t.Parallel()
	build := func(pack bool) string {
		alloc := NewObjAllocator(0)
		catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})
		alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Pages")})
		return rewriteTo(t, RewriteConfig{
			Version:           "1.4",
			Allocator:         alloc,
			Trailer:           ast.PdfDict{"Root": catalog},
			PackObjectStreams: pack,
		})
	}

	packed, plain := build(true), build(false)
	if strings.Contains(packed, "/Type /ObjStm") {
		t.Fatal("fixture packed after all; it no longer exercises the declined path")
	}
	if !strings.HasPrefix(packed, "%PDF-1.4\n") {
		t.Errorf("declined packing still raised the version to %q",
			packed[:min(len(packed), 12)])
	}
	if packed != plain {
		t.Errorf("declined packing changed the output: %s", firstDiff(packed, plain))
	}
}

// TestWriteRewrite_PlainOutputKeepsTheSourceVersion pins the other direction:
// a rewrite that was never asked to compact must not drift upward.
func TestWriteRewrite_PlainOutputKeepsTheSourceVersion(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})
	substantialDicts(alloc, 12)

	out := rewriteTo(t, RewriteConfig{
		Version:   "1.4",
		Allocator: alloc,
		Trailer:   ast.PdfDict{"Root": catalog},
	})
	if !strings.HasPrefix(out, "%PDF-1.4\n") {
		t.Errorf("plain rewrite declares %q, want %%PDF-1.4", out[:min(len(out), 12)])
	}
}

// TestWriteRewrite_XRefStreamAloneDeclaresPDF15 covers the half of this that
// predates compaction: a caller selecting a cross-reference stream directly
// gets the same 1.5 constructs and needs the same declaration. pdf.WithXRefStream
// has documented "PDF 1.5+" all along without enforcing it.
func TestWriteRewrite_XRefStreamAloneDeclaresPDF15(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	catalog := alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Catalog")})

	out := rewriteTo(t, RewriteConfig{
		Version:       "1.4",
		Allocator:     alloc,
		Trailer:       ast.PdfDict{"Root": catalog},
		UseXRefStream: true,
	})
	if !strings.Contains(out, "/Type /XRef") {
		t.Fatal("no cross-reference stream in the output")
	}
	if !strings.HasPrefix(out, "%PDF-1.5\n") {
		t.Errorf("xref-stream output declares %q, want %%PDF-1.5", out[:min(len(out), 12)])
	}
}

// TestWriteRewrite_RaisesTheCatalogVersionWhenPacking covers the entry that
// silently defeats a header-only fix. Per ISO 32000-1, 7.5.2 the catalog
// /Version overrides the header, and litho's reader honors it downward
// (document.initCatalogVersion), so a catalog saying 1.4 would keep the file
// reporting 1.4 whatever the header claims.
func TestWriteRewrite_RaisesTheCatalogVersionWhenPacking(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	catalog := alloc.Alloc(ast.PdfDict{
		"Type":    ast.PdfName("Catalog"),
		"Version": ast.PdfName("1.4"),
	})
	substantialDicts(alloc, 12)

	out := rewriteTo(t, RewriteConfig{
		Version:           "1.4",
		Allocator:         alloc,
		Trailer:           ast.PdfDict{"Root": catalog},
		PackObjectStreams: true,
	})
	if !strings.Contains(out, "/Type /ObjStm") {
		t.Fatal("nothing was packed, so this proves nothing about the catalog")
	}

	flat := flattenStreams(t, out)
	if !containsEntry(flat, "/Version", "/1.5") {
		t.Error("catalog /Version was not raised; the file still reports PDF 1.4")
	}
	if containsEntry(flat, "/Version", "/1.4") {
		t.Error("catalog still carries /Version /1.4 alongside PDF 1.5 constructs")
	}
}

// TestWriteRewrite_LeavesALaterCatalogVersionAlone: the raise is a floor, not
// an assignment. A catalog declaring 1.7 must not be dragged down to 1.5.
func TestWriteRewrite_LeavesALaterCatalogVersionAlone(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	catalog := alloc.Alloc(ast.PdfDict{
		"Type":    ast.PdfName("Catalog"),
		"Version": ast.PdfName("1.7"),
	})
	substantialDicts(alloc, 12)

	out := rewriteTo(t, RewriteConfig{
		Version:           "1.7",
		Allocator:         alloc,
		Trailer:           ast.PdfDict{"Root": catalog},
		PackObjectStreams: true,
	})
	flat := flattenStreams(t, out)
	if !containsEntry(flat, "/Version", "/1.7") {
		t.Error("a catalog /Version of 1.7 was lowered by the raise")
	}
	if !strings.HasPrefix(out, "%PDF-1.7\n") {
		t.Errorf("header declares %q; 1.7 already exceeds the floor", out[:min(len(out), 12)])
	}
}

// TestWriteRewrite_DeclinedPackingRestoresTheCatalogVersion exercises the undo.
// The catalog has to be raised BEFORE packing, because packing seals it inside
// a compressed stream, but whether the raise is warranted is known only after
// packing returns. A declined pack must leave no trace.
func TestWriteRewrite_DeclinedPackingRestoresTheCatalogVersion(t *testing.T) {
	t.Parallel()
	build := func(pack bool) string {
		alloc := NewObjAllocator(0)
		catalog := alloc.Alloc(ast.PdfDict{
			"Type":    ast.PdfName("Catalog"),
			"Version": ast.PdfName("1.4"),
		})
		alloc.Alloc(ast.PdfDict{"Type": ast.PdfName("Pages")})
		return rewriteTo(t, RewriteConfig{
			Version:           "1.4",
			Allocator:         alloc,
			Trailer:           ast.PdfDict{"Root": catalog},
			PackObjectStreams: pack,
		})
	}

	packed, plain := build(true), build(false)
	if strings.Contains(packed, "/Type /ObjStm") {
		t.Fatal("fixture packed after all; it no longer exercises the undo")
	}
	if !containsEntry(packed, "/Version", "/1.4") {
		t.Error("declined packing left the catalog /Version raised")
	}
	if packed != plain {
		t.Errorf("declined packing changed the output: %s", firstDiff(packed, plain))
	}
}

func TestAtLeastVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		v     string
		floor string
		want  string
	}{
		{"raises an earlier version", "1.4", "1.5", "1.5"},
		{"keeps a later version", "1.7", "1.5", "1.7"},
		{"keeps an equal version", "1.5", "1.5", "1.5"},
		{"keeps a later major", "2.0", "1.5", "2.0"},
		// A lexical comparison answers "1.5" here, which is the trap this
		// helper exists to avoid.
		{"compares minors numerically", "1.10", "1.5", "1.10"},
		{"an empty version takes the floor", "", "1.5", "1.5"},
		{"a non-version takes the floor", "Nonsense", "1.5", "1.5"},
		{"a partial version takes the floor", "1.", "1.5", "1.5"},
		{"a signed component is not a version", "+1.5", "1.5", "1.5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := atLeastVersion(tc.v, tc.floor); got != tc.want {
				t.Errorf("atLeastVersion(%q, %q) = %q, want %q", tc.v, tc.floor, got, tc.want)
			}
		})
	}
}

// TestRaiseCatalogVersion_LeavesWhatReadersIgnore covers the two catalogs that
// must not be touched: one with no /Version, where the header already answers
// the question and adding the key would write an entry the source never had,
// and one whose /Version is not a version number, which readers ignore
// (document.initCatalogVersion does) and which therefore misdescribes nothing.
func TestRaiseCatalogVersion_LeavesWhatReadersIgnore(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		catalog ast.PdfDict
	}{
		{"no /Version entry", ast.PdfDict{"Type": ast.PdfName("Catalog")}},
		{"/Version is not a version", ast.PdfDict{
			"Type":    ast.PdfName("Catalog"),
			"Version": ast.PdfName("Nonsense"),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			alloc := NewObjAllocator(0)
			ref := alloc.Alloc(tc.catalog)
			trailer := ast.PdfDict{"Root": ref}

			undo := raiseCatalogVersion(alloc, trailer, minXRefStreamVersion)
			undo()

			got, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
			if !ok {
				t.Fatal("catalog left the pending set")
			}
			if len(got) != len(tc.catalog) {
				t.Errorf("catalog gained or lost entries: %v", got)
			}
			if got["Version"] != tc.catalog["Version"] {
				t.Errorf("catalog /Version changed to %v, want %v",
					got["Version"], tc.catalog["Version"])
			}
		})
	}
}

// TestRaiseCatalogVersion_DoesNotMutateInPlace pins the allocator's contract:
// [ObjAllocator.Snapshot] captures pending entries by reference, so editing
// the catalog dictionary in place would also alter a snapshot taken earlier.
func TestRaiseCatalogVersion_DoesNotMutateInPlace(t *testing.T) {
	t.Parallel()
	alloc := NewObjAllocator(0)
	original := ast.PdfDict{
		"Type":    ast.PdfName("Catalog"),
		"Version": ast.PdfName("1.4"),
	}
	ref := alloc.Alloc(original)

	raiseCatalogVersion(alloc, ast.PdfDict{"Root": ref}, minXRefStreamVersion)

	if original["Version"] != ast.PdfName("1.4") {
		t.Errorf("the original catalog dictionary was mutated to %v", original["Version"])
	}
	raised, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatal("catalog left the pending set")
	}
	if raised["Version"] != ast.PdfName("1.5") {
		t.Errorf("allocator holds /Version %v, want /1.5", raised["Version"])
	}
}
