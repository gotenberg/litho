package html

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// pageText returns the characters of pg's glyph runs in paint order.
func pageText(pg *compose.Page) string {
	var b strings.Builder
	var walk func(fs []compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Group:
				walk(v.Content)
			case *compose.Text:
				for _, g := range v.Glyphs {
					b.WriteString(g.Text)
				}
				b.WriteByte(' ')
			}
		}
	}
	walk(pg.Content)
	return b.String()
}

func TestCullPage_DropsWhatClipsHide(t *testing.T) {
	run := func(x, y float64) *compose.Text {
		return &compose.Text{Size: 10, Color: compose.Black, Origin: compose.Point{X: x, Y: y}, Glyphs: []compose.Glyph{{GID: 1, Text: "a"}}}
	}
	clipTo := func(x, y, w, h float64, kids ...compose.Fragment) *compose.Group {
		return &compose.Group{Clip: &compose.Path{Ops: rectPath(compose.Rect{X: x, Y: y, Width: w, Height: h})}, Opacity: 1, Content: kids}
	}
	tests := []struct {
		name    string
		content []compose.Fragment
		want    int
		dropped bool
	}{
		{"on the page", []compose.Fragment{run(10, 10)}, 1, false},
		{"below the page", []compose.Fragment{run(10, 500)}, 0, true},
		{"crossing the page edge", []compose.Fragment{run(10, 205)}, 1, false},
		{"inside the clip", []compose.Fragment{clipTo(0, 0, 50, 50, run(10, 20))}, 1, false},
		{"past the clip", []compose.Fragment{clipTo(0, 0, 50, 50, run(10, 20), run(10, 150))}, 1, true},
		{"clip wholly hidden", []compose.Fragment{clipTo(0, 0, 50, 50, clipTo(100, 100, 10, 10, run(105, 105)))}, 0, true},
		{"transformed clip", []compose.Fragment{&compose.Group{Transform: compose.Matrix{A: 1, D: 1, F: 150}, Opacity: 1, Content: []compose.Fragment{clipTo(0, 0, 50, 20, run(10, 15), run(10, 100))}}}, 1, true},
		{"stroke reaching in", []compose.Fragment{clipTo(0, 0, 50, 50, &compose.Path{Ops: rectPath(compose.Rect{X: 52, Y: 10, Width: 10, Height: 10}), Stroke: &compose.Stroke{Color: compose.Black, Width: 6}})}, 1, false},
		{"empty group kept", []compose.Fragment{clipTo(0, 0, 50, 50)}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pg := &compose.Page{Width: 200, Height: 200, Content: tt.content}
			if got := cullPage(pg); got != tt.dropped {
				t.Errorf("cullPage reported %v, want %v", got, tt.dropped)
			}
			n := 0
			var walk func(fs []compose.Fragment)
			walk = func(fs []compose.Fragment) {
				for _, f := range fs {
					if g, ok := f.(*compose.Group); ok {
						walk(g.Content)
						continue
					}
					n++
				}
			}
			walk(pg.Content)
			if n != tt.want {
				t.Errorf("%d leaves left, want %d", n, tt.want)
			}
		})
	}
}

func TestConvert_CullsTextPastOverflowClip(t *testing.T) {
	t.Parallel()
	var lines []string
	for i := range 200 {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	src := `<body style="margin:0"><div style="height:12pt;line-height:12pt;overflow:hidden">` + strings.Join(lines, "<br>") + `</div><p>after</p>`
	doc := conv(t, src, Options{})
	got := pageText(doc.Pages[0])
	if !strings.Contains(got, "line0") || !strings.Contains(got, "after") {
		t.Errorf("text %q lost what the clip shows", got)
	}
	if strings.Contains(got, "line5") || strings.Contains(got, "line199") {
		t.Errorf("text %q holds lines the clip hides", got)
	}
	size := func(d *compose.Document) int {
		var b bytes.Buffer
		if err := compose.Write(context.Background(), d, &b, compose.Options{}); err != nil {
			t.Fatalf("Write: %v", err)
		}
		return b.Len()
	}
	clipped := size(doc)
	plain := size(conv(t, `<body style="margin:0"><div style="height:12pt;line-height:12pt">line0</div><p>after</p>`, Options{}))
	if clipped > plain+plain/4 {
		t.Errorf("clipped document %d bytes, the visible text alone %d", clipped, plain)
	}
}
