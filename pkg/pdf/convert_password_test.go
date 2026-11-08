package pdf

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConvert_DOCXPassword converts Gotenberg's password-protected DOCX
// document, whose password is foo, and refuses it without the password or
// with another one.
func TestConvert_DOCXPassword(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "test", "integration", "corpus", "gotenberg", "docx", "sources", "protected_page_1", "protected_page_1.docx")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("protected_page_1.docx is not in the docx corpus sources")
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		opts []ConvertOption
		want error
	}{
		{"right password", []ConvertOption{WithConvertPassword("foo")}, nil},
		{"wrong password", []ConvertOption{WithConvertPassword("bar")}, ErrConvertEncrypted},
		{"no password", nil, ErrConvertEncrypted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			var out bytes.Buffer
			opts := append([]ConvertOption{WithConvertFontDirs(convertFontDir(t))}, tc.opts...)
			rep, err := Convert(ctx, bytes.NewReader(data), FormatDOCX, &out, opts...)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Convert = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if out.Len() != 0 {
					t.Errorf("%d bytes written on a refusal", out.Len())
				}
				return
			}
			if rep.Pages != 1 {
				t.Errorf("%d pages, want 1", rep.Pages)
			}
			pdf := out.Bytes()
			r, err := Open(ctx, bytes.NewReader(pdf), int64(len(pdf)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			got, err := r.ExtractText(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, "Page 1") {
				t.Errorf("text %q lacks Page 1", got)
			}
		})
	}
}
