package markdown

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/html"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

func TestParseTemplate_Check(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		tmpl string
		want string // "" accepts; else a fragment of the error
	}{
		"default":               {"", ""},
		"minimal":               {"{{ toHTML }}", ""},
		"title and stylesheet":  {"<style>p{color:red}</style><h1>{{ .Title }}</h1>{{ toHTML }}", ""},
		"inside a branch":       {"{{ if .Title }}{{ toHTML }}{{ else }}none{{ end }}", ""},
		"inside with":           {"{{ with .Title }}{{ . }}{{ end }}{{ with $t := toHTML }}{{ $t }}{{ end }}", ""},
		"through define":        {`{{ define "b" }}{{ toHTML }}{{ end }}<body>{{ template "b" . }}</body>`, ""},
		"through block":         {`<main>{{ block "main" . }}{{ toHTML }}{{ end }}</main>`, ""},
		"nested defines":        {`{{ define "a" }}{{ template "b" }}{{ end }}{{ define "b" }}{{ toHTML }}{{ end }}{{ template "a" }}`, ""},
		"twice":                 {"{{ toHTML }}{{ toHTML }}", ""},
		"root variable":         {"{{ $.Title }}{{ toHTML }}", ""},
		"comment":               {"{{/* c */}}{{ toHTML }}", ""},
		"does not parse":        {"{{ toHTML ", "parsing"},
		"unknown function":      {"{{ toPDF }}", "parsing"},
		"no toHTML":             {"<p>{{ .Title }}</p>", "never calls toHTML"},
		"only in unused define": {`{{ define "x" }}{{ toHTML }}{{ end }}<p>x</p>`, "never calls toHTML"},
		"only defines":          {`{{ define "x" }}{{ toHTML }}{{ end }}`, "never calls toHTML"},
		"whitespace":            {"  ", "never calls toHTML"},
		"builtin":               {`{{ printf "%s" .Title }}{{ toHTML }}`, `function "printf"`},
		"builtin in arg":        {`{{ toHTML | len }}`, `function "len"`},
		"other field":           {"{{ .Body }}{{ toHTML }}", "field .Body"},
		"deep field":            {"{{ .Title.X }}{{ toHTML }}", "field .Title.X"},
		"variable field":        {"{{ $.Other }}{{ toHTML }}", "field $.Other"},
		"range":                 {"{{ range 3 }}{{ toHTML }}{{ end }}", "range"},
		"recursion":             {`{{ define "r" }}{{ template "r" }}{{ end }}{{ template "r" }}{{ toHTML }}`, "calls itself"},
		"undefined template":    {`{{ template "nope" }}{{ toHTML }}`, "not defined"},
		"too long":              {strings.Repeat(" ", maxTemplateBytes) + "{{ toHTML }}", "longer than"},
	} {
		_, err := parseTemplate(tc.tmpl)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case tc.want != "" && (!errors.Is(err, html.ErrInvalidOptions) || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: %v, want an invalid option naming %q", name, err, tc.want)
		}
	}
}

func TestParseTemplate_ExpansionBound(t *testing.T) {
	t.Parallel()
	// 24 levels each calling the next twice expand to 2^24 calls: refused
	// before execution, which would otherwise run them all.
	var b strings.Builder
	for i := range 24 {
		b.WriteString(`{{ define "l` + string(rune('a'+i)) + `" }}{{ template "l` + string(rune('b'+i)) + `" }}{{ template "l` + string(rune('b'+i)) + `" }}{{ end }}`)
	}
	b.WriteString(`{{ define "l` + string(rune('a'+24)) + `" }}x{{ end }}{{ template "la" }}{{ toHTML }}`)
	if _, err := parseTemplate(b.String()); !errors.Is(err, html.ErrInvalidOptions) || !strings.Contains(err.Error(), "expands past") {
		t.Errorf("exponential expansion: %v", err)
	}
}

func TestRender(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	body, title := render([]byte("# A <b>&\n\ntext\n"))
	for name, tc := range map[string]struct{ tmpl, want string }{
		"title escaped":      {"<title>{{ .Title }}</title>{{ toHTML }}", "<title>A &amp;</title><h1>A <b>&amp;</h1>\n\n<p>text</p>\n"},
		"twice":              {"{{ toHTML }}|{{ toHTML }}", "<h1>A <b>&amp;</h1>\n\n<p>text</p>\n|<h1>A <b>&amp;</h1>\n\n<p>text</p>\n"},
		"in an attribute":    {`<p title="{{ toHTML }}"></p>`, "<p title=\"A &amp;\n\ntext\n\"></p>"},
		"title in attribute": {`<meta name="t" content="{{ .Title }}">{{ toHTML }}`, `<meta name="t" content="A &amp;"><h1>`},
	} {
		tmpl, err := parseTemplate(tc.tmpl)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, _, err := tmpl.render(ctx, body, title, 1<<20)
		if err != nil || !strings.HasPrefix(string(got), tc.want) {
			t.Errorf("%s: %q, %v; want %q", name, got, err, tc.want)
		}
	}
}

func TestRender_Fails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	body, title := render([]byte(strings.Repeat("word ", 100)))
	tmpl, err := parseTemplate("{{ toHTML }}{{ toHTML }}")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tmpl.render(ctx, body, title, int64(len(body))+10); !errors.Is(err, html.ErrInputTooLarge) {
		t.Errorf("past the cap: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := tmpl.render(cctx, body, title, 1<<20); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	// An escaping error is the caller's: a branch ends in another context.
	bad, err := parseTemplate(`{{ if .Title }}<a href="{{ end }}{{ toHTML }}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := bad.render(ctx, body, title, 1<<20); !errors.Is(err, html.ErrInvalidOptions) {
		t.Errorf("escaping error: %v", err)
	}
}

func TestRender_ChargesTheBudget(t *testing.T) {
	t.Parallel()
	ctx, budget, done := limits.WithBudget(context.Background(), limits.Limits{MaxConvertMemoryBytes: 64})
	defer done()
	body, title := render([]byte(strings.Repeat("word ", 100)))
	tmpl, err := parseTemplate("")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tmpl.render(ctx, body, title, 1<<20); !errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("past the budget: %v", err)
	}
	if budget.Held() != 0 {
		t.Errorf("held %d after a refusal", budget.Held())
	}
}

func TestConvert_Template(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := "# Report\n\nText.\n"
	doc, err := Convert(ctx, strings.NewReader(src), ahem(t), Options{
		HTML:     html.Options{PreferCSSPageSize: true},
		Template: `<!doctype html><html><head><title>Book: {{ .Title }}</title><style>@page { size: 200pt 300pt }</style></head><body>{{ toHTML }}</body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Metadata.Title != "Book: Report" {
		t.Errorf("title %q", doc.Metadata.Title)
	}
	if len(doc.Pages) != 1 || doc.Pages[0].Width != 200 || doc.Pages[0].Height != 300 {
		t.Errorf("pages %d, first %vx%v", len(doc.Pages), doc.Pages[0].Width, doc.Pages[0].Height)
	}
	// The template is checked before the source is read.
	if _, err := Convert(ctx, failingReader{}, ahem(t), Options{Template: "<p></p>"}); !errors.Is(err, html.ErrInvalidOptions) {
		t.Errorf("no toHTML: %v", err)
	}
}

// failingReader fails every read.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read") }

func FuzzTemplate(f *testing.F) {
	f.Add(DefaultTemplate)
	f.Add(DefaultTemplate[:len(DefaultTemplate)/2])
	body, title := render([]byte("# T\n\ntext\n"))
	f.Fuzz(func(t *testing.T, text string) {
		tmpl, err := parseTemplate(text)
		if err != nil {
			if !errors.Is(err, html.ErrInvalidOptions) {
				t.Fatalf("parse: %v", err)
			}
			return
		}
		_, _, err = tmpl.render(context.Background(), body, title, 1<<16)
		if err != nil && !errors.Is(err, html.ErrInvalidOptions) && !errors.Is(err, html.ErrInputTooLarge) {
			t.Fatalf("render: %v", err)
		}
	})
}
