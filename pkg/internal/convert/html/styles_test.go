package html

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/css"
)

var (
	red   = css.Color{R: 1, A: 1}
	green = css.Color{G: 128.0 / 255, A: 1}
	blue  = css.Color{B: 1, A: 1}
)

func TestPrepare_CascadesStylesheetsInDocumentOrder(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"a.css": {Data: []byte("#x { color: green } #y { color: green }")},
	}
	src := `<style>#x { color: red } #z { color: red }</style><link rel=stylesheet href=a.css>` +
		`<style>#y { color: blue }</style><p id=x>x<p id=y>y<p id=z>z<p id=w style="color: blue">w`
	p := prep(t, src, Options{Resources: fsys})
	for id, want := range map[string]css.Color{"x": green, "y": blue, "z": red, "w": blue} {
		if got := byID(t, p.root, id).Style.Color; got != want {
			t.Errorf("%s: colour %+v, want %+v", id, got, want)
		}
	}
	if len(p.warnings) != 0 {
		t.Errorf("warnings %+v", p.warnings)
	}
}

func TestPrepare_HonoursMediaAttributes(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"print.css":  {Data: []byte("#l { color: red }")},
		"screen.css": {Data: []byte("#l { color: blue }")},
	}
	src := `<link rel=stylesheet href=print.css media=print><link rel=stylesheet href=screen.css media="screen">` +
		`<style media=print>#s { color: red }</style><style media="screen and (min-width: 100px)">#s { color: blue }</style>` +
		`<style media="print;}#b{color:red">#b { color: blue }</style>` +
		`<style media="print">@import "print.css"; #i { color: red }</style><p id=l>l<p id=s>s<p id=b>b<p id=i>i`
	for media, want := range map[string]css.Color{MediaPrint: red, MediaScreen: blue} {
		p := prep(t, src, Options{Resources: fsys, Media: media})
		for _, id := range []string{"l", "s"} {
			if got := byID(t, p.root, id).Style.Color; got != want {
				t.Errorf("%s, %s: colour %+v, want %+v", media, id, got, want)
			}
		}
		if got := byID(t, p.root, "b").Style.Color; got != (css.Color{A: 1}) {
			t.Errorf("%s: a malformed media attribute applied: %+v", media, got)
		}
	}
}

func TestPrepare_SkipsNonCSSAndAlternateSheets(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"a.css": {Data: []byte("#x { color: red }")}}
	src := `<style type="text/less">#x { color: red }</style><link rel="alternate stylesheet" href=a.css>` +
		`<link rel=stylesheet type="text/plain" href=a.css><link rel=icon href=a.css><p id=x>x`
	p := prep(t, src, Options{Resources: fsys})
	if got := byID(t, p.root, "x").Style.Color; got != (css.Color{A: 1}) {
		t.Errorf("colour %+v, want the initial black", got)
	}
}

func TestPrepare_ReadsImportsAndURLsThroughTheResources(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"css/main.css":         {Data: []byte(`@import "parts/colors.css"; #x { background-image: url(../img/bg.png) }`)},
		"css/parts/colors.css": {Data: []byte(`@import url("../../../escape.css"); #x { color: red }`)},
		"img/bg.png":           {Data: pngBytes(t, 2, 2)},
	}
	src := `<link rel=stylesheet href="css/main.css"><style>#y { background-image: url("img/bg.png") }</style>` +
		`<p id=x>x<p id=y>y<p id=z style="background-image: url(img/z.png)">z`
	p := prep(t, src, Options{Resources: fsys})
	x := byID(t, p.root, "x")
	if x.Style.Color != red {
		t.Errorf("imported colour %+v", x.Style.Color)
	}
	for id, want := range map[string]string{"x": "img/bg.png", "y": "img/bg.png", "z": "img/z.png"} {
		u := byID(t, p.root, id).Style.BackgroundImage
		if u != rootPrefix+want {
			t.Errorf("%s: background %q, want %q", id, u, rootPrefix+want)
		}
	}
	if pic, err := p.layout.Image(t.Context(), x.Style.BackgroundImage); err != nil || pic.Width != 2*css.Pt {
		t.Errorf("background picture %+v, %v", pic, err)
	}
	if !hasWarning(p.warnings, "css-import-failed", "leaves the resources root") {
		t.Errorf("warnings %+v, want the escaping import refused", p.warnings)
	}
	for _, w := range p.warnings {
		if strings.Contains(w.Message, rootPrefix) {
			t.Errorf("warning %q names the internal root", w.Message)
		}
	}
}

func TestPrepare_RefusesStylesheetsOutsideTheResources(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"a.css": {Data: []byte("#x { color: red }")}}
	for _, tc := range []struct {
		href string
		fsys fstest.MapFS
		code string
	}{
		{"https://example.com/a.css", fsys, CodeResourceRefused},
		{"../a.css", fsys, CodeResourceRefused},
		{"a.css", nil, CodeResourceRefused},
		{"missing.css", fsys, CodeResourceFailed},
	} {
		o := Options{}
		if tc.fsys != nil {
			o.Resources = tc.fsys
		}
		p := prep(t, `<link rel=stylesheet href="`+tc.href+`"><p id=x>x`, o)
		if !hasWarning(p.warnings, tc.code, tc.href) {
			t.Errorf("%s: warnings %+v, want %s", tc.href, p.warnings, tc.code)
		}
	}
	data := prep(t, `<link rel=stylesheet href="data:text/css,%23x%7Bcolor:red%7D"><p id=x>x`, Options{})
	if byID(t, data.root, "x").Style.Color != red || len(data.warnings) != 0 {
		t.Errorf("data: stylesheet not applied: %+v", data.warnings)
	}
}

func TestPrepare_ResolvesRelativeToBase(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"theme/a.css": {Data: []byte("#x { color: red }")}}
	p := prep(t, `<base href="theme/"><link rel=stylesheet href="a.css"><p id=x>x`, Options{Resources: fsys})
	if byID(t, p.root, "x").Style.Color != red {
		t.Errorf("base not applied to the link: %+v", p.warnings)
	}
}

func TestDecode_FollowsTheDeclaredCharset(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		src  []byte
		want string
	}{
		"utf-8 default":      {[]byte("<p>caf\xc3\xa9</p>"), "café"},
		"utf-8 with bom":     {[]byte("\xef\xbb\xbf<p>caf\xc3\xa9</p>"), "café"},
		"windows-1252 meta":  {[]byte(`<meta charset="windows-1252"><p>caf` + "\xe9" + `</p>`), "café"},
		"latin-1 http-equiv": {[]byte(`<meta http-equiv="Content-Type" content="text/html; charset=ISO-8859-1"><p>caf` + "\xe9 \x80" + `</p>`), "café €"},
		"utf-16le bom":       {[]byte("\xff\xfe<\x00p\x00>\x00\xe9\x00"), "é"},
		"undeclared latin":   {[]byte("<p>caf\xe9</p>"), "caf�"},
	} {
		got := decode(tc.src)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q, want it to hold %q", name, got, tc.want)
		}
	}
}

func TestPageBox_AppliesPageRules(t *testing.T) {
	t.Parallel()
	o := Options{PageWidth: 600, PageHeight: 800, MarginTop: 10, MarginRight: 20, MarginBottom: 30, MarginLeft: 40}
	for _, tc := range []struct {
		css           string
		width, height float64
		margin        [4]float64
		warn          bool
	}{
		{"", 600, 800, [4]float64{10, 20, 30, 40}, false},
		{"@page { size: 300pt 400pt }", 300, 400, [4]float64{10, 20, 30, 40}, false},
		{"@page { size: landscape }", 800, 600, [4]float64{10, 20, 30, 40}, false},
		{"@page { size: portrait }", 600, 800, [4]float64{10, 20, 30, 40}, false},
		{"@page { margin-top: 1in; margin-left: 10% }", 600, 800, [4]float64{72, 20, 30, 60}, false},
		{"@page { margin: 5% }", 600, 800, [4]float64{40, 30, 40, 30}, false},
		{"@page { margin: 0 }", 600, 800, [4]float64{}, false},
		{"@page :first { margin: 0 }", 600, 800, [4]float64{}, false},
		{"@page :left { margin: 0 }", 600, 800, [4]float64{10, 20, 30, 40}, false},
		{"@page { margin-left: 590pt }", 600, 800, [4]float64{10, 20, 30, 40}, true},
		{"@page { size: 50pt 50pt }", 50, 50, [4]float64{}, true},
		{"@page { size: 20000pt 100pt }", maxPageSide, 100, [4]float64{10, 20, 30, 40}, true},
	} {
		p := prep(t, "<style>"+tc.css+"</style><p>x", o)
		pb := p.layout.Page
		if pb.Width != tc.width || pb.Height != tc.height || pb.Margin != tc.margin {
			t.Errorf("%q: page %+v, want %vx%v %v", tc.css, pb, tc.width, tc.height, tc.margin)
		}
		if got := hasWarning(p.warnings, CodePageBox, ""); got != tc.warn {
			t.Errorf("%q: page-box warning %v, want %v: %+v", tc.css, got, tc.warn, p.warnings)
		}
	}
}

func TestPageBoxes_FirstLeftAndRightPages(t *testing.T) {
	t.Parallel()
	o := Options{PageWidth: 600, PageHeight: 800, MarginTop: 10, MarginRight: 20, MarginBottom: 30, MarginLeft: 40}
	opt := [4]float64{10, 20, 30, 40}
	sheet := "@page :first { margin: 0 } @page :left { margin-left: 100pt } @page :right { margin-right: 100pt }"
	for _, tc := range []struct {
		name string
		src  string
		want [4][4]float64
	}{
		{"same pages", "<p>x", [4][4]float64{opt, opt, opt, opt}},
		{"ltr starts right", "<style>" + sheet + "</style><p>x", [4][4]float64{
			{}, {10, 20, 30, 100}, {10, 100, 30, 40}, {10, 20, 30, 100},
		}},
		{"rtl starts left", "<html dir=rtl><style>" + sheet + "</style><p>x", [4][4]float64{
			{}, {10, 100, 30, 40}, {10, 20, 30, 100}, {10, 100, 30, 40},
		}},
	} {
		p := prep(t, tc.src, o)
		for i, want := range tc.want {
			pb := p.layout.Page
			if p.layout.PageAt != nil {
				pb = p.layout.PageAt(i)
			} else if i > 0 && want != opt {
				t.Fatalf("%s: no PageAt", tc.name)
			}
			if pb.Margin != want {
				t.Errorf("%s: page %d margins %v, want %v", tc.name, i+1, pb.Margin, want)
			}
		}
		if tc.name == "same pages" && p.layout.PageAt != nil {
			t.Errorf("%s: PageAt set for identical pages", tc.name)
		}
	}
}

func TestPrepare_ViewportIsThePageArea(t *testing.T) {
	t.Parallel()
	src := `<style>@page { size: 300pt 300pt; margin: 50pt } #x { width: 100vw; height: 50vh }</style><p id=x>x`
	p := prep(t, src, Options{})
	st := byID(t, p.root, "x").Style
	if st.Width.Value != 200 || st.Height.Value != 100 {
		t.Errorf("100vw by 50vh is %v by %v, want 200 by 100: the page area of the first page", st.Width, st.Height)
	}
}

func TestPrepare_EvaluatesMediaOnThePageSize(t *testing.T) {
	t.Parallel()
	src := `<style>@page { size: 300pt 300pt } @media (max-width: 500px) { #x { color: red } }</style><p id=x>x`
	p := prep(t, src, Options{})
	if byID(t, p.root, "x").Style.Color != red {
		t.Errorf("media features compared against the paper, not the @page size")
	}
}
