package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// objStmMembersPDF builds, with [objStmPDF], a file whose object stream holds
// one member per body, numbered 3 upwards in order.
func objStmMembersPDF(t *testing.T, bodies ...string) []byte {
	t.Helper()
	var header, body string
	members := make([]int, len(bodies))
	for i, b := range bodies {
		members[i] = 3 + i
		header += fmt.Sprintf("%d %d ", members[i], len(body))
		body += b + "\n"
	}
	return objStmPDF(t, header, body, members...)
}

// openObjStmDoc opens data, a file [objStmPDF] built.
func openObjStmDoc(t *testing.T, data []byte) *Document {
	t.Helper()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Rebuilt {
		t.Fatal("the test file's cross-reference stream did not parse")
	}
	return doc
}

// violationTexts returns the messages of vs.
func violationTexts(vs []error) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Error()
	}
	return out
}

// objStmFormatBodies are the members the format tests below store: an
// odd-digit hex string, a non-hex one inside an array, a clean dictionary, and
// a member whose bytes hold no object.
var objStmFormatBodies = []string{
	"<< /A <414> /B (ok) >>",
	"[<4G> <41>]",
	"<< /C 1 >>",
	">>",
}

// TestResolveCompressed_RecordsFormatViolations pins that a member of an object
// stream parsed in compliance mode records its format violations, as an
// uncompressed object does, and records them once however often it is
// resolved. A clean member and one that does not parse record an empty list,
// which keeps a later format check from parsing them again.
func TestResolveCompressed_RecordsFormatViolations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := openObjStmDoc(t, objStmMembersPDF(t, objStmFormatBodies...))
	doc.SetComplianceMode(true)

	for range 2 {
		for objNum := 3; objNum <= 6; objNum++ {
			_, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum})
			if (err != nil) != (objNum == 6) {
				t.Fatalf("resolve object %d: %v", objNum, err)
			}
		}
	}

	if got := doc.ObjectFormatViolations(3); len(got) != 1 || !errors.Is(got[0], parser.ErrHexStringOddDigits) {
		t.Errorf("object 3 recorded %v, want the odd-digit hex string", got)
	}
	if got := doc.ObjectFormatViolations(4); len(got) != 1 || !errors.Is(got[0], parser.ErrHexStringInvalidChar) {
		t.Errorf("object 4 recorded %v, want the non-hex character", got)
	}
	if got := doc.FormatViolations(); len(got) != 2 {
		t.Errorf("recorded %d violations, want 2: %v", len(got), got)
	}
	doc.mu.RLock()
	_, clean := doc.formatViolations[5]
	_, broken := doc.formatViolations[6]
	doc.mu.RUnlock()
	if !clean || !broken {
		t.Errorf("clean member recorded: %t, unparsable member recorded: %t, want both", clean, broken)
	}
}

// TestCheckObjectFormat_CompressedObjectsAnyAccessOrder pins that the members
// of an object stream contribute the same format violations whichever way they
// were first read: parsed in compliance mode, resolved before compliance mode
// was turned on and then format-checked, or format-checked before any
// resolution. pdf.Open resolves the catalog and the root of the page tree
// before any validation, and a file writing them in an object stream left
// their strings unchecked.
func TestCheckObjectFormat_CompressedObjectsAnyAccessOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := objStmMembersPDF(t, objStmFormatBodies...)

	fresh := openObjStmDoc(t, data)
	fresh.SetComplianceMode(true)
	for objNum := 3; objNum <= 6; objNum++ {
		_, _ = fresh.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum}) // Object 6 fails to parse by design.
	}
	want := violationTexts(fresh.FormatViolations())
	if len(want) != 2 {
		t.Fatalf("a first parse in compliance mode recorded %d violations, want 2: %v", len(want), want)
	}

	warmed := openObjStmDoc(t, data)
	cached := make(map[int]ast.PdfObject)
	for objNum := 3; objNum <= 5; objNum++ {
		obj, err := warmed.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			t.Fatalf("resolve object %d: %v", objNum, err)
		}
		cached[objNum] = obj
	}
	warmed.SetComplianceMode(true)
	for objNum := 3; objNum <= 5; objNum++ {
		if _, err := warmed.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum}); err != nil {
			t.Fatalf("resolve object %d: %v", objNum, err)
		}
	}
	if got := warmed.FormatViolations(); len(got) != 0 {
		t.Fatalf("a cache hit recorded %v; the fixture must reach the members through the cache", got)
	}

	checkedFirst := openObjStmDoc(t, data)
	for _, doc := range []*Document{warmed, checkedFirst} {
		for range 3 {
			for objNum := 3; objNum <= 6; objNum++ {
				doc.CheckObjectFormat(ctx, objNum)
			}
		}
	}
	for name, doc := range map[string]*Document{"resolved first": warmed, "checked first": checkedFirst} {
		got := violationTexts(doc.FormatViolations())
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s: recorded %v, want %v", name, got, want)
		}
	}

	for objNum, obj := range cached {
		got, err := warmed.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			t.Fatalf("resolve object %d: %v", objNum, err)
		}
		if fmt.Sprintf("%p", got) != fmt.Sprintf("%p", obj) {
			t.Errorf("object %d: the format check replaced the cached value", objNum)
		}
	}
}

// TestCheckObjectFormat_CompressedCancelledRecordsNothing pins that a format
// check of a member of an object stream the context cancelled leaves no
// record, so the next check records the member's findings.
func TestCheckObjectFormat_CompressedCancelledRecordsNothing(t *testing.T) {
	t.Parallel()
	for _, after := range []int32{0, 1, 2} {
		doc := openObjStmDoc(t, objStmMembersPDF(t, objStmFormatBodies...))

		doc.CheckObjectFormat(&cancelAfterCtx{Context: context.Background(), after: after}, 3)
		doc.mu.RLock()
		_, recorded := doc.formatViolations[3]
		doc.mu.RUnlock()
		if recorded {
			t.Fatalf("cancelled after %d checkpoints: recorded %v, want nothing", after, doc.ObjectFormatViolations(3))
		}

		doc.CheckObjectFormat(context.Background(), 3)
		if got := doc.ObjectFormatViolations(3); len(got) != 1 {
			t.Errorf("cancelled after %d checkpoints: the next check recorded %v, want the odd-digit hex string", after, got)
		}
	}
}

// TestCheckObjectFormat_CompressedConcurrentWithResolve pins that format checks
// racing compliance-mode resolutions of the members of one object stream leave
// one record per member, the same one whichever goroutine made it.
func TestCheckObjectFormat_CompressedConcurrentWithResolve(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := openObjStmDoc(t, objStmMembersPDF(t, objStmFormatBodies...))
	if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 3}); err != nil {
		t.Fatalf("resolve object 3: %v", err)
	}
	doc.SetComplianceMode(true)

	const goroutines = 16
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			for objNum := 3; objNum <= 6; objNum++ {
				if i%2 == 0 {
					doc.CheckObjectFormat(ctx, objNum)
					continue
				}
				_, _ = doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum}) // Object 6 fails to parse by design.
			}
		})
	}
	wg.Wait()

	if got := doc.FormatViolations(); len(got) != 2 {
		t.Fatalf("recorded %d violations from %d goroutines, want 2 (the two hex strings): %v", len(got), goroutines, got)
	}
}
