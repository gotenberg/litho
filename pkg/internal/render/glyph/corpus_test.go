package glyph

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// fixture opens the litho corpus fixture name.
func fixture(t *testing.T, name string, opts ...document.Option) *document.Document {
	t.Helper()
	data, err := os.ReadFile("../../../../test/integration/corpus/litho/pdfs/" + name)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil, opts...)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// fixtureFont loads the font dictionary object num of doc through cache.
func fixtureFont(t *testing.T, cache *Cache, num int) *Font {
	t.Helper()
	f, err := cache.Font(context.Background(), ast.PdfIndirectRef{ObjectNum: num})
	if err != nil {
		t.Fatalf("loading font %d: %v", num, err)
	}
	return f
}

func TestCorpusCIDFontType0SelectsThroughTheCharset(t *testing.T) {
	t.Parallel()
	doc := fixture(t, "fontleft_2b.pdf")
	f := fixtureFont(t, NewCache(doc, Options{}), 6)
	p, ok := f.prog.(*cffProgram)
	if !ok || f.subtype != "CIDFontType0" || !p.font.CIDKeyed() {
		t.Fatalf("font 6 is %s over %T, want CIDFontType0 over a CID-keyed CFF program", f.subtype, f.prog)
	}
	inCharset, outside := 0, 0
	for cid := 1; cid < 1000; cid++ {
		g := glyphOf(t, f, byte(cid>>8), byte(cid))
		if g.Source != SourceEmbedded || g.Unreadable {
			t.Fatalf("CID %d: source %v, unreadable %v", cid, g.Source, g.Unreadable)
		}
		_, held := p.font.GlyphForCID(cid)
		if held == g.Notdef {
			t.Errorf("CID %d: charset holds it %v, notdef %v", cid, held, g.Notdef)
		}
		if held {
			inCharset++
		} else {
			outside++
		}
	}
	if inCharset == 0 || outside == 0 {
		t.Errorf("CIDs 1 to 999: %d in the charset, %d outside; the fixture no longer tells them apart", inCharset, outside)
	}
}

// TestCorpusCIDToGIDMapStream pins the fixture whose /CIDToGIDMap stream
// sends CIDs 1 and 2 to the program's glyph A (cmapcollect_1b.pdf).
func TestCorpusCIDToGIDMapStream(t *testing.T) {
	t.Parallel()
	doc := fixture(t, "cmapcollect_1b.pdf")
	f := fixtureFont(t, NewCache(doc, Options{}), 5)
	if f.cidToGID == nil {
		t.Fatal("font 5 has no /CIDToGIDMap stream")
	}
	one, two := f.cidToGID[1], f.cidToGID[2]
	if one == 0 || one != two {
		t.Fatalf("CIDs 1 and 2 map to glyphs %d and %d, want one glyph", one, two)
	}
	g1, g2 := glyphOf(t, f, 0, 1), glyphOf(t, f, 0, 2)
	b1, ok1 := g1.Outline.Bounds()
	b2, ok2 := g2.Outline.Bounds()
	if !ok1 || !ok2 || b1 != b2 {
		t.Errorf("CIDs 1 and 2 drew %v and %v, want the same glyph", b1, b2)
	}
}

func TestCorpusType3(t *testing.T) {
	t.Parallel()
	doc := fixture(t, "complex_2b.pdf")
	cache := NewCache(doc, Options{})
	f := fixtureFont(t, cache, 12)
	if f.Source() != SourceType3 {
		t.Fatalf("font 12 source %v, want type3", f.Source())
	}
	found := false
	for code := range 256 {
		g := glyphOf(t, f, byte(code))
		if g.CharProc != nil {
			found = true
			if g.Matrix.Det() == 0 {
				t.Errorf("code %d: singular glyph matrix", code)
			}
		}
	}
	if !found {
		t.Error("no code named a glyph description")
	}
}

func TestCacheSharesAndBoundsFonts(t *testing.T) {
	t.Parallel()
	doc := fixture(t, "fontclaims_2b.pdf")
	cache := NewCache(doc, Options{})
	a, b := fixtureFont(t, cache, 6), fixtureFont(t, cache, 6)
	if a != b {
		t.Error("the cache loaded font 6 twice")
	}

	// A stream budget of 4 bytes bounds the font program cache at 1 byte,
	// which no program fits: each request loads the font anew.
	small := fixture(t, "fontclaims_2b.pdf", document.WithLimits(limits.Limits{MaxStreamBytes: 4}))
	tight := NewCache(small, Options{})
	if fixtureFont(t, tight, 6) == fixtureFont(t, tight, 6) {
		t.Error("a font whose program passes the budget was kept")
	}
	if len(tight.fonts) != 0 || tight.bytes != 0 {
		t.Errorf("the cache holds %d fonts and %d bytes past its budget", len(tight.fonts), tight.bytes)
	}
}

func TestCacheConcurrentUse(t *testing.T) {
	t.Parallel()
	doc := fixture(t, "fontleft_2b.pdf")
	cache := NewCache(doc, Options{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for _, num := range []int{6, 7} {
				f, err := cache.Font(context.Background(), ast.PdfIndirectRef{ObjectNum: num})
				if err != nil {
					t.Error(err)
					return
				}
				for _, code := range f.Codes([]byte{0, 1, 0, 2, 0, 3, 0, 4}) {
					if _, err := f.Glyph(context.Background(), code); err != nil {
						t.Error(err)
					}
				}
			}
		})
	}
	wg.Wait()
}

func BenchmarkGlyph(b *testing.B) {
	data, err := os.ReadFile("../../../../test/integration/corpus/litho/pdfs/fontleft_2b.pdf")
	if err != nil {
		b.Fatal(err)
	}
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		b.Fatal(err)
	}
	f, err := NewCache(doc, Options{}).Font(context.Background(), ast.PdfIndirectRef{ObjectNum: 6})
	if err != nil {
		b.Fatal(err)
	}
	codes := f.Codes([]byte{0, 1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6})
	b.ReportAllocs()
	for b.Loop() {
		for _, code := range codes {
			if _, err := f.Glyph(context.Background(), code); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func TestCacheResource(t *testing.T) {
	t.Parallel()
	doc := fixture(t, "fontclaims_2b.pdf")
	cache := NewCache(doc, Options{})
	res := ast.PdfDict{"Font": ast.PdfDict{"F1": ast.PdfIndirectRef{ObjectNum: 6}}}
	f, err := cache.Resource(context.Background(), res, "F1")
	if err != nil || f != fixtureFont(t, cache, 6) {
		t.Errorf("Resource F1 = %p, %v; want the cached font 6", f, err)
	}
	if _, err := cache.Resource(context.Background(), res, "F2"); err == nil {
		t.Error("an undefined resource name loaded a font")
	}
}
