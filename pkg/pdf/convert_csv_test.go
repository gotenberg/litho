package pdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// TestConvert_CSVSeparators checks that the fields of a CSV source end at
// the separators asked for, a comma alone by default, and that a
// separator inside quotes is text.
func TestConvert_CSVSeparators(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	src := "a;b,c\n\"x;y\";z\n"
	tests := []struct {
		name string
		opts []CSVOption
		// want are the fields of the page's text, the footer an
		// artifact.
		want []string
	}{
		// The quote that a semicolon follows does not close the field
		// under a comma alone: it is text, and so is the opening one.
		{"comma", nil, []string{"a;b", "c", "\"x;y\";z"}},
		{"semicolon", []CSVOption{WithCSVSeparators(';')}, []string{"a", "b,c", "x;y", "z"}},
		{"both", []CSVOption{WithCSVSeparators(',', ';')}, []string{"a", "b", "c", "x;y", "z"}},
	}
	for _, tt := range tests {
		pages, _ := convertedPages(t, FromCSV(strings.NewReader(src), append([]CSVOption{WithConvertFontDirs(dir)}, tt.opts...)...))
		if len(pages) != 1 || strings.Join(tt.want, " ") != pages[0] {
			t.Errorf("%s: %q, want %q", tt.name, pages, tt.want)
		}
	}
	for _, r := range []rune{'"', '\n', '\r', '�'} {
		_, err := FromCSV(strings.NewReader(src), WithConvertFontDirs(dir), WithCSVSeparators(r)).WriteTo(context.Background(), &bytes.Buffer{})
		if !errors.Is(err, ErrInvalidConvertOptions) {
			t.Errorf("separator %q: err = %v, want ErrInvalidConvertOptions", r, err)
		}
	}
}

// TestConvert_CSVTypeDetection checks that a field that reads as a number
// is laid out as one by default and as the file's text without detection.
func TestConvert_CSVTypeDetection(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	src := "zip,code\n007,1e3\n"
	for detect, want := range map[bool]string{true: "zip code 7 1.00E+03", false: "zip code 007 1e3"} {
		pages, _ := convertedPages(t, FromCSV(strings.NewReader(src), WithConvertFontDirs(dir), WithCSVTypeDetection(detect)))
		if len(pages) != 1 || pages[0] != want {
			t.Errorf("detection %v: %q, want %q", detect, pages, want)
		}
	}
}
