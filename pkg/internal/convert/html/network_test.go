package html

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// testServer serves routes, each a content type and a body, counting
// every request; any other path is a 404.
type testServer struct {
	*httptest.Server
	hits atomic.Int64
}

type route struct {
	ctype  string
	body   []byte
	status int
	// redirect, when set, answers 302 to it.
	redirect string
}

func newTestServer(t *testing.T, routes map[string]route) *testServer {
	t.Helper()
	ts := &testServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.hits.Add(1)
		rt, ok := routes[r.URL.Path]
		switch {
		case !ok:
			http.NotFound(w, r)
		case rt.redirect != "":
			http.Redirect(w, r, rt.redirect, http.StatusFound)
		default:
			if rt.ctype != "" {
				w.Header().Set("Content-Type", rt.ctype)
			}
			if rt.status != 0 {
				w.WriteHeader(rt.status)
			}
			_, _ = w.Write(rt.body)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestPrepare_FetchesResourcesOverHTTP(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, map[string]route{
		"/css/site.css":  {ctype: "text/css", body: []byte(`@import "more.css"; #x { background-image: url(../img/bg.png) }`)},
		"/css/more.css":  {ctype: "text/css; charset=utf-8", body: []byte(`#x { color: rgb(255, 0, 0) }`)},
		"/img/bg.png":    {ctype: "image/png", body: pngBytes(t, 2, 2)},
		"/docs/a.png":    {ctype: "image/png", body: pngBytes(t, 3, 3)},
		"/assets/b.png":  {ctype: "image/png", body: pngBytes(t, 5, 5)},
		"/old/moved.css": {redirect: "/new/real.css"},
		"/new/real.css":  {ctype: "text/css", body: []byte(`#y { background-image: url(pic.png) }`)},
		"/new/pic.png":   {ctype: "application/octet-stream", body: pngBytes(t, 7, 7)},
	})
	src := `<link rel=stylesheet href="/css/site.css"><link rel=stylesheet href="../old/moved.css">` +
		`<p id=x>x<img id=i src="a.png"><img id=again src="a.png"></p><p id=y>y</p><a id=l href="other.html">l</a>`
	p := prep(t, src, Options{BaseURL: ts.URL + "/docs/index.html#frag", HTTPClient: ts.Client()})
	if len(p.warnings) != 0 {
		t.Fatalf("warnings: %+v", p.warnings)
	}
	x := byID(t, p.root, "x").Style
	if x.Color != (css.Color{R: 1, A: 1}) {
		t.Errorf("imported stylesheet not applied: colour %+v", x.Color)
	}
	if got := byID(t, p.root, "i").Picture; got == nil || got.Width != 3*css.Pt {
		t.Errorf("relative image: %+v", got)
	}
	if got := byID(t, p.root, "l").Href; got != ts.URL+"/docs/other.html" {
		t.Errorf("link: %q", got)
	}
	ctx := context.Background()
	// The sheet's url() resolves against the sheet's URL, and after a
	// redirect against the URL it was served from.
	for id, want := range map[string]float64{"x": 2, "y": 7} {
		u := byID(t, p.root, id).Style.BackgroundImage
		pic, err := p.layout.Image(ctx, u)
		if err != nil || pic.Width != want*css.Pt {
			t.Errorf("%s: background %q: %+v, %v", id, u, pic, err)
		}
	}
	// The document names a.png twice: one request.
	hits := ts.hits.Load()
	prep(t, `<img src="a.png"><img src="a.png"><img src="a.png">`, Options{BaseURL: ts.URL + "/docs/", HTTPClient: ts.Client()})
	if got := ts.hits.Load() - hits; got != 1 {
		t.Errorf("a picture named three times took %d requests", got)
	}
	// A <base href> overrides the base URL.
	b := prep(t, `<base href="/assets/"><img id=i src="b.png">`, Options{BaseURL: ts.URL + "/docs/", HTTPClient: ts.Client()})
	if got := byID(t, b.root, "i").Picture; got == nil || got.Width != 5*css.Pt {
		t.Errorf("base href: %+v %+v", got, b.warnings)
	}
}

func TestPrepare_ReportsHTTPFailures(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, map[string]route{
		"/gone.png":  {status: http.StatusGone, body: []byte("gone")},
		"/boom.css":  {status: http.StatusInternalServerError},
		"/plain.css": {ctype: "text/plain", body: []byte("#x { color: red }")},
		"/pic.svg":   {ctype: "image/svg+xml", body: []byte("<svg/>")},
		"/pic":       {ctype: "image/svg+xml", body: []byte(" <svg/>")},
		"/pic.avif":  {ctype: "image/avif", body: []byte("\x00\x00\x00\x1cftypavif")},
		"/fav.ico":   {ctype: "image/x-icon", body: []byte{0, 0, 1, 0, 1, 0, 16, 16}},
	})
	for _, tc := range []struct {
		src, code, sub string
	}{
		{`<img src="missing.png" alt=a>`, CodeResourceHTTPStatus, "404"},
		{`<img src="gone.png" alt=a>`, CodeResourceHTTPStatus, "410"},
		{`<link rel=stylesheet href="boom.css">`, CodeResourceHTTPStatus, "500"},
		{`<link rel=stylesheet href="plain.css">`, CodeResourceFailed, "text/plain"},
		{`<img src="pic.avif" alt=a>`, CodeImageFormatUnsupported, "AVIF"},
		{`<img src="fav.ico" alt=a>`, CodeImageFormatUnsupported, "ICO"},
		{`<img src="file:///etc/passwd" alt=a>`, CodeResourceRefused, "file:"},
	} {
		p := prep(t, tc.src, Options{BaseURL: ts.URL + "/", HTTPClient: ts.Client()})
		if !hasWarning(p.warnings, tc.code, tc.sub) {
			t.Errorf("%s: warnings %+v, want %s %q", tc.src, p.warnings, tc.code, tc.sub)
		}
	}
	// A picture served as image/svg+xml is drawn as SVG, whatever its
	// bytes start with.
	sp := prep(t, `<img id=i src="pic" alt=a>`, Options{BaseURL: ts.URL + "/", HTTPClient: ts.Client()})
	if got := byID(t, sp.root, "i").Picture; got == nil || got.Vector == nil {
		t.Errorf("image/svg+xml: %+v %+v", got, sp.warnings)
	}
	// Without a client nothing is fetched.
	hits := ts.hits.Load()
	p := prep(t, `<link rel=stylesheet href="a.css"><img src="a.png" alt=a>`, Options{BaseURL: ts.URL + "/"})
	if got := codes(p.warnings); len(got) != 2 || got[0] != CodeResourceNoNetwork || got[1] != CodeResourceNoNetwork {
		t.Errorf("no client: %+v", p.warnings)
	}
	if ts.hits.Load() != hits {
		t.Errorf("a request was sent without a client")
	}
	// A base URL that is not http(s) keeps the resources local.
	local := prep(t, `<img id=i src="a.png">`, Options{BaseURL: "file:///srv/doc.html", Resources: fstest.MapFS{"a.png": {Data: pngBytes(t, 1, 1)}}})
	if byID(t, local.root, "i").Picture == nil {
		t.Errorf("file: base URL: %+v", local.warnings)
	}
}

func TestResources_NetworkCaps(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", 1024)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/huge" {
			// One byte past the cap is enough to refuse it.
			_, _ = io.CopyN(w, zeroReader{}, maxResourceBytes+1)
			return
		}
		_, _ = io.WriteString(w, big)
	}))
	t.Cleanup(ts.Close)
	ctx := context.Background()
	r := newResources(nil)
	r.client = ts.Client()
	if _, err := r.fetch(ctx, ts.URL+"/huge"); warningCode(err) != CodeResourceLimit {
		t.Errorf("huge: %v", err)
	}
	for i := range maxNetworkRequests - 1 {
		if _, err := r.fetch(ctx, fmt.Sprintf("%s/%d", ts.URL, i)); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	_, err := r.fetch(ctx, ts.URL+"/one-more")
	if warningCode(err) != CodeResourceLimit || !strings.Contains(err.Error(), "HTTP requests") {
		t.Errorf("request past the cap: %v", err)
	}
	// A URL already fetched still answers from the cache.
	if _, err := r.fetch(ctx, ts.URL+"/0"); err != nil {
		t.Errorf("cached URL: %v", err)
	}
	// The total byte budget is shared with the files.
	r = newResources(nil)
	r.client = ts.Client()
	r.read = maxTotalResourceBytes - 10
	if _, err := r.fetch(ctx, ts.URL+"/x"); warningCode(err) != CodeResourceLimit {
		t.Errorf("past the budget: %v", err)
	}
	// A cancelled context stops the fetch.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	r = newResources(nil)
	r.client = ts.Client()
	if _, err := r.fetch(cctx, ts.URL+"/x"); err == nil {
		t.Errorf("cancelled fetch succeeded")
	}
}

// zeroReader reads zeros forever.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestDocumentBase(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"":                           documentURL,
		"https://example.com/a#frag": "https://example.com/a",
		"HTTP://example.com/":        "http://example.com/",
		"file:///srv/a.html":         documentURL,
		"http:///no-host":            documentURL,
		"urn:isbn:0451450523":        documentURL,
	} {
		if got := documentBase(in); got != want {
			t.Errorf("documentBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func FuzzResolveRef(f *testing.F) {
	for _, s := range [][2]string{
		{documentURL, "a.png"},
		{"https://example.com/a/b.html", "../c.css"},
		{"https://example.com/a/b.html", "//cdn.example.com/x"},
		{documentURL, "file:///etc/passwd"},
		{"http://[::1]:80/", "?q#f"},
		{documentURL, "data:image/png;base64,AAAA"},
	} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, base, ref string) {
		u := resolveRef(documentBase(base), ref)
		if u == "" {
			return
		}
		// Whatever it resolves to, a fetch without resources or client
		// fails cleanly or reads a data: URL.
		r := newResources(nil)
		if _, err := r.fetch(context.Background(), u); err == nil && !strings.HasPrefix(strings.ToLower(u), "data:") {
			t.Fatalf("fetch(%q) succeeded without resources", u)
		}
	})
}
