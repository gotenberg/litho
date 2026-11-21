package html

import (
	"bytes"
	"context"
	"errors"
	"html"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// FuzzFrames converts a page that frames the fuzzed bytes as a file, which
// may frame itself, and as a srcdoc: the conversion ends, within its
// bounds, with a valid document.
func FuzzFrames(f *testing.F) {
	valid := `<!doctype html><style>@page{size:A6}body{background:red}</style><p>x</p><iframe src="f.html" width=50% height=-1></iframe>` +
		`<iframe srcdoc="<iframe src=f.html#a style='transform:rotate(9deg)'></iframe>"></iframe>` +
		`<iframe src="data:text/html,<p>d"></iframe><iframe frameborder=0 marginwidth=9 src=f.html></iframe>`
	f.Add([]byte(valid))
	f.Add([]byte(valid[:len(valid)/2]))
	f.Add([]byte("<iframe srcdoc=\"\xff\xfe\"></iframe>"))
	f.Fuzz(func(t *testing.T, data []byte) {
		fsys := fstest.MapFS{"f.html": {Data: data}}
		src := `<iframe src="f.html" width=300 height=200></iframe><iframe srcdoc="` + html.EscapeString(string(data)) + `"></iframe>`
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		o := Options{Resources: fsys, MaxInputBytes: 1 << 14, MaxPages: 16}
		doc, err := Convert(ctx, strings.NewReader(src), testFonts(t), o)
		if errors.Is(err, limits.ErrInputTooLarge) || errors.Is(err, limits.ErrTooManyPages) || errors.Is(err, ErrTooDeep) ||
			errors.Is(err, ErrTooManyElements) || errors.Is(err, css.ErrTooComplex) || errors.Is(err, layout.ErrTooManyBoxes) {
			return
		}
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		var b bytes.Buffer
		if err := typeset.Write(context.Background(), doc, &b, typeset.Options{}); err != nil {
			t.Fatalf("typeset.Write: %v", err)
		}
	})
}
