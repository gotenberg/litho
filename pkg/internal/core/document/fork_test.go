package document

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// forkFields records what [Document.Fork] does with each field of [Document]:
// "shared" when the fork reads the parent's value, which the parent fixed at
// construction and only reads afterwards; "copied" when the fork holds a copy
// it may change on its own; "fresh" when the fork starts from the zero or empty
// value. A field this table does not name fails
// TestForkDecidesEveryField, so a new field cannot reach a fork, or miss
// one, without a decision.
var forkFields = map[string]string{
	"r":                       "shared",
	"size":                    "copied",
	"Index":                   "copied",
	"xrefEntries":             "copied",
	"objectCache":             "fresh",
	"fontCache":               "fresh",
	"cmapCache":               "fresh",
	"objStmCache":             "fresh",
	"objStmCacheBytes":        "fresh",
	"fontProgCache":           "fresh",
	"fontProgBytes":           "fresh",
	"colorCache":              "fresh",
	"colorCacheBytes":         "fresh",
	"mu":                      "fresh",
	"runMu":                   "fresh",
	"xrefAugment":             "copied",
	"xrefAugmented":           "copied",
	"xrefAdded":               "copied",
	"maxCacheSize":            "copied",
	"limits":                  "copied",
	"Crypto":                  "shared",
	"streamCrypt":             "copied",
	"OCConfig":                "shared",
	"IsEncrypted":             "copied",
	"Authenticated":           "copied",
	"Logger":                  "copied",
	"Linearization":           "shared",
	"HintTable":               "shared",
	"Rebuilt":                 "copied",
	"TrailerSynthesized":      "copied",
	"HeaderOffset":            "copied",
	"Version":                 "copied",
	"HeaderVersion":           "copied",
	"complianceMode":          "fresh",
	"formatViolations":        "fresh",
	"integerOverflows":        "fresh",
	"trailerFormatViolations": "shared",
	"trailerMergedKeys":       "shared",
	"trailerStream":           "copied",
	"objectStarts":            "shared",
	"declaredStarts":          "shared",
	"XRefTableOffsets":        "shared",
	"XRefSectionOffsets":      "shared",
	"Filters":                 "shared",
	"CMYKProfile":             "shared",
}

// TestForkDecidesEveryField pins that [forkFields] names every field of
// [Document] and nothing else.
func TestForkDecidesEveryField(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeFor[Document]()
	seen := make(map[string]bool, typ.NumField())
	for f := range typ.Fields() {
		seen[f.Name] = true
		if _, ok := forkFields[f.Name]; !ok {
			t.Errorf("Document.%s: decide what Fork does with it and record it in forkFields", f.Name)
		}
	}
	for name := range forkFields {
		if !seen[name] {
			t.Errorf("forkFields names %s, which Document no longer has", name)
		}
	}
}

// unlistedObjectPDF builds a one-page document whose cross-reference table
// lists objects 1 to 3 and leaves out object 4, which the file defines, so
// only the raw-byte augmentation finds it.
func unlistedObjectPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Unlisted true >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	b.WriteString("xref\n0 4\n0000000000 65535 f \n")
	for _, off := range offsets[:3] {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", start)
	return b.Bytes()
}

// TestForkKeepsItsEditsToItself pins that what a writer does to a fork, to
// its trailer, to an object it resolved, to its caches or to its logger, never
// reaches the parent, and that the fork starts from the index the parent
// holds, augmentation included.
func TestForkKeepsItsEditsToItself(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := unlistedObjectPDF()
	d, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if err := d.AugmentXRef(ctx); err != nil {
		t.Fatalf("AugmentXRef: %v", err)
	}
	page := ast.PdfIndirectRef{ObjectNum: 3}
	parentPage, err := ResolveDict(ctx, d, page)
	if err != nil {
		t.Fatalf("resolving the page: %v", err)
	}

	f := d.Fork()

	if !f.XRefAugmented() {
		t.Error("the fork lost the augmentation the parent ran")
	}
	if _, ok := f.XRefEntry(4); !ok {
		t.Error("the fork lost the object the augmentation added")
	}
	if got, want := f.OpenedXRefEntryCount(), d.OpenedXRefEntryCount(); got != want {
		t.Errorf("the fork opened %d entries, the parent %d", got, want)
	}
	if got, want := f.HighestObjectNumber(ctx), d.HighestObjectNumber(ctx); got != want {
		t.Errorf("HighestObjectNumber: fork %d, parent %d", got, want)
	}

	f.Index.Trailer["ID"] = ast.PdfArray{ast.PdfString("a"), ast.PdfString("a")}
	if _, ok := d.Index.Trailer["ID"]; ok {
		t.Error("an entry set in the fork's trailer reached the parent's")
	}
	if _, ok := f.Index.LastTrailer["ID"]; !ok {
		t.Error("the fork's one trailer came apart: its LastTrailer misses an edit of its Trailer")
	}

	forkPage, err := ResolveDict(ctx, f, page)
	if err != nil {
		t.Fatalf("resolving the page in the fork: %v", err)
	}
	forkPage["Rotate"] = ast.PdfInt(90)
	again, err := ResolveDict(ctx, d, page)
	if err != nil {
		t.Fatalf("resolving the page again: %v", err)
	}
	if _, ok := again["Rotate"]; ok {
		t.Error("an object edited in place in the fork changed in the parent")
	}
	if reflect.ValueOf(again).UnsafePointer() != reflect.ValueOf(parentPage).UnsafePointer() {
		t.Error("the parent's cache no longer holds the page it resolved")
	}

	f.ClearCaches()
	f.Logger = slog.New(slog.DiscardHandler)
	if d.Logger == f.Logger {
		t.Error("a logger set on the fork reached the parent")
	}
	cached, err := ResolveDict(ctx, d, page)
	if err != nil {
		t.Fatalf("resolving the page after the fork cleared its caches: %v", err)
	}
	if reflect.ValueOf(cached).UnsafePointer() != reflect.ValueOf(parentPage).UnsafePointer() {
		t.Error("clearing the fork's caches cleared the parent's")
	}
}

// TestForkBesideConcurrentReads forks a Document while other goroutines read
// it, and edits each fork, so -race sees every access Fork makes to the parent
// against the reads it is documented to be safe beside.
func TestForkBesideConcurrentReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := unlistedObjectPDF()
	d, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for n := 1; n <= 4; n++ {
				if _, err := d.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: n}); err != nil {
					t.Errorf("resolving object %d: %v", n, err)
				}
			}
			_ = d.HighestObjectNumber(ctx)
		})
		wg.Go(func() {
			f := d.Fork()
			f.Index.Trailer["ID"] = ast.PdfArray{}
			f.Logger = slog.New(slog.DiscardHandler)
			if _, err := f.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 4}); err != nil {
				t.Errorf("resolving the unlisted object in a fork: %v", err)
			}
			f.ClearCaches()
		})
	}
	wg.Wait()
	if _, ok := d.Index.Trailer["ID"]; ok {
		t.Error("a fork's trailer edit reached the parent")
	}
}
