package markdown

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	md "github.com/gomarkdown/markdown"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/html"
	"github.com/gotenberg/litho/pkg/internal/text"
)

var (
	ahemOnce  sync.Once
	ahemFonts *text.Fonts
	ahemErr   error
)

// ahem returns an index of the Ahem font, the one font in git.
func ahem(t testing.TB) *text.Fonts {
	t.Helper()
	ahemOnce.Do(func() {
		ahemFonts, ahemErr = text.NewFonts(context.Background(), nil, []string{"../../layout/testdata"})
	})
	if ahemErr != nil {
		t.Fatalf("indexing Ahem: %v", ahemErr)
	}
	return ahemFonts
}

// samples are Markdown documents covering the common extensions.
var samples = []string{
	"",
	"# Title\n\nSome *emphasis* and **strong** text.\n",
	"Setext\n======\n\n- a\n- b\n\n1. one\n2. two\n",
	"| a | b |\n|---|---|\n| 1 | 2 |\n",
	"```go\nfunc main() {}\n```\n\n> quote\n\n---\n\n~~gone~~ https://example.com\n",
	"Term\n: Definition\n\nNote[^1].\n\n[^1]: The note.\n",
	"<div style=\"color: red\">raw <b>HTML</b></div>\n\n![alt](img.png \"t\") [link](page.html)\n",
}

func TestToHTML_WrapsTheRendererOutput(t *testing.T) {
	t.Parallel()
	for _, src := range samples {
		got := ToHTML([]byte(src))
		body := md.ToHTML([]byte(src), nil, nil)
		head, rest, ok := bytes.Cut(got, []byte("<body>"))
		if !ok || !bytes.HasPrefix(head, []byte(`<!doctype html><html><head><meta charset="utf-8"><title>`)) {
			t.Errorf("%q: wrapper %q", src, head)
			continue
		}
		if want := append(append([]byte{}, body...), "</body></html>"...); !bytes.Equal(rest, want) {
			t.Errorf("%q: body\n%s\nwant\n%s", src, rest, want)
		}
	}
}

func TestFirstHeading(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]string{
		"":                         "",
		"no heading\n":             "",
		"# A *b* `c`\n## second\n": "A b c",
		// Raw HTML is markup, not text.
		"para\n\nSetext <x> & y\n---\n":  "Setext & y",
		"#   spaced    out   \n":         "spaced out",
		"## [Link](u) title\n# later\n":  "Link title",
		"# " + strings.Repeat("x", 9000): strings.Repeat("x", maxTitleBytes),
		"# caf\xc3\xa9 \xff\n":           "café �",
	} {
		if got := firstHeading(md.Parse([]byte(src), nil)); got != want {
			t.Errorf("firstHeading(%.40q) = %.40q, want %.40q", src, got, want)
		}
	}
	if got := ToHTML([]byte("# 1 < 2 & \"3\"\n")); !bytes.Contains(got, []byte("<title>1 &lt; 2 &amp; &#34;3&#34;</title>")) {
		t.Errorf("title not escaped: %s", got)
	}
}

func TestConvert_LaysOutTheMarkdown(t *testing.T) {
	t.Parallel()
	var pic bytes.Buffer
	if err := png.Encode(&pic, image.NewGray(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{"img/a.png": {Data: pic.Bytes()}}
	src := "# Report\n\nText with ![a picture](img/a.png) and [a link](https://example.com/x).\n\n## Next\n"
	doc, err := Convert(context.Background(), strings.NewReader(src), ahem(t), html.Options{Resources: fsys})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Metadata.Title != "Report" {
		t.Errorf("title %q", doc.Metadata.Title)
	}
	if len(doc.Warnings) != 0 {
		t.Errorf("warnings %+v", doc.Warnings)
	}
	images, links := 0, 0
	for _, p := range doc.Pages {
		links += len(p.Links)
		var walk func([]compose.Fragment)
		walk = func(fs []compose.Fragment) {
			for _, f := range fs {
				switch f := f.(type) {
				case *compose.Image:
					images++
				case *compose.Group:
					walk(f.Content)
				}
			}
		}
		walk(p.Content)
	}
	if images != 1 || links != 1 {
		t.Errorf("%d images and %d links, want 1 and 1", images, links)
	}
	if len(doc.Outline) != 1 || doc.Outline[0].Title != "Report" || len(doc.Outline[0].Kids) != 1 {
		t.Errorf("outline %+v", doc.Outline)
	}
}

func TestConvert_Caps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, err := Convert(ctx, strings.NewReader("# 12345"), ahem(t), html.Options{MaxInputBytes: 4}); !errors.Is(err, html.ErrInputTooLarge) {
		t.Errorf("past the cap: %v", err)
	}
	if _, err := Convert(ctx, strings.NewReader("x"), ahem(t), html.Options{MaxInputBytes: -1}); !errors.Is(err, html.ErrInvalidOptions) {
		t.Errorf("negative cap: %v", err)
	}
	// The rendered HTML is larger than the Markdown under its own cap.
	if _, err := Convert(ctx, strings.NewReader("* a\n* b\n"), ahem(t), html.Options{MaxInputBytes: 8}); err != nil {
		t.Errorf("rendered past the Markdown cap: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Convert(cctx, strings.NewReader("x"), ahem(t), html.Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	if _, err := Convert(ctx, strings.NewReader("x"), ahem(t), html.Options{Scale: 9}); !errors.Is(err, html.ErrInvalidOptions) {
		t.Errorf("bad options: %v", err)
	}
}

func FuzzConvert(f *testing.F) {
	for _, s := range samples {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		page := ToHTML(data)
		if !bytes.HasPrefix(page, []byte("<!doctype html>")) || !bytes.HasSuffix(page, []byte("</body></html>")) {
			t.Fatalf("wrapper broken: %q", page)
		}
		_, err := Convert(context.Background(), bytes.NewReader(data), ahem(t), html.Options{MaxInputBytes: 1 << 12, MaxPages: 8})
		if err != nil && !errors.Is(err, html.ErrInputTooLarge) && !errors.Is(err, html.ErrTooManyPages) &&
			!errors.Is(err, html.ErrTooManyElements) && !errors.Is(err, html.ErrTooDeep) {
			t.Fatalf("Convert: %v", err)
		}
	})
}
