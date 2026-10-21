package glyph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// TestCacheRetriesAFailedProvider keeps a provider's failure out of the
// cache: the next request asks again and draws with what it supplies, while an
// object that is no font stays refused.
func TestCacheRetriesAFailedProvider(t *testing.T) {
	t.Parallel()
	pdf := "%PDF-1.7\n1 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Melior /FirstChar 65 /LastChar 65 /Widths [600] >>\nendobj\n" +
		"2 0 obj\n42\nendobj\n"
	off1 := len("%PDF-1.7\n")
	off2 := strings.Index(pdf, "2 0 obj")
	xref := len(pdf)
	pdf += fmt.Sprintf("xref\n0 3\n0000000000 65535 f\r\n%010d 00000 n\r\n%010d 00000 n\r\ntrailer\n<< /Size 3 >>\nstartxref\n%d\n%%%%EOF\n", off1, off2, xref)
	doc, err := document.NewDocument(context.Background(), strings.NewReader(pdf), int64(len(pdf)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	program := fontbuild.TrueType{PostScriptName: "Sub", Family: "Sub", Subfamily: "Regular", Glyphs: glyphs}.Build()
	var calls atomic.Int32
	provider := func(context.Context, Request) ([]Candidate, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("transient")
		}
		return []Candidate{func(context.Context) ([]byte, error) { return program, nil }}, nil
	}
	cache := NewCache(doc, Options{Substitutes: provider})
	ref := ast.PdfIndirectRef{ObjectNum: 1}
	f, err := cache.Font(context.Background(), ref)
	if err == nil || f == nil || f.Source() != SourcePlaceholder {
		t.Fatalf("first load: font %v, err %v; want a placeholder font and the provider's error", f, err)
	}
	f, err = cache.Font(context.Background(), ref)
	if err != nil || f == nil || f.Source() != SourceSubstitute {
		t.Fatalf("second load: font %v, err %v; want the substitute", f, err)
	}
	asked := calls.Load()
	if again, _ := cache.Font(context.Background(), ref); again != f || calls.Load() != asked {
		t.Errorf("third load asked the provider again (%d calls), want the cached font", calls.Load())
	}
	if _, err := cache.Font(context.Background(), ast.PdfIndirectRef{ObjectNum: 2}); !errors.Is(err, ErrNotAFont) {
		t.Errorf("an integer loaded as a font: %v", err)
	}
	if _, ok := cache.fonts[2]; !ok {
		t.Error("an object that is no font was not cached")
	}
}
