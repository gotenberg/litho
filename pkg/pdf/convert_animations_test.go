package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestWithFinishedAnimations checks that an animation prints at time zero
// by default and as it stands once finished with the option: a box that
// fades in and keeps its last keyframe is absent, then drawn.
func TestWithFinishedAnimations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := `<style>body { margin: 0 } @keyframes fade { from { opacity: 0 } to { opacity: 1 } }
	div { width: 100px; height: 100px; background: blue; animation: fade 1s forwards }</style><div></div>`
	for _, tt := range []struct {
		name    string
		opts    []HTMLOption
		r, g, b uint32
	}{
		{"time zero", nil, 0xffff, 0xffff, 0xffff},
		{"time zero, stated", []HTMLOption{WithFinishedAnimations(false)}, 0xffff, 0xffff, 0xffff},
		{"finished", []HTMLOption{WithFinishedAnimations(true)}, 0, 0, 0xffff},
	} {
		opts := append([]HTMLOption{WithConvertFontDirs(convertFontDir(t)), WithPaper(Paper{Width: 200, Height: 200}), WithMargins(0, 0, 0, 0)}, tt.opts...)
		var out bytes.Buffer
		if _, err := FromHTML(strings.NewReader(src), opts...).WriteTo(ctx, &out); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		img, err := r.RenderPage(ctx, 1, WithRenderDPI(72))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		// At 72 dpi the box spans device pixels 0 to 75 each way.
		if r, g, b, _ := img.At(30, 30).RGBA(); r != tt.r || g != tt.g || b != tt.b {
			t.Errorf("%s: colour %x %x %x, want %x %x %x", tt.name, r, g, b, tt.r, tt.g, tt.b)
		}
	}
}
