package pdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestConvert_ErrorNamesEachFactOnce locks the chains a [Conversion]
// returns: the sentinel's message appears once, the format is named once,
// by the conversion's own wrap, and the error is the caller's.
func TestConvert_ErrorNamesEachFactOnce(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	fonts := WithConvertFontDirs(dir)
	onePage := WithConvertLimits(Limits{MaxConvertPages: 1})
	for name, tc := range map[string]struct {
		format testFormat
		src    []byte
		opts   []ConvertOption
		sheets []string
		want   error
		label  string
	}{
		"text pages":     {format: formatText, src: []byte("A\fB"), opts: []ConvertOption{fonts, onePage}, want: ErrConvertTooManyPages, label: "text"},
		"HTML pages":     {format: formatHTML, src: []byte(`<p style="break-after: page">A<p>B`), opts: []ConvertOption{fonts, onePage}, want: ErrConvertTooManyPages, label: "HTML"},
		"DOCX pages":     {format: formatDOCX, src: longDOCX(t, 60), opts: []ConvertOption{fonts, onePage}, want: ErrConvertTooManyPages, label: "DOCX"},
		"XLSX pages":     {format: formatXLSX, src: longXLSX(t, 200), opts: []ConvertOption{fonts, onePage}, want: ErrConvertTooManyPages, label: "XLSX"},
		"DOCX input":     {format: formatDOCX, src: longDOCX(t, 1), opts: []ConvertOption{fonts, WithConvertLimits(Limits{MaxConvertInputBytes: 10})}, want: ErrConvertInputTooLarge, label: "DOCX"},
		"HTML input":     {format: formatHTML, src: []byte("<p>ABCD"), opts: []ConvertOption{fonts, WithConvertLimits(Limits{MaxConvertInputBytes: 4})}, want: ErrConvertInputTooLarge, label: "HTML"},
		"DOCX malformed": {format: formatDOCX, src: []byte("PK\x03\x04 not a package"), opts: []ConvertOption{fonts}, want: ErrConvertMalformed, label: "DOCX"},
		"PPTX no main":   {format: formatPPTX, src: noMainPart(t), opts: []ConvertOption{fonts}, want: ErrConvertMalformed, label: "PPTX"},
		"XLSX no main":   {format: formatXLSX, src: noMainPart(t), opts: []ConvertOption{fonts}, want: ErrConvertMalformed, label: "XLSX"},
		"XLSX no sheet":  {format: formatXLSX, src: longXLSX(t, 1), opts: []ConvertOption{fonts}, sheets: []string{"missing"}, want: ErrInvalidConvertOptions, label: "XLSX"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := fromFormat(tc.format, bytes.NewReader(tc.src), tc.opts...)
			if tc.sheets != nil {
				c = FromXLSX(bytes.NewReader(tc.src), append(optionsAs[SpreadsheetOption](tc.opts), WithSheets(tc.sheets...))...)
			}
			_, err := c.WriteTo(context.Background(), io.Discard)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error %v, want %v", err, tc.want)
			}
			if !IsInvalidInput(err) {
				t.Errorf("IsInvalidInput(%v) = false", err)
			}
			msg := err.Error()
			if n := strings.Count(msg, tc.want.Error()); n != 1 {
				t.Errorf("%q names %q %d times, want once", msg, tc.want.Error(), n)
			}
			if n := strings.Count(msg, tc.label); n != 1 {
				t.Errorf("%q names %s %d times, want once", msg, tc.label, n)
			}
		})
	}
}

// noMainPart returns an OOXML package whose officeDocument relationship
// names a part it does not hold.
func noMainPart(t *testing.T) []byte {
	t.Helper()
	return zipParts(t, map[string]string{
		"[Content_Types].xml": `<Types ` + ctNS + `>` + ctXML + `</Types>`,
		"_rels/.rels":         `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relDoc + `" Target="main.xml"/></Relationships>`,
	})
}
