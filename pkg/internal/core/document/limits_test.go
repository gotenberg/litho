package document

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// limitsDocBytes assembles a document of objs, numbered from 1, the first
// being the catalog.
func limitsDocBytes(objs ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

// TestNewDocument_ReadsItsLimits pins that the budgets [WithLimits] sets reach
// the object cache and the document's stream decodes, and that a Document
// opened without it, or never opened, reads under the defaults.
func TestNewDocument_ReadsItsLimits(t *testing.T) {
	t.Parallel()

	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	if _, err := w.Write(make([]byte, 4096)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", z.Len(), z.Bytes()),
	}
	for range 6 {
		objs = append(objs, "<< /Filler true >>")
	}
	data := limitsDocBytes(objs...)
	ctx := context.Background()

	var zero *Document
	if got := zero.Limits(); got != limits.Default() {
		t.Errorf("a nil Document reads under %+v, want the defaults", got)
	}
	def, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if got := def.Limits(); got != limits.Default() {
		t.Errorf("a Document opened without WithLimits reads under %+v, want the defaults", got)
	}

	lim := limits.Limits{MaxStreamBytes: 1024, MaxCachedObjects: 2}
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, WithLimits(lim))
	if err != nil {
		t.Fatalf("NewDocument with limits: %v", err)
	}
	if got := doc.Limits(); got != lim.WithDefaults() {
		t.Errorf("Limits() = %+v, want %+v", got, lim.WithDefaults())
	}
	for n := 1; n <= len(objs); n++ {
		if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: n}); err != nil {
			t.Fatalf("Resolve %d: %v", n, err)
		}
		doc.mu.RLock()
		cached := len(doc.objectCache)
		doc.mu.RUnlock()
		if cached > lim.MaxCachedObjects {
			t.Fatalf("after object %d the cache holds %d objects, past a budget of %d", n, cached, lim.MaxCachedObjects)
		}
	}

	stream, err := ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("ResolveStream: %v", err)
	}
	if out, err := def.DecodeStream(ctx, stream); err != nil || len(out) != 4096 {
		t.Errorf("default budget: %d bytes, %v; want 4096 bytes", len(out), err)
	}
	if _, err := doc.DecodeStream(ctx, stream); !errors.Is(err, filter.ErrDecompressionBomb) {
		t.Errorf("1 KiB budget: DecodeStream err = %v, want ErrDecompressionBomb", err)
	}
	if _, err := doc.NewStreamReader(ctx, stream); err != nil {
		t.Errorf("1 KiB budget: NewStreamReader of a single FlateDecode filter = %v, want the unbounded streaming read", err)
	}
}

// TestCacheObjStm_FollowsTheStreamBudget pins the object-stream cache to
// [limits.Limits.MaxObjStmCacheBytes] of the document's budgets: an object
// stream as large as the stream budget is cached, a larger one is not, and a
// stream that would take the cache past the budget empties it first. The
// default document caches a stream the lowered budget refuses.
func TestCacheObjStm_FollowsTheStreamBudget(t *testing.T) {
	t.Parallel()

	data := limitsDocBytes(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
	)
	ctx := context.Background()
	const budget = 100

	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, WithLimits(limits.Limits{MaxStreamBytes: budget}))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	cached := func(d *Document) []int {
		d.mu.RLock()
		defer d.mu.RUnlock()
		var nums []int
		for n := range d.objStmCache {
			nums = append(nums, n)
		}
		return nums
	}

	doc.cacheObjStm(1, &objStmData{decoded: make([]byte, budget+1)})
	if got := cached(doc); len(got) != 0 {
		t.Errorf("a stream past the %d-byte budget was cached: %v", budget, got)
	}
	doc.cacheObjStm(2, &objStmData{decoded: make([]byte, budget)})
	if got := cached(doc); len(got) != 1 || got[0] != 2 {
		t.Errorf("after a stream of exactly the budget the cache holds %v, want [2]", got)
	}
	doc.cacheObjStm(3, &objStmData{decoded: make([]byte, 1)})
	if got := cached(doc); len(got) != 1 || got[0] != 3 {
		t.Errorf("a stream past the remaining budget left the cache holding %v, want only [3]", got)
	}

	def, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	def.cacheObjStm(1, &objStmData{decoded: make([]byte, budget+1)})
	if got := cached(def); len(got) != 1 {
		t.Errorf("the default budget did not cache a %d-byte stream: %v", budget+1, got)
	}
}

// TestSetCachedFontProgram_FollowsTheStreamBudget pins the font-program cache
// to [limits.Limits.MaxFontProgCacheBytes] of the document's budgets, a
// quarter of the stream budget: a program as large as the bound is cached, a
// larger one is not, and a program that would take the cache past the bound
// empties it first. The default document caches a program the lowered budget
// refuses.
func TestSetCachedFontProgram_FollowsTheStreamBudget(t *testing.T) {
	t.Parallel()

	data := limitsDocBytes(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
	)
	ctx := context.Background()
	const budget, bound = 400, 100

	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, WithLimits(limits.Limits{MaxStreamBytes: budget}))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if got := doc.Limits().MaxFontProgCacheBytes(); got != bound {
		t.Fatalf("MaxFontProgCacheBytes() = %d under a %d-byte budget, want %d", got, budget, bound)
	}
	cached := func(d *Document, nums ...int) []int {
		var held []int
		for _, n := range nums {
			if _, ok := d.CachedFontProgram(n); ok {
				held = append(held, n)
			}
		}
		return held
	}

	doc.SetCachedFontProgram(1, make([]byte, bound+1))
	if got := cached(doc, 1); len(got) != 0 {
		t.Errorf("a program past the %d-byte bound was cached", bound)
	}
	doc.SetCachedFontProgram(2, make([]byte, bound))
	if got := cached(doc, 2); len(got) != 1 {
		t.Errorf("a program of exactly the bound was not cached")
	}
	doc.SetCachedFontProgram(3, make([]byte, 1))
	if got := cached(doc, 2, 3); len(got) != 1 || got[0] != 3 {
		t.Errorf("a program past the remaining bound left the cache holding %v, want only [3]", got)
	}

	def, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	def.SetCachedFontProgram(1, make([]byte, bound+1))
	if got := cached(def, 1); len(got) != 1 {
		t.Errorf("the default budget did not cache a %d-byte program", bound+1)
	}
}

// TestSetCachedColorTransform_FollowsTheStreamBudget pins the colour-transform
// cache to [limits.Limits.MaxColorCacheBytes] of the document's budgets, a
// quarter of the stream budget: a transform as large as the bound is cached, a
// larger one is not, and a transform that would take the cache past the bound
// empties it first. The default document caches a transform the lowered
// budget refuses.
func TestSetCachedColorTransform_FollowsTheStreamBudget(t *testing.T) {
	t.Parallel()

	data := limitsDocBytes(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
	)
	ctx := context.Background()
	const budget, bound, intent = 400, 100, 1

	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, WithLimits(limits.Limits{MaxStreamBytes: budget}))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if got := doc.Limits().MaxColorCacheBytes(); got != bound {
		t.Fatalf("MaxColorCacheBytes() = %d under a %d-byte budget, want %d", got, budget, bound)
	}
	cached := func(d *Document, nums ...int) []int {
		var held []int
		for _, n := range nums {
			if _, ok := d.CachedColorTransform(n, intent); ok {
				held = append(held, n)
			}
		}
		return held
	}

	doc.SetCachedColorTransform(1, intent, "t1", bound+1)
	if got := cached(doc, 1); len(got) != 0 {
		t.Errorf("a transform past the %d-byte bound was cached", bound)
	}
	doc.SetCachedColorTransform(2, intent, "t2", bound)
	if got := cached(doc, 2); len(got) != 1 {
		t.Errorf("a transform of exactly the bound was not cached")
	}
	doc.SetCachedColorTransform(3, intent, "t3", 1)
	if got := cached(doc, 2, 3); len(got) != 1 || got[0] != 3 {
		t.Errorf("a transform past the remaining bound left the cache holding %v, want only [3]", got)
	}

	def, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	def.SetCachedColorTransform(1, intent, "t1", bound+1)
	if got := cached(def, 1); len(got) != 1 {
		t.Errorf("the default budget did not cache a %d-byte transform", bound+1)
	}
}

// TestSetCachedFormContent_FollowsTheStreamBudget pins the form content cache
// to [limits.Limits.MaxFormCacheBytes] of the document's budgets, a quarter of
// the stream budget: content as large as the bound is cached, larger content
// is not, and content that would take the cache past the bound empties it
// first. Two variants of one form are held apart, [Document.ClearCaches] and
// [Document.Fork] start empty, and the default document caches content the
// lowered budget refuses.
func TestSetCachedFormContent_FollowsTheStreamBudget(t *testing.T) {
	t.Parallel()

	data := limitsDocBytes(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
	)
	ctx := context.Background()
	const budget, bound, variant = 400, 100, 0

	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, WithLimits(limits.Limits{MaxStreamBytes: budget}))
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if got := doc.Limits().MaxFormCacheBytes(); got != bound {
		t.Fatalf("MaxFormCacheBytes() = %d under a %d-byte budget, want %d", got, budget, bound)
	}
	cached := func(d *Document, nums ...int) []int {
		var held []int
		for _, n := range nums {
			if _, ok := d.CachedFormContent(n, variant); ok {
				held = append(held, n)
			}
		}
		return held
	}

	doc.SetCachedFormContent(1, variant, "f1", bound+1)
	if got := cached(doc, 1); len(got) != 0 {
		t.Errorf("content past the %d-byte bound was cached", bound)
	}
	doc.SetCachedFormContent(2, variant, "f2", bound)
	if got := cached(doc, 2); len(got) != 1 {
		t.Errorf("content of exactly the bound was not cached")
	}
	doc.SetCachedFormContent(3, variant, "f3", 1)
	if got := cached(doc, 2, 3); len(got) != 1 || got[0] != 3 {
		t.Errorf("content past the remaining bound left the cache holding %v, want only [3]", got)
	}
	if _, ok := doc.CachedFormContent(3, variant+1); ok {
		t.Errorf("another variant of form 3 read the cached content")
	}
	if got := cached(doc.Fork(), 3); len(got) != 0 {
		t.Errorf("a fork read the cached content of form 3")
	}
	doc.ClearCaches()
	if got := cached(doc, 3); len(got) != 0 {
		t.Errorf("ClearCaches left form 3 cached")
	}

	def, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	def.SetCachedFormContent(1, variant, "f1", bound+1)
	if got := cached(def, 1); len(got) != 1 {
		t.Errorf("the default budget did not cache %d bytes of content", bound+1)
	}
}
