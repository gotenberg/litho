package html

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// docText returns the text of every page of doc, the pages apart.
func docText(doc *typeset.Document) string {
	var b strings.Builder
	for _, pg := range doc.Pages {
		b.WriteString(pageText(pg))
		b.WriteString("\n")
	}
	return b.String()
}

// frameDocs returns a file system of the HTML documents files maps names
// to, each a body holding the text given.
func frameDocs(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte("<!doctype html><body>" + body)}
	}
	return fsys
}

// srcdoc returns an iframe of srcdoc doc, escaped as an attribute.
func srcdoc(doc, attrs string) string {
	return `<iframe ` + attrs + ` srcdoc="` + html.EscapeString(doc) + `"></iframe>`
}

// A frame prints the document of its srcdoc, else of its src, in its box
// (HTML Standard, 4.8.5).
func TestFrame_PrintsItsDocument(t *testing.T) {
	fsys := frameDocs(map[string]string{"child.html": "<p>Childtext</p>", "other.html": "<p>Othertext</p>"})
	tests := []struct {
		name, src, want string
		absent          []string
	}{
		{"srcdoc", srcdoc("<p>Srcdoctext</p>", ""), "Srcdoctext", nil},
		{"src", `<iframe src="child.html"></iframe>`, "Childtext", nil},
		{"srcdoc wins over src", srcdoc("<p>Srcdoctext</p>", `src="child.html"`), "Srcdoctext", []string{"Childtext"}},
		{"fallback content never", `<iframe src="child.html"><p>Fallbacktext</p></iframe>`, "Childtext", []string{"Fallbacktext"}},
		{"display none", `<iframe src="child.html" style="display:none"></iframe><p>Outertext</p>`, "Outertext", []string{"Childtext"}},
		{"lazy", `<iframe src="child.html" loading="lazy"></iframe>`, "Childtext", nil},
		{"sandboxed", `<iframe src="child.html" sandbox></iframe>`, "Childtext", nil},
		{"data URL", `<iframe src="data:text/html,%3Cp%3EDatatext"></iframe>`, "Datatext", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := convertDoc(t, `<!doctype html><body>`+tt.src, Options{Resources: fsys})
			got := docText(doc)
			if !strings.Contains(got, tt.want) {
				t.Errorf("text %q, want %q", got, tt.want)
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("text %q holds %q", got, a)
				}
			}
			for _, w := range doc.Warnings {
				if strings.Contains(w.Message, "frame") || strings.Contains(w.Message, "iframe") {
					t.Errorf("warning %v", w)
				}
			}
		})
	}
}

// What a frame's document shows past its viewport is cut, and its own
// relative URLs resolve against its URL (HTML Standard, 2.4.1): a
// picture beside it, a link written relative to the conversion's
// document.
func TestFrame_CutAtItsViewportAndResolvedAgainstItsURL(t *testing.T) {
	fsys := frameDocs(map[string]string{
		"sub/child.html": `<style>body{margin:0}p{margin:0;height:100px}</style><p>Firsttext</p><p><a href="other.html">Linktext</a></p><p>Lasttext</p>`,
	})
	fsys["sub/pic.png"] = &fstest.MapFile{Data: pngBytes(t, 2, 2)}
	fsys["sub/child.html"].Data = append(fsys["sub/child.html"].Data, []byte(`<img src="pic.png" alt="Pic">`)...)
	doc := convertDoc(t, `<!doctype html><iframe src="sub/child.html" style="width:300px;height:150px"></iframe>`, Options{Resources: fsys})
	got := docText(doc)
	if !strings.Contains(got, "Firsttext") || !strings.Contains(got, "Linktext") || strings.Contains(got, "Lasttext") {
		t.Errorf("text %q, want the first 150 pixels of the frame's document", got)
	}
	var uris []string
	for _, l := range doc.Pages[0].Links {
		uris = append(uris, l.URI)
	}
	if len(uris) != 1 || uris[0] != "sub/other.html" {
		t.Errorf("links %v, want sub/other.html", uris)
	}
	for _, w := range doc.Warnings {
		t.Errorf("warning %v", w)
	}
	// The paragraph and the picture past the viewport leave the structure
	// with their content.
	var kids []string
	var find func(e *typeset.StructElem)
	find = func(e *typeset.StructElem) {
		if e.Role == frameRole {
			for _, k := range e.Kids {
				kids = append(kids, k.Role)
			}
		}
		for _, k := range e.Kids {
			find(k)
		}
	}
	find(doc.Structure)
	if strings.Join(kids, ",") != "P,P" {
		t.Errorf("the frame holds %v, want the two paragraphs it shows", kids)
	}
}

// The frame's document is tagged under a DocumentFragment, the frame's
// element (ISO 32000-2, 14.8.4.4, Table 364), its headings out of the
// outline as Chromium prints them.
func TestFrame_TaggedUnderADocumentFragment(t *testing.T) {
	src := `<!doctype html><h1>Outer</h1>` + srcdoc(`<html lang="de"><h1>Inner</h1><p>Para</p>`, "") + `<h2>After</h2>`
	doc := convertDoc(t, src, Options{})
	var roles []string
	var walk func(e *typeset.StructElem, depth int)
	var fragment *typeset.StructElem
	walk = func(e *typeset.StructElem, depth int) {
		roles = append(roles, strings.Repeat(" ", depth)+e.Role)
		if e.Role == frameRole {
			fragment = e
		}
		for _, k := range e.Kids {
			walk(k, depth+1)
		}
	}
	walk(doc.Structure, 0)
	want := "Document\n H1\n DocumentFragment\n  H1\n  P\n H2"
	if got := strings.Join(roles, "\n"); got != want {
		t.Errorf("structure\n%s\nwant\n%s", got, want)
	}
	if fragment == nil || fragment.Lang != "de" {
		t.Errorf("fragment %+v, want the language of the frame's document", fragment)
	}
	var titles []string
	for _, o := range doc.Outline {
		titles = append(titles, o.Title)
		for _, k := range o.Kids {
			titles = append(titles, k.Title)
		}
	}
	if strings.Join(titles, ",") != "Outer,After" {
		t.Errorf("outline %v, want Outer and After alone", titles)
	}
}

// The frame's document is print media at the frame's width, laid out in
// one column its @page rules and forced breaks do not cut, its fixed
// boxes in its viewport.
func TestFrame_MediaAndPages(t *testing.T) {
	child := `<style>@page{size:A6;margin:0}.p,.w{display:none}@media print{.p{display:block}}@media (min-width:400px){.w{display:block}}</style>` +
		`<p class=p>Printtext</p><p class=w>Widetext</p><p style="break-before:page">Breaktext</p>`
	tests := []struct {
		width   string
		wide    bool
		warning bool
	}{{"300px", false, false}, {"500px", true, false}}
	for _, tt := range tests {
		t.Run(tt.width, func(t *testing.T) {
			doc := convertDoc(t, `<!doctype html>`+srcdoc(child, `style="width:`+tt.width+`;height:400px"`), Options{})
			got := docText(doc)
			if len(doc.Pages) != 1 || !strings.Contains(got, "Printtext") || !strings.Contains(got, "Breaktext") || strings.Contains(got, "Widetext") != tt.wide {
				t.Errorf("%d pages, text %q", len(doc.Pages), got)
			}
		})
	}
}

// Every bound of a frame leaves it out with a warning and converts the
// rest of the page, within the conversion's context deadline.
func TestFrame_Bounds(t *testing.T) {
	chain := map[string]string{}
	for i := range 3 * maxFrameDepth {
		chain[fmt.Sprintf("d%d.html", i)] = fmt.Sprintf(`<p>Level%d</p><iframe src="d%d.html" width=600 height=400></iframe>`, i, i+1)
	}
	many := strings.Repeat(srcdoc("<p>x</p>", "width=2 height=2"), maxFrames+5)
	big := frameDocs(map[string]string{"big.html": strings.Repeat("<p>long</p>", 400)})
	tests := []struct {
		name, src string
		fsys      fstest.MapFS
		o         Options
		code      warn.Code
		message   string
		want      []string
		absent    []string
		once      string
	}{
		{name: "itself", src: `<p>Outertext</p><iframe src="index.html"></iframe>`, fsys: frameDocs(map[string]string{"index.html": `<p>Selftext</p><iframe src="index.html"></iframe>`}),
			code: warn.LimitReached, message: "holds a document that holds it", want: []string{"Outertext", "Selftext"}, once: "Selftext"},
		{name: "a cycle", src: `<iframe src="a.html" width=800 height=600></iframe>`,
			fsys: frameDocs(map[string]string{"a.html": `<p>Atext</p><iframe src="b.html" width=700 height=500></iframe>`, "b.html": `<p>Btext</p><iframe src="a.html#x" width=600 height=400></iframe>`}),
			code: warn.LimitReached, message: "holds a document that holds it", want: []string{"Atext", "Btext"}},
		{name: "depth", src: `<iframe src="d0.html" width=600 height=400></iframe>`, fsys: frameDocs(chain),
			code: warn.LimitReached, message: fmt.Sprintf("nested deeper than %d", maxFrameDepth), want: []string{"Level0", fmt.Sprintf("Level%d", maxFrameDepth-1)}, absent: []string{fmt.Sprintf("Level%d", maxFrameDepth)}},
		{name: "count", src: many + "<p>Outertext</p>", code: warn.LimitReached, message: fmt.Sprintf("more than %d frames", maxFrames), want: []string{"Outertext"}},
		{name: "size", src: `<p>Outertext</p><iframe src="big.html"></iframe>`, fsys: big, o: Options{MaxInputBytes: 1000},
			code: warn.LimitReached, message: "pass the input cap of 1000 bytes", want: []string{"Outertext"}, absent: []string{"long"}},
		{name: "size together", src: `<p>Outertext</p>` + strings.Repeat(`<iframe src="half.html"></iframe>`, 3), fsys: frameDocs(map[string]string{"half.html": strings.Repeat("<p>Half</p>", 30)}), o: Options{MaxInputBytes: 1000},
			code: warn.LimitReached, message: "pass the input cap of 1000 bytes", want: []string{"Outertext", "Half"}},
		{name: "missing", src: `<p>Outertext</p><iframe src="missing.html"></iframe>`, fsys: fstest.MapFS{},
			code: warn.ResourceFailed, message: "the frame missing.html was left out", want: []string{"Outertext"}},
		{name: "outside the root", src: `<iframe src="../x.html"></iframe>`, fsys: fstest.MapFS{}, code: warn.ResourceRefused, message: "the frame ../x.html was left out"},
		{name: "a picture", src: `<iframe src="p.png"></iframe>`, fsys: fstest.MapFS{"p.png": {Data: pngBytes(t, 2, 2)}}, code: warn.ContentUnsupported, message: "not an HTML document"},
		{name: "a text file", src: `<iframe src="t.txt"></iframe>`, fsys: fstest.MapFS{"t.txt": {Data: []byte("<b>x")}}, code: warn.ContentUnsupported, message: "not an HTML document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			o := tt.o
			o.Resources = tt.fsys
			doc, err := Convert(ctx, strings.NewReader(`<!doctype html><body>`+tt.src), testFonts(t), o)
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if err := doc.Validate(t.Context()); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !hasWarning(doc.Warnings, tt.code, tt.message) {
				t.Errorf("warnings %v, want %s %q", doc.Warnings, tt.code, tt.message)
			}
			got := docText(doc)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("text %q, want %q", got, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("text %q holds %q", got, a)
				}
			}
			if tt.once != "" && strings.Count(got, tt.once) != 1 {
				t.Errorf("text %q holds %q %d times, want once", got, tt.once, strings.Count(got, tt.once))
			}
		})
	}
}

// Sizes that leave no viewport load nothing and say nothing: a NaN of
// calc() is 0 (CSS Values 4, 10.9); sizes that do not parse leave the
// default object size.
func TestFrame_Sizes(t *testing.T) {
	fsys := frameDocs(map[string]string{"c.html": "<p>Childtext</p>", "x.html": "<p>Never</p>"})
	tests := []struct {
		attrs string
		shown bool
	}{
		{`width=0 height=0`, false},
		{`style="width:0;height:100px"`, false},
		{`width=-5 height=abc`, true},
		{`style="width:calc(0px/0)"`, false},
		{`style="height:NaNpx"`, true},
		{`style="height:-3px"`, true},
	}
	for _, tt := range tests {
		t.Run(tt.attrs, func(t *testing.T) {
			doc := convertDoc(t, `<!doctype html><iframe src="c.html" `+tt.attrs+`></iframe>`, Options{Resources: fsys})
			if got := docText(doc); strings.Contains(got, "Childtext") != tt.shown {
				t.Errorf("text %q, shown want %v", got, tt.shown)
			}
			for _, w := range doc.Warnings {
				if strings.Contains(w.Message, "frame") {
					t.Errorf("warning %v", w)
				}
			}
		})
	}
}

// A srcdoc of bytes that are not UTF-8 reads as the conversion's source
// does: each invalid sequence a U+FFFD.
func TestFrame_SrcdocOfInvalidUTF8(t *testing.T) {
	src := "<!doctype html><meta charset=utf-8><iframe srcdoc=\"<p>A\xff\xfeB</p>\"></iframe>"
	doc := convertDoc(t, src, Options{})
	if got := docText(doc); !strings.Contains(got, "A") || !strings.Contains(got, "B") {
		t.Errorf("text %q", got)
	}
}

// A frame whose document does not load ends a conversion that requires
// its resources, as a picture does.
func TestFrame_RequiredResource(t *testing.T) {
	_, err := Convert(t.Context(), strings.NewReader(`<!doctype html><iframe src="missing.html"></iframe>`), testFonts(t), Options{Resources: fstest.MapFS{}, RequiredResources: true})
	if !errors.Is(err, ErrResourceUnavailable) {
		t.Errorf("error %v, want %v", err, ErrResourceUnavailable)
	}
}

// The same source and resources write the same bytes, however many
// frames load which documents in which order, and each document is read
// once.
func TestFrame_SameBytes(t *testing.T) {
	fsys := frameDocs(map[string]string{
		"a.html": `<p>A</p><iframe src="b.html"></iframe>`,
		"b.html": `<p>B</p>`,
	})
	src := `<!doctype html><iframe src="b.html"></iframe><iframe src="a.html" width=400 height=300></iframe>` + srcdoc(`<iframe src="a.html"></iframe>`, "") + `<iframe src="b.html" style="transform:rotate(3deg)"></iframe>`
	write := func() []byte {
		doc := convertDoc(t, src, Options{Resources: fsys})
		var b bytes.Buffer
		if err := typeset.Write(t.Context(), doc, &b, typeset.Options{}); err != nil {
			t.Fatalf("Write: %v", err)
		}
		return b.Bytes()
	}
	first := write()
	for range 3 {
		if !bytes.Equal(write(), first) {
			t.Fatal("two conversions of the same source differ")
		}
	}
}

// marginwidth and marginheight on the frame set the margins of its
// document's body (HTML Standard, 15.3.2), which the body's own attributes
// override.
func TestFrame_Margins(t *testing.T) {
	body := func(n *frameDoc) string {
		var b strings.Builder
		for _, d := range n.body {
			fmt.Fprintf(&b, "%s;", d.Property)
		}
		return b.String()
	}
	doc, err := parse([]byte(`<iframe marginwidth="30" marginheight="x"></iframe><iframe marginheight=" 4 "></iframe>`))
	if err != nil {
		t.Fatal(err)
	}
	var frames []string
	walk(doc, func(n *dom.Node) bool {
		if n.Type == dom.ElementNode && n.Data == "iframe" {
			frames = append(frames, body(&frameDoc{body: frameMargins(n)}))
		}
		return true
	})
	if got := strings.Join(frames, "|"); got != "margin-left;margin-right;|margin-top;margin-bottom;" {
		t.Errorf("margins %q", got)
	}
	text := func(attrs string) float64 {
		doc := convertDoc(t, `<!doctype html><style>body{margin:0}</style>`+srcdoc(`<p style="margin:0">Childtext</p>`, attrs), Options{})
		for _, r := range pageTexts(doc.Pages[0]) {
			if strings.Contains(r.text, "Childtext") {
				return r.x
			}
		}
		t.Fatalf("no Childtext in %q", docText(doc))
		return 0
	}
	plain, wide := text(""), text(`marginwidth=30`)
	if !approx(wide-plain, 22*0.75, 0.01) {
		t.Errorf("text moved %v points by marginwidth 30, want 16.5", wide-plain)
	}
}

// An object or an embed of an HTML document shows it as an iframe does;
// of anything else, or of a document that does not load, an object shows
// its fallback content and an embed nothing.
func TestFrame_ObjectAndEmbed(t *testing.T) {
	fsys := frameDocs(map[string]string{"c.html": "<p>Childtext</p>", "c.svg": "<p>Svgtext</p>", "c": "<p>Plaintext</p>"})
	tests := []struct {
		name, src, want string
		absent          []string
	}{
		{"object", `<object data="c.html">Fallbacktext</object>`, "Childtext", []string{"Fallbacktext"}},
		{"object of an HTML type", `<object data="c" type="text/html">Fallbacktext</object>`, "Plaintext", []string{"Fallbacktext"}},
		{"object of another type", `<object data="c.html" type="image/png">Fallbacktext</object>`, "Fallbacktext", []string{"Childtext"}},
		{"object of a picture's name", `<object data="c.svg">Fallbacktext</object>`, "Fallbacktext", []string{"Svgtext"}},
		{"object missing", `<object data="missing.html">Fallbacktext</object>`, "Fallbacktext", nil},
		{"embed", `<embed src="c.html"><p>Outertext</p>`, "Childtext", nil},
		{"embed of another type", `<embed src="c.html" type="image/svg+xml"><p>Outertext</p>`, "Outertext", []string{"Childtext"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := convertDoc(t, `<!doctype html><body>`+tt.src, Options{Resources: fsys})
			got := docText(doc)
			if !strings.Contains(got, tt.want) {
				t.Errorf("text %q, want %q", got, tt.want)
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("text %q holds %q", got, a)
				}
			}
		})
	}
}

// A frame's document holds what its viewport shows, not what lies past
// it, which would stay in memory until the conversion ends.
func TestFrame_HoldsWhatItsViewportShows(t *testing.T) {
	o, err := Options{}.withDefaults()
	if err != nil {
		t.Fatal(err)
	}
	s := newFrameSet(testFonts(t), layout.Layout)
	fd := &frameDoc{url: srcdocURL, base: documentURL, res: newResources(nil), depth: 1}
	data := `<style>body{margin:0}p{margin:0;height:100px}</style><p>Firsttext</p><p>Secondtext</p><p>Lasttext</p>`
	out, _, err := s.layOutDoc(t.Context(), o, fd, data, 150, 120)
	if err != nil {
		t.Fatal(err)
	}
	got := pageText(&typeset.Page{Content: out.Content})
	if !strings.Contains(got, "Firsttext") || !strings.Contains(got, "Secondtext") || strings.Contains(got, "Lasttext") {
		t.Errorf("text %q, want the first two paragraphs alone", got)
	}
	if n := len(out.Structure.Kids); n != 2 {
		t.Errorf("%d elements, want 2", n)
	}
}

// A group that only clips the frame's page to a rectangle holding the
// viewport is unwrapped; any other group stays.
func TestUnwrapViewport(t *testing.T) {
	rect := func(x0, y0, x1, y1 float64) *typeset.Path {
		return &typeset.Path{Ops: []typeset.PathOp{
			{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x0, Y: y0}}}, {Kind: typeset.LineTo, P: [3]typeset.Point{{X: x1, Y: y0}}},
			{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x1, Y: y1}}}, {Kind: typeset.LineTo, P: [3]typeset.Point{{X: x0, Y: y1}}},
			{Kind: typeset.ClosePath},
		}}
	}
	leaf := &typeset.Path{}
	view := area{0, 0, 100, 50}
	tests := []struct {
		name  string
		group *typeset.Group
		flat  bool
	}{
		{"page area", &typeset.Group{Opacity: 1, Clip: rect(0, 0, 100, 1e6)}, true},
		{"no clip", &typeset.Group{}, true},
		{"a smaller clip", &typeset.Group{Opacity: 1, Clip: rect(0, 0, 100, 40)}, false},
		{"a slanted clip", &typeset.Group{Opacity: 1, Clip: &typeset.Path{Ops: []typeset.PathOp{
			{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: -10, Y: -10}}}, {Kind: typeset.LineTo, P: [3]typeset.Point{{X: 200, Y: -5}}},
			{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 200, Y: 200}}}, {Kind: typeset.LineTo, P: [3]typeset.Point{{X: -10, Y: 200}}},
		}}}, false},
		{"a transform", &typeset.Group{Opacity: 1, Transform: typeset.Matrix{A: 2, D: 1}}, false},
		{"an opacity", &typeset.Group{Opacity: 0.5}, false},
		{"a blend mode", &typeset.Group{Opacity: 1, Blend: typeset.BlendMultiply}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.group.Content = []typeset.Fragment{leaf}
			out := unwrapViewport([]typeset.Fragment{tt.group}, view)
			if flat := len(out) == 1 && out[0] == typeset.Fragment(leaf); flat != tt.flat {
				t.Errorf("unwrapped %v, want %v", flat, tt.flat)
			}
		})
	}
}

// A frameset prints its frames side by side in the page area (HTML
// Standard, 15.6); a frame outside a frameset prints nothing, and the
// parser drops one in a body.
func TestFrame_Frameset(t *testing.T) {
	fsys := frameDocs(map[string]string{"left.html": "<p>Lefttext</p>", "right.html": "<p>Righttext</p>"})
	doc := convertDoc(t, `<!doctype html><html><frameset cols="40%,60%"><frame src="left.html"><frame src="right.html" srcdoc="<p>Srcdoctext"></frameset></html>`, Options{Resources: fsys})
	if len(doc.Pages) != 1 {
		t.Fatalf("%d pages, want 1", len(doc.Pages))
	}
	var left, right placed
	for _, r := range pageTexts(doc.Pages[0]) {
		switch {
		case strings.Contains(r.text, "Lefttext"):
			left = r
		case strings.Contains(r.text, "Righttext"):
			right = r
		}
	}
	if left.text == "" || right.text == "" || !approx(left.y, right.y, 0.01) || right.x <= left.x+100 {
		t.Errorf("left %+v, right %+v: want them side by side", left, right)
	}
	if got := docText(doc); strings.Contains(got, "Srcdoctext") {
		t.Errorf("text %q: a frame's srcdoc counts", got)
	}
	doc = convertDoc(t, `<!doctype html><p>Outertext</p><frame src="left.html">`, Options{Resources: fsys})
	if got := docText(doc); strings.Contains(got, "Lefttext") {
		t.Errorf("text %q: a frame in a body counts", got)
	}
}
