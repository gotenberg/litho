package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// remoteServer serves documents and resources for the URL input tests,
// each path a content type and a body.
func remoteServer(t *testing.T) *httptest.Server {
	t.Helper()
	var pic bytes.Buffer
	if err := png.Encode(&pic, image.NewGray(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	routes := map[string][2]string{
		"/site/doc":      {"text/html; charset=utf-8", `<link rel=stylesheet href="s.css"><p class=x>AB</p><img src="i.png" alt="B">`},
		"/site/s.css":    {"text/css", `.x { font-size: 20px }`},
		"/site/i.png":    {"image/png", pic.String()},
		"/notes":         {"text/markdown; charset=utf-8", "# AB\n\nBA\n"},
		"/page.md":       {"text/plain", "# BA\n"},
		"/raw":           {"application/octet-stream", "<b>BAB</b>"},
		"/plain.txt":     {"text/plain", "AB\n"},
		"/assets/i.png":  {"image/png", pic.String()},
		"/moved/doc.htm": {"", ""},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/site/doc", http.StatusMovedPermanently)
			return
		}
		rt, ok := routes[r.URL.Path]
		if !ok || rt[0] == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", rt[0])
		_, _ = w.Write([]byte(rt[1]))
	}))
	t.Cleanup(ts.Close)
	return ts
}

// convertJSON runs litho convert -f json with args and returns its report.
func convertJSON(t *testing.T, args ...string) (convertResult, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	full := append([]string{"convert", "-f", "json", "--font-dir", convertFontDir(t), "-o", out}, args...)
	if rc := run(context.Background(), full, &stdout, &stderr); rc != exitOK {
		t.Fatalf("%v: exit %d, stderr %s", args, rc, stderr.String())
	}
	var res convertResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("report %q: %v", stdout.String(), err)
	}
	return res, out
}

func TestConvert_FetchesAURL(t *testing.T) {
	t.Parallel()
	ts := remoteServer(t)
	for _, tc := range []struct {
		args   []string
		format pdf.Format
		text   string
	}{
		{[]string{ts.URL + "/site/doc"}, pdf.FormatHTML, "AB"},
		{[]string{ts.URL + "/old"}, pdf.FormatHTML, "AB"},
		{[]string{ts.URL + "/notes"}, pdf.FormatMarkdown, "AB BA"},
		{[]string{ts.URL + "/page.md"}, pdf.FormatMarkdown, "BA"},
		{[]string{ts.URL + "/raw"}, pdf.FormatHTML, "BAB"},
		{[]string{"--from", "text", ts.URL + "/plain.txt"}, pdf.FormatText, "AB"},
		{[]string{"--from", "markdown", ts.URL + "/site/doc"}, pdf.FormatMarkdown, "AB"},
		{[]string{ts.URL + "/plain.txt"}, pdf.FormatText, "AB"},
	} {
		res, out := convertJSON(t, tc.args...)
		if res.Format != string(tc.format) {
			t.Errorf("%v: format %q, want %q", tc.args, res.Format, tc.format)
		}
		if len(res.Warnings) != 0 {
			t.Errorf("%v: warnings %+v", tc.args, res.Warnings)
		}
		if got := pdfText(t, out); len(got) != 1 || !strings.Contains(got[0], tc.text) {
			t.Errorf("%v: text %q, want %q", tc.args, got, tc.text)
		}
	}
	// The document's resources came over HTTP: its picture is drawn.
	_, out := convertJSON(t, ts.URL+"/old")
	r, err := pdf.OpenFile(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if imgs, err := r.ExtractImages(context.Background(), 1); err != nil || len(imgs) != 1 {
		t.Errorf("images %d, %v", len(imgs), err)
	}
}

func TestConvert_BaseURLFetchesResourcesOfAFile(t *testing.T) {
	t.Parallel()
	ts := remoteServer(t)
	src := writeTempText(t, "doc.html", `<p>AB</p><img src="i.png" alt="B">`)
	res, _ := convertJSON(t, "--base-url", ts.URL+"/assets/", src)
	if len(res.Warnings) != 0 {
		t.Errorf("warnings %+v", res.Warnings)
	}
	// Without a network base URL, the picture is read from the file's
	// directory, where it is not.
	res, _ = convertJSON(t, src)
	if len(res.Warnings) != 1 || res.Warnings[0].Code != "resource-failed" {
		t.Errorf("local: warnings %+v", res.Warnings)
	}
	// A file: base URL keeps the resources local and the network off.
	res, _ = convertJSON(t, "--base-url", "file:///srv/doc.html", src)
	if len(res.Warnings) != 1 || res.Warnings[0].Code != "resource-failed" {
		t.Errorf("file: base: warnings %+v", res.Warnings)
	}
}

func TestConvert_URLFailures(t *testing.T) {
	t.Parallel()
	ts := remoteServer(t)
	for _, url := range []string{ts.URL + "/missing", ts.URL + "/moved/doc.htm", "http://127.0.0.1:1/unreachable"} {
		var stdout, stderr bytes.Buffer
		out := filepath.Join(t.TempDir(), "out.pdf")
		if rc := run(context.Background(), []string{"convert", "--font-dir", convertFontDir(t), "-o", out, url}, &stdout, &stderr); rc != exitError {
			t.Errorf("%s: exit %d", url, rc)
		}
		if !strings.Contains(stderr.String(), url) {
			t.Errorf("%s: stderr %q", url, stderr.String())
		}
	}
}

func TestRemoteFormat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		ctype, url string
		want       pdf.Format
	}{
		{"text/html", "https://x/a.md", pdf.FormatHTML},
		{"text/markdown", "https://x/a.html", pdf.FormatMarkdown},
		{"text/x-markdown; charset=utf-8", "https://x/", pdf.FormatMarkdown},
		{"text/plain", "https://x/a.MARKDOWN?q=1", pdf.FormatMarkdown},
		{"text/plain", "https://x/a.txt", pdf.FormatText},
		{"", "https://x/a", pdf.FormatHTML},
		{"bogus;;", "https://x/a.htm", pdf.FormatHTML},
	} {
		if got := remoteFormat(tc.ctype, tc.url); got != tc.want {
			t.Errorf("remoteFormat(%q, %q) = %q, want %q", tc.ctype, tc.url, got, tc.want)
		}
	}
	for s, want := range map[string]bool{"https://x/": true, "HTTP://x": true, "http:x": false, "file:///a": false, "a.html": false, "-": false, "": false} {
		if got := isRemoteInput(s); got != want {
			t.Errorf("isRemoteInput(%q) = %v", s, got)
		}
	}
}
