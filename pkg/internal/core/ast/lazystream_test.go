package ast

import (
	"context"
	"errors"
	"testing"
)

// TestLazyStreamReadsThroughItsLoaderEachTime pins what [NewLazyStream]
// actually provides.
//
// Its doc used to promise the loader was "called at most once" and that
// subsequent calls returned a cached result. PdfStream is a value type with
// nowhere to memoise, so no caching happens here: three Bytes calls ran the
// loader three times. A caller who believed the old wording and passed an
// expensive loader would have paid for every access.
func TestLazyStreamReadsThroughItsLoaderEachTime(t *testing.T) {
	t.Parallel()

	calls := 0
	s := NewLazyStream(PdfDict{}, func(context.Context) ([]byte, error) {
		calls++
		return []byte("payload"), nil
	})

	ctx := context.Background()
	for i := range 3 {
		got, err := s.Bytes(ctx)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if string(got) != "payload" {
			t.Fatalf("call %d returned %q", i, got)
		}
	}
	if calls != 3 {
		t.Errorf("loader ran %d times for 3 reads; the constructor does not memoise, so it should run each time", calls)
	}
}

// TestLazyStreamHonoursAMemoisingLoader pins the other half of the contract:
// caching belongs to the loader, and a loader that memoises is called once
// however often the stream is read. This is the shape the parser uses, with a
// sync.Once around its read and decrypt.
func TestLazyStreamHonoursAMemoisingLoader(t *testing.T) {
	t.Parallel()

	calls := 0
	var cached []byte
	s := NewLazyStream(PdfDict{}, func(context.Context) ([]byte, error) {
		if cached == nil {
			calls++
			cached = []byte("read once")
		}
		return cached, nil
	})

	ctx := context.Background()
	for range 5 {
		if _, err := s.Bytes(ctx); err != nil {
			t.Fatalf("reading: %v", err)
		}
	}
	if calls != 1 {
		t.Errorf("a memoising loader ran %d times, want 1", calls)
	}
}

// TestLazyStreamPropagatesTheLoaderError pins that a failing read surfaces
// rather than yielding empty bytes, which a caller could mistake for an empty
// stream.
func TestLazyStreamPropagatesTheLoaderError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("read failed")
	s := NewLazyStream(PdfDict{}, func(context.Context) ([]byte, error) {
		return nil, sentinel
	})

	got, err := s.Bytes(context.Background())
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the loader's error", err)
	}
	if got != nil {
		t.Errorf("returned %d bytes alongside an error", len(got))
	}
}

// TestStreamWithNoLoaderReadsEmpty pins the zero value: a PdfStream that was
// never given data or a loader reads as empty without error, which is what the
// resolvers rely on when a dictionary has no stream behind it.
func TestStreamWithNoLoaderReadsEmpty(t *testing.T) {
	t.Parallel()

	var s PdfStream
	got, err := s.Bytes(context.Background())
	if err != nil {
		t.Fatalf("Bytes on a zero PdfStream: %v", err)
	}
	if got != nil {
		t.Errorf("read %d bytes from a stream with no loader", len(got))
	}
}

// TestNewStreamHoldsItsData is the control for the eager constructor: it
// already has the bytes, so repeated reads cost nothing and return the same
// data.
func TestNewStreamHoldsItsData(t *testing.T) {
	t.Parallel()

	s := NewStream(PdfDict{}, []byte("preloaded"))
	ctx := context.Background()
	for range 3 {
		got, err := s.Bytes(ctx)
		if err != nil {
			t.Fatalf("Bytes: %v", err)
		}
		if string(got) != "preloaded" {
			t.Errorf("got %q, want %q", got, "preloaded")
		}
	}
}
