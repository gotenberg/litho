package pdf

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"testing"
)

func TestRecoverToError(t *testing.T) {
	t.Run("panic converts to ErrInternalPanic", func(t *testing.T) {
		var err error
		func() {
			defer recoverToError(nil, "test", &err)
			panic("boom")
		}()
		if !errors.Is(err, ErrInternalPanic) {
			t.Fatalf("got %v, want ErrInternalPanic", err)
		}
	})

	t.Run("no panic leaves err nil", func(t *testing.T) {
		var err error
		func() {
			defer recoverToError(nil, "test", &err)
		}()
		if err != nil {
			t.Errorf("got %v, want nil", err)
		}
	})

	t.Run("does not overwrite an error already in flight", func(t *testing.T) {
		sentinel := errors.New("original")
		err := sentinel
		func() {
			defer recoverToError(nil, "test", &err)
			panic("boom")
		}()
		if !errors.Is(err, sentinel) {
			t.Errorf("got %v, want the original error preserved", err)
		}
	})
}

// panicSigner panics in every method to exercise the public-API panic
// boundary deterministically.
type panicSigner struct{}

func (panicSigner) Sign([]byte) ([]byte, error)    { panic("signer blew up") }
func (panicSigner) Certificate() *x509.Certificate { panic("signer blew up") }
func (panicSigner) EstimatedSize() int             { panic("signer blew up") }

// TestWriter_PanicBoundary confirms the boundary is wired on a public
// method: a panic raised deep inside WriteTo (here from a misbehaving
// Signer) surfaces as ErrInternalPanic instead of crashing the process.
func TestWriter_PanicBoundary(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	w.Sign(panicSigner{})

	var out bytes.Buffer
	err = w.WriteTo(ctx, &out)
	if !errors.Is(err, ErrInternalPanic) {
		t.Fatalf("WriteTo with a panicking signer: got %v, want ErrInternalPanic", err)
	}
}
