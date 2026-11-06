package plaintext_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/plaintext"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/pdf"
)

var (
	fuzzFontsOnce sync.Once
	fuzzFonts     *text.Fonts
)

func FuzzConvert(f *testing.F) {
	f.Add([]byte("Hello world\n\tindented\r\nnext\fpage two\n"))
	f.Add([]byte("\xEF\xBB\xBF\xff\xfe\x00\r\r\n\f\f\t\x0b Á‮אב\xed\xa0\x80"))
	f.Fuzz(func(t *testing.T, data []byte) {
		// The index outlives the first iteration's directory: it holds the
		// bytes it read.
		fuzzFontsOnce.Do(func() { fuzzFonts = testFonts(t) })
		o := plaintext.Options{FontFamilies: []string{"Test Serif"}, MaxInputBytes: 1 << 14, MaxPages: 64}
		doc, err := plaintext.Convert(context.Background(), bytes.NewReader(data), fuzzFonts, o)
		if errors.Is(err, plaintext.ErrInputTooLarge) || errors.Is(err, plaintext.ErrTooManyPages) {
			return
		}
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		var b bytes.Buffer
		if err := compose.Write(context.Background(), doc, &b, compose.Options{}); err != nil {
			t.Fatalf("compose.Write: %v", err)
		}
		r, err := pdf.Open(context.Background(), bytes.NewReader(b.Bytes()), int64(b.Len()))
		if err != nil {
			t.Fatalf("pdf.Open: %v", err)
		}
		r.Close()
	})
}
