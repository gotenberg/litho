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
// may frame itself, as a srcdoc, as a text file and as a file the bytes
// alone say the kind of, a picture among them, and shows them in an object:
// the conversion ends, within its bounds, with a valid document.
func FuzzFrames(f *testing.F) {
	valid := `<!doctype html><style>@page{size:A6}body{background:red}</style><p>x</p><iframe src="f.html" width=50% height=-1></iframe>` +
		`<iframe srcdoc="<iframe src=f.html#a style='transform:rotate(9deg)'></iframe>"></iframe><object data=f.html></object>` +
		`<embed src=f.html type=text/html><iframe src="data:text/html,<p>d"></iframe><iframe frameborder=0 marginwidth=9 src=f.html></iframe>`
	f.Add([]byte(valid))
	f.Add([]byte(valid[:len(valid)/2]))
	f.Add([]byte("<iframe srcdoc=\"\xff\xfe\"></iframe>"))
	pic := pngBytes(f, 3, 2)
	f.Add(pic)
	f.Add(pic[:len(pic)/2])
	f.Add([]byte("<b>plain</b>\n\ttext \x1b\x00"))
	f.Add([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="9" height="9"><image href="f" width="9" height="9"/></svg>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		fsys := fstest.MapFS{"f.html": {Data: data}, "f.txt": {Data: data}, "f": {Data: data}}
		src := `<iframe src="f.html" width=300 height=200></iframe><iframe srcdoc="` + html.EscapeString(string(data)) + `"></iframe>` +
			`<iframe src="f.txt" width=200 height=100></iframe><iframe src="f" width=200 height=100></iframe><object data="f">x</object><embed src="f" type="text/plain">`
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
