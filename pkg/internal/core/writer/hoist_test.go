package writer

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// Payloads of the streams [directStreamDoc] writes inline, each distinct so a
// test can find every one again in the output.
const (
	formPayload     = "0 0 10 10 re f"
	metadataPayload = "<x:xmpmeta/>"
	contentPayload  = "q /Fm0 Do Q"
	trailerPayload  = "a stream in the trailer"
)

// directStreamDoc queues a one-page document holding a stream in every place
// a producer or a resolved reference can leave one inline: a form XObject in
// a resource dictionary, a stream in that form's own dictionary, a content
// stream in a /Contents array, and a stream in the trailer. It returns the
// allocator, the trailer and the page dictionary it queued.
func directStreamDoc() (*ObjAllocator, ast.PdfDict, ast.PdfDict) {
	alloc := NewObjAllocator(0)
	catalogRef := alloc.Alloc(ast.PdfNull{})
	pagesRef := alloc.Alloc(ast.PdfNull{})
	metadata := ast.NewStream(ast.PdfDict{"Type": ast.PdfName("Metadata"), "Subtype": ast.PdfName("XML")}, []byte(metadataPayload))
	form := ast.NewStream(ast.PdfDict{
		"Type":     ast.PdfName("XObject"),
		"Subtype":  ast.PdfName("Form"),
		"BBox":     ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(10), ast.PdfInt(10)},
		"Metadata": metadata,
	}, []byte(formPayload))
	page := ast.PdfDict{
		"Type":      ast.PdfName("Page"),
		"Parent":    pagesRef,
		"MediaBox":  ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
		"Resources": ast.PdfDict{"XObject": ast.PdfDict{"Fm0": form}},
		"Contents":  ast.PdfArray{ast.NewStream(ast.PdfDict{}, []byte(contentPayload))},
	}
	pageRef := alloc.Alloc(page)
	alloc.AllocAt(pagesRef.ObjectNum, ast.PdfDict{
		"Type": ast.PdfName("Pages"), "Kids": ast.PdfArray{pageRef}, "Count": ast.PdfInt(1),
	})
	alloc.AllocAt(catalogRef.ObjectNum, ast.PdfDict{"Type": ast.PdfName("Catalog"), "Pages": pagesRef})
	trailer := ast.PdfDict{
		"Root":  catalogRef,
		"Extra": ast.NewStream(ast.PdfDict{}, []byte(trailerPayload)),
	}
	return alloc, trailer, page
}

// countDirectStreams counts the streams nested inside obj. top reports whether
// obj is the value of an indirect object, where a stream is not nested.
func countDirectStreams(obj ast.PdfObject, top bool) int {
	n := 0
	switch v := obj.(type) {
	case ast.PdfStream:
		if !top {
			n++
		}
		for _, e := range v.Dict {
			n += countDirectStreams(e, false)
		}
	case ast.PdfDict:
		for _, e := range v {
			n += countDirectStreams(e, false)
		}
	case ast.PdfArray:
		for _, e := range v {
			n += countDirectStreams(e, false)
		}
	}
	return n
}

// readBack opens data and returns how many streams it holds inside another
// object, counting its trailer, and the raw data of every stream it holds as
// an indirect object.
func readBack(t *testing.T, data []byte) (int, map[string]bool) {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	direct := countDirectStreams(doc.Index.Trailer, false)
	payloads := make(map[string]bool)
	for num, entry := range doc.XRefEntries() {
		if entry.Free || num == 0 {
			continue
		}
		gen := entry.Generation
		if entry.ObjStmNum != 0 {
			gen = 0
		}
		obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: num, GenNum: gen})
		if err != nil {
			t.Fatalf("resolving object %d of the output: %v", num, err)
		}
		direct += countDirectStreams(obj, true)
		if stream, ok := obj.(ast.PdfStream); ok {
			raw, err := stream.Bytes(ctx)
			if err != nil {
				t.Fatalf("reading stream %d of the output: %v", num, err)
			}
			payloads[string(raw)] = true
		}
	}
	return direct, payloads
}

// assertEveryStreamIndirect fails unless data holds no stream inside another
// object and holds each of [directStreamDoc]'s streams as an object of its own.
func assertEveryStreamIndirect(t *testing.T, data []byte) {
	t.Helper()
	direct, payloads := readBack(t, data)
	if direct != 0 {
		t.Errorf("the output holds %d streams inside another object; ISO 32000-1, 7.3.8.1 makes every stream indirect", direct)
	}
	for _, want := range []string{formPayload, metadataPayload, contentPayload, trailerPayload} {
		if !payloads[want] {
			t.Errorf("no indirect stream of the output holds %q", want)
		}
	}
}

// TestHoistDirectStreams_MovesEveryNestedStream pins the walk itself: every
// stream below the top of a pending object, or in the trailer, becomes an
// object of its own, a stream nested in a moved stream's dictionary included,
// and the containers the allocator was given are left as they were.
func TestHoistDirectStreams_MovesEveryNestedStream(t *testing.T) {
	t.Parallel()
	alloc, trailer, page := directStreamDoc()

	moved, err := HoistDirectStreams(context.Background(), alloc, trailer)
	if err != nil {
		t.Fatalf("HoistDirectStreams: %v", err)
	}
	if moved != 4 {
		t.Errorf("moved %d streams, want 4: the form, its metadata, the content stream and the trailer's", moved)
	}
	for num, obj := range alloc.Pending() {
		if n := countDirectStreams(obj, true); n != 0 {
			t.Errorf("object %d still holds %d streams inside it", num, n)
		}
	}
	if n := countDirectStreams(trailer, false); n != 0 {
		t.Errorf("the trailer still holds %d streams", n)
	}
	if _, isRef := trailer["Extra"].(ast.PdfIndirectRef); !isRef {
		t.Errorf("trailer /Extra is %T, want a reference to the moved stream", trailer["Extra"])
	}
	// The page the caller built still holds its streams: the walk copied the
	// containers on the path instead of writing into them.
	if n := countDirectStreams(page, true); n != 3 {
		t.Errorf("the page dictionary the caller holds lost streams to the walk: %d left, want 3", n)
	}

	again, err := HoistDirectStreams(context.Background(), alloc, trailer)
	if err != nil || again != 0 {
		t.Errorf("a second walk moved %d streams (err %v), want 0", again, err)
	}
}

// TestHoistDirectStreams_Canceled pins that a done ctx stops the walk with an
// error rather than letting a write go on half moved.
func TestHoistDirectStreams_Canceled(t *testing.T) {
	t.Parallel()
	alloc, trailer, _ := directStreamDoc()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := HoistDirectStreams(ctx, alloc, trailer); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestSerialize_RefusesDirectStream pins the serializer's backstop: a stream
// inside a dictionary or an array has no conforming spelling, so a writer path
// that skipped [HoistDirectStreams] fails instead of writing it.
func TestSerialize_RefusesDirectStream(t *testing.T) {
	t.Parallel()
	stream := ast.NewStream(ast.PdfDict{}, []byte("data"))
	for name, obj := range map[string]ast.PdfObject{
		"dictionary":        ast.PdfDict{"S": stream},
		"array":             ast.PdfArray{ast.PdfInt(1), stream},
		"stream dictionary": ast.NewStream(ast.PdfDict{"DP": stream}, []byte("outer")),
	} {
		if _, err := Serialize(context.Background(), &bytes.Buffer{}, obj); !errors.Is(err, ErrDirectStream) {
			t.Errorf("%s: err = %v, want ErrDirectStream", name, err)
		}
	}
	if _, err := Serialize(context.Background(), &bytes.Buffer{}, stream); err != nil {
		t.Errorf("a stream at the top of the value is where it belongs, got %v", err)
	}
}

// TestWriteRewrite_WritesEveryStreamIndirect pins the full rewrite on each of
// its output forms: every stream goes out as an object of its own, and one an
// object stream would otherwise carry stays out of it (ISO 32000-1, 7.5.7).
func TestWriteRewrite_WritesEveryStreamIndirect(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]RewriteConfig{
		"table":            {},
		"xref stream":      {UseXRefStream: true},
		"object streams":   {PackObjectStreams: true},
		"objstm and dedup": {PackObjectStreams: true, DedupStreams: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			alloc, trailer, _ := directStreamDoc()
			// Enough dictionaries that packing does pack, so the test cannot
			// pass because nothing reached an object stream.
			substantialDicts(alloc, 12)
			cfg.Allocator, cfg.Trailer = alloc, trailer
			var out bytes.Buffer
			if err := WriteRewrite(context.Background(), &out, cfg); err != nil {
				t.Fatalf("WriteRewrite: %v", err)
			}
			if cfg.PackObjectStreams && !bytes.Contains(out.Bytes(), []byte("/ObjStm")) {
				t.Fatal("nothing was packed, so the test proves nothing about object streams")
			}
			assertEveryStreamIndirect(t, out.Bytes())
		})
	}
}

// TestWriteRewrite_DirectStreamNumberingIsDeterministic pins that the numbers
// the moved streams take do not depend on map order: two rewrites of the same
// objects write the same bytes.
func TestWriteRewrite_DirectStreamNumberingIsDeterministic(t *testing.T) {
	t.Parallel()
	write := func() []byte {
		alloc, trailer, _ := directStreamDoc()
		var out bytes.Buffer
		if err := WriteRewrite(context.Background(), &out, RewriteConfig{Allocator: alloc, Trailer: trailer}); err != nil {
			t.Fatalf("WriteRewrite: %v", err)
		}
		return out.Bytes()
	}
	first := write()
	for range 5 {
		if !bytes.Equal(first, write()) {
			t.Fatal("two rewrites of the same objects wrote different bytes")
		}
	}
}

// TestWriteRewrite_RefusesDirectStreamWhenEncrypted pins that an encrypted
// rewrite never moves a stream itself: encryption already keyed its data to
// the object holding it, so a moved stream would decrypt to noise. The
// encryption step moves them first; one it did not reach fails the write.
func TestWriteRewrite_RefusesDirectStreamWhenEncrypted(t *testing.T) {
	t.Parallel()
	alloc, trailer, _ := directStreamDoc()
	delete(trailer, "Extra")
	trailer["Encrypt"] = alloc.Alloc(ast.PdfDict{"Filter": ast.PdfName("Standard")})
	err := WriteRewrite(context.Background(), &bytes.Buffer{}, RewriteConfig{Allocator: alloc, Trailer: trailer})
	if !errors.Is(err, ErrDirectStream) {
		t.Fatalf("err = %v, want ErrDirectStream", err)
	}
}

// TestWriteIncremental_WritesEveryStreamIndirect pins the incremental update: a
// replacement object holding a stream inline, and a stream in the trailer the
// update carries, go out as objects of their own.
func TestWriteIncremental_WritesEveryStreamIndirect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	srcAlloc, srcTrailer, _ := directStreamDoc()
	var src bytes.Buffer
	if err := WriteRewrite(ctx, &src, RewriteConfig{Allocator: srcAlloc, Trailer: srcTrailer}); err != nil {
		t.Fatalf("writing the source: %v", err)
	}
	doc, err := document.NewDocument(ctx, bytes.NewReader(src.Bytes()), int64(src.Len()), nil)
	if err != nil {
		t.Fatalf("opening the source: %v", err)
	}
	startXRef, err := document.FindStartXRef(doc.RawReader(), doc.FileSize())
	if err != nil {
		t.Fatalf("FindStartXRef: %v", err)
	}

	// The update rewrites the page and the trailer with fresh inline streams,
	// numbered in the source's own namespace.
	_, freshTrailer, page := directStreamDoc()
	alloc := NewObjAllocator(doc.HighestObjectNumber(ctx))
	alloc.AllocAt(3, page)
	trailer := ast.PdfDict{"Root": doc.Index.Trailer["Root"], "Extra": freshTrailer["Extra"]}

	var out bytes.Buffer
	if err := WriteIncremental(ctx, &out, IncrementalConfig{
		Source:          doc.RawReader(),
		SourceSize:      doc.FileSize(),
		StartXRefOffset: startXRef,
		Allocator:       alloc,
		Trailer:         trailer,
	}); err != nil {
		t.Fatalf("WriteIncremental: %v", err)
	}
	if _, isStream := trailer["Extra"].(ast.PdfStream); !isStream {
		t.Error("WriteIncremental changed the caller's trailer map; it writes a copy")
	}
	assertEveryStreamIndirect(t, out.Bytes())
}
