package main

import (
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestReftestDocument_NamesOutside pins which documents are served from
// the WPT origin: those reading a file outside their own directory.
func TestReftestDocument_NamesOutside(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		doc  reftestDocument
		want bool
	}{
		{reftestDocument{path: "css/css-box/a.html"}, false},
		{reftestDocument{path: "css/css-box/a.html", resources: []string{"css/css-box/support/i.png"}}, false},
		{reftestDocument{path: "mathml/relations/css-styling/a.html", resources: []string{"fonts/math/f.woff"}}, true},
		{reftestDocument{path: "css/css-box/a.html", resources: []string{"css/css-box-other/i.png"}}, true},
	} {
		if got := c.doc.namesOutside(); got != c.want {
			t.Errorf("%+v: names outside %v, want %v", c.doc, got, c.want)
		}
	}
	e := &reftestEntry{ID: "a/t.html", Resources: []string{"x"}}
	r := reftestRef{Path: "a/r.html", Resources: []string{"y"}}
	if d := e.document(); d.path != "a/t.html" || len(d.resources) != 1 || d.resources[0] != "x" {
		t.Errorf("test document %+v", d)
	}
	if d := r.document(); d.path != "a/r.html" || len(d.resources) != 1 || d.resources[0] != "y" {
		t.Errorf("reference document %+v", d)
	}
}

// TestReftestTransport_ServesTheDocumentsFilesAlone pins what the WPT
// origin answers: the document's own directory and the files listed for
// it, typed by their extension's media type, and 404 for anything else,
// /fonts/ahem.css and other hosts included.
func TestReftestTransport_ServesTheDocumentsFilesAlone(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for rel, body := range map[string]string{
		"mathml/scripts/support/s.css": "p {}",
		"mathml/scripts/i.png":         "png",
		"fonts/math/f.woff":            "listed font",
		"fonts/math/g.woff":            "unlisted font",
		"fonts/ahem.css":               "@font-face {}",
		"mathml/other/o.css":           "q {}",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	doc := reftestDocument{path: "mathml/scripts/t.html", resources: []string{"fonts/math/f.woff"}}
	client := doc.client(root)
	for _, c := range []struct {
		url, body, typ string
		code           int
	}{
		{reftestOrigin + "mathml/scripts/support/s.css", "p {}", mime.TypeByExtension(".css"), http.StatusOK},
		{reftestOrigin + "mathml/scripts/i.png", "png", mime.TypeByExtension(".png"), http.StatusOK},
		{reftestOrigin + "fonts/math/f.woff", "listed font", mime.TypeByExtension(".woff"), http.StatusOK},
		{reftestOrigin + "fonts/math/g.woff", "", "", http.StatusNotFound},
		{reftestOrigin + "fonts/ahem.css", "", "", http.StatusNotFound},
		{reftestOrigin + "mathml/other/o.css", "", "", http.StatusNotFound},
		{reftestOrigin + "mathml/scripts/../other/o.css", "", "", http.StatusNotFound},
		{reftestOrigin + "mathml/scripts/missing.css", "", "", http.StatusNotFound},
		{"http://example.com/mathml/scripts/i.png", "", "", http.StatusNotFound},
	} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, c.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", c.url, err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close() //nolint:errcheck // an in-memory body
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != c.code || string(body) != c.body || resp.Header.Get("Content-Type") != c.typ {
			t.Errorf("%s: %d %q %q, want %d %q %q", c.url, resp.StatusCode, resp.Header.Get("Content-Type"), body, c.code, c.typ, c.body)
		}
	}
}
