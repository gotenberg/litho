package image

import (
	"context"
	"log/slog"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolveKeyErrorsNameTheKeyOnce pins that a missing or mistyped key is
// named with one slash: [ast.PdfName] prints its own.
func TestResolveKeyErrorsNameTheKeyOnce(t *testing.T) {
	t.Parallel()

	doc := &document.Document{Logger: slog.New(slog.DiscardHandler)}
	dict := ast.PdfDict{"BitsPerComponent": ast.PdfName("Eight")}
	cases := []struct {
		name string
		err  func() error
		want string
	}{
		{"missing int", func() error { _, err := resolveInt(context.Background(), doc, dict, "Width"); return err }, "missing key /Width"},
		{"missing name", func() error { _, err := resolveName(context.Background(), doc, dict, "Filter"); return err }, "missing key /Filter"},
		{"mistyped int", func() error {
			_, err := resolveInt(context.Background(), doc, dict, "BitsPerComponent")
			return err
		}, "key /BitsPerComponent: "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.err()
			if err == nil {
				t.Fatal("want an error")
			}
			if got := err.Error(); len(got) < len(tc.want) || got[:len(tc.want)] != tc.want {
				t.Fatalf("error = %q, want prefix %q", got, tc.want)
			}
		})
	}
}
