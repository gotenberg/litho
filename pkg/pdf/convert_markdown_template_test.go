package pdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/markdown"
)

func TestDefaultMarkdownTemplate_IsTheConverterDefault(t *testing.T) {
	t.Parallel()
	if DefaultMarkdownTemplate != markdown.DefaultTemplate {
		t.Errorf("DefaultMarkdownTemplate\n%s\nwant\n%s", DefaultMarkdownTemplate, markdown.DefaultTemplate)
	}
}

func TestConvert_MarkdownTemplate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fonts := WithConvertFontDirs(convertFontDir(t))
	src := "# Notes\n\nA paragraph.\n"
	convert := func(opts ...MarkdownOption) ([]byte, error) {
		var out bytes.Buffer
		_, err := FromMarkdown(strings.NewReader(src), append([]MarkdownOption{fonts, WithDate(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))}, opts...)...).WriteTo(ctx, &out)
		return out.Bytes(), err
	}

	plain, err := convert()
	if err != nil {
		t.Fatal(err)
	}
	same, err := convert(WithMarkdownTemplate(DefaultMarkdownTemplate))
	if err != nil || !bytes.Equal(plain, same) {
		t.Errorf("the default template given prints otherwise: %v", err)
	}

	data, err := convert(WithMarkdownTemplate(`<!doctype html><html><head><title>Book: {{ .Title }}</title>` +
		`<style>@page { size: 300pt 400pt; margin: 20pt } p { color: red }</style></head><body>{{ toHTML }}</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if meta, err := r.Metadata(ctx); err != nil || meta["Title"] != "Book: Notes" {
		t.Errorf("metadata %v, %v", meta, err)
	}
	if p, err := r.Page(ctx, 1); err != nil || p.Width != 300 || p.Height != 400 {
		t.Errorf("page %+v, %v", p, err)
	}
	text, err := r.ExtractText(ctx, 1)
	if err != nil || !strings.Contains(text, "A paragraph.") {
		t.Errorf("text %q, %v", text, err)
	}

	for name, tmpl := range map[string]string{
		"does not parse": "{{ toHTML",
		"no toHTML":      "<p>{{ .Title }}</p>",
		"builtin":        `{{ printf "%s" .Title }}{{ toHTML }}`,
	} {
		_, err := convert(WithMarkdownTemplate(tmpl))
		if !errors.Is(err, ErrInvalidConvertOptions) || !IsInvalidInput(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
