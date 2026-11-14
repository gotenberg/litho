package main

import (
	"bytes"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// reftestOrigin is the origin a document of the fetched WPT tree is served
// from when it names a file outside its own directory: the host WPT's own
// server answers as, which no request ever leaves the process for (see
// [reftestTransport]).
const reftestOrigin = "http://web-platform.test/"

// reftestDocument is one document of a reftest, the test or a reference:
// its WPT path and the files it reads, by WPT path (reftests.json).
type reftestDocument struct {
	path      string
	resources []string
}

func (e *reftestEntry) document() reftestDocument {
	return reftestDocument{path: e.ID, resources: e.Resources}
}

func (r reftestRef) document() reftestDocument {
	return reftestDocument{path: r.Path, resources: r.Resources}
}

// namesOutside reports whether the document reads a file outside its own
// directory: a math font it names by an absolute URL under /fonts/math/
// (select_tests.go, wptMathFonts). Such a document is converted from
// [reftestOrigin] through [reftestTransport], since litho reads a
// root-relative URL from no file system; every other document is converted
// with the file system rooted at its directory.
func (d reftestDocument) namesOutside() bool {
	dir := path.Dir(d.path) + "/"
	return slices.ContainsFunc(d.resources, func(r string) bool { return !strings.HasPrefix(r, dir) })
}

// client returns the HTTP client a document that names files outside its
// directory is converted with, serving the fetched WPT tree under root.
func (d reftestDocument) client(root string) *http.Client {
	return &http.Client{Transport: reftestTransport{root: root, dir: path.Dir(d.path) + "/", listed: d.resources}}
}

// reftestTransport answers the requests of one document's conversion from
// the fetched WPT tree under root, as WPT's server would: a file of the
// document's own directory dir, or one of the files listed for it outside
// it. Anything else, another host, /fonts/ahem.css among them (Ahem is
// installed as a font), is answered 404, as a file the file system route
// does not hold is left out. No request leaves the process.
type reftestTransport struct {
	root   string
	dir    string
	listed []string
}

// maxReftestResourceBytes bounds a file the transport reads. Anti-DoS
// guard, not spec-mandated: the files are the hash-checked ones of the
// fetched WPT tree, the largest of them a few hundred kilobytes, and litho
// reads at most 32 MiB a resource.
const maxReftestResourceBytes = 32 << 20

func (t reftestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_ = req.Body.Close() //nolint:errcheck // a GET carries no body to lose
	}
	p := strings.TrimPrefix(req.URL.Path, "/")
	served := "http://"+req.URL.Host+"/" == reftestOrigin && fs.ValidPath(p) &&
		(strings.HasPrefix(p, t.dir) || slices.Contains(t.listed, p))
	if !served {
		return reftestResponse(req, http.StatusNotFound, nil), nil
	}
	f, err := os.Open(filepath.Join(filepath.FromSlash(t.root), filepath.FromSlash(p))) //nolint:gosec // a file of the fetched WPT tree, under the document's directory or listed for it
	if err != nil {
		return reftestResponse(req, http.StatusNotFound, nil), nil //nolint:nilerr // a file the tree lacks is a 404, as WPT's server answers
	}
	defer f.Close() // read-only: a close error loses nothing
	data, err := io.ReadAll(io.LimitReader(f, maxReftestResourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReftestResourceBytes {
		return reftestResponse(req, http.StatusRequestEntityTooLarge, nil), nil
	}
	return reftestResponse(req, http.StatusOK, data), nil
}

// reftestResponse is the response to req of the status code, with data as
// its body, typed by its path's extension.
func reftestResponse(req *http.Request, code int, data []byte) *http.Response {
	h := http.Header{}
	if code == http.StatusOK {
		if typ := mime.TypeByExtension(path.Ext(req.URL.Path)); typ != "" {
			h.Set("Content-Type", typ)
		}
	}
	h.Set("Content-Length", strconv.Itoa(len(data)))
	return &http.Response{
		Status: strconv.Itoa(code) + " " + http.StatusText(code), StatusCode: code,
		Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: h, Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)), Request: req,
	}
}
