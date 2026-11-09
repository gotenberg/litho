package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// fetchServer serves files by path, ranges included, and counts requests.
func fetchServer(t *testing.T, files map[string][]byte) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		data, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, filepath.Base(r.URL.Path), time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestCorpusFetch_GitHubAndZip(t *testing.T) {
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	docBody := bytes.Repeat([]byte("binary document "), 5000)
	w, err := zw.Create("000/000002.doc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(docBody); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	ref, licence, font := []byte("a docx"), []byte("licence"), []byte("font")
	srv, hits := fetchServer(t, map[string][]byte{
		"/owner/repo/c1/test data/a b.docx": ref,
		"/owner/repo/c1/LICENSE":            licence,
		"/owner/repo/c1/fonts/x.ttf":        font,
		"/owner/repo/c1/tests/t.svg":        licence,
		"/zips/000.zip":                     archive.Bytes(),
	})
	old := githubRaw
	githubRaw = srv.URL + "/"
	t.Cleanup(func() { githubRaw = old })

	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeFiles(t, src, map[string]string{
		"office/files.sha256":   sha(ref) + "  test data/a b.docx\n" + sha(licence) + "  LICENSE\n",
		"office/documents.json": `[{"id": "test_data/a_b.docx", "path": "test data/a b.docx"}]`,
		"mapped/files.sha256":   sha(font) + "  fonts/x.ttf\n" + sha(licence) + "  tests/t.svg\n",
		"zipped/files.sha256":   sha(docBody) + "  000.zip/000/000002.doc\n",
		"zipped/documents.json": `[{"id": "000/000002.doc", "path": "000.zip/000/000002.doc"}]`,
	})
	testdata := filepath.Join(root, "test", "integration", "testdata")
	list := &InputSpec{List: "documents.json", ListRoot: "sources"}
	ms := []*Manifest{
		{Name: "office", Source: "office", Inputs: list, Fetch: &FetchSpec{GitHub: "owner/repo", Pin: "C"}},
		{Name: "mapped", Source: "mapped", GoldensInSource: true, Fetch: &FetchSpec{GitHub: "owner/repo", Pin: "C", Map: []FetchMap{
			{Prefix: "fonts/", To: "mine/", Fonts: true}, {Prefix: "tests/", To: ""},
		}}},
		{Name: "zipped", Source: "zipped", Inputs: list, Fetch: &FetchSpec{Zip: srv.URL + "/zips", Pin: "T"}},
	}
	t.Chdir(root)
	if err := os.MkdirAll(filepath.Join(root, "test", "integration"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, filepath.Join(root, "test", "integration", "corpus")); err != nil {
		t.Fatal(err)
	}
	pins := map[string]string{"C": "c1", "T": "000"}
	var files []fetchFile
	zips := newZipCache(srv.Client())
	for _, m := range ms {
		fs, err := corpusFetchFiles(m, pins, testdata, srv.Client(), zips)
		if err != nil {
			t.Fatalf("%s: %v", m.Name, err)
		}
		files = append(files, fs...)
	}
	counts, err := downloadAll(t.Context(), files, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{
		"test/integration/testdata/office/sources/test_data/a_b.docx": ref,
		"test/integration/testdata/office/sources/LICENSE":            licence,
		"test/integration/testdata/fonts/mine/x.ttf":                  font,
		"test/integration/testdata/mapped/t.svg":                      licence,
		"test/integration/testdata/zipped/sources/000/000002.doc":     docBody,
	}
	for p, body := range want {
		if got, err := os.ReadFile(filepath.Join(root, p)); err != nil || !bytes.Equal(got, body) {
			t.Errorf("%s: %v", p, err)
		}
	}
	if c := counts["zipped"]; c.total != 1 || c.downloaded != 1 {
		t.Errorf("zipped counts %+v", c)
	}

	before := hits.Load()
	counts, err = downloadAll(t.Context(), files, 3)
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != before || counts["office"].downloaded != 0 {
		t.Errorf("a second fetch downloaded again: %d requests, %+v", hits.Load()-before, counts)
	}

	bad := files[0]
	bad.sha256 = strings.Repeat("0", 64)
	bad.dest = filepath.Join(root, "bad")
	if _, err := downloadAll(t.Context(), []fetchFile{bad}, 1); !errors.Is(err, errHashMismatch) {
		t.Errorf("hash mismatch: %v", err)
	}
	if _, err := os.Stat(bad.dest); err == nil {
		t.Error("a file of the wrong hash was installed")
	}
}

func TestCorpusMatches_NameGlobOrSource(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"poi/docx", "poi/docx", true},
		{"poi", "poi/docx", true},
		{"poi/*", "poi/docx", true},
		{"*/docx", "litho/docx", true},
		{"*/docx", "litho/xlsx", false},
		{"po", "poi/docx", false},
		{"wpt", "wpt", true},
		{"litho", "litho/pdf", true},
		{"litho/p*", "litho/pptx", true},
	} {
		if got := corpusMatches(tc.pattern, tc.name); got != tc.want {
			t.Errorf("corpusMatches(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

func TestArMember_ReadsADebianPackage(t *testing.T) {
	t.Parallel()

	member := func(name, body string) string {
		h := make([]byte, 60)
		for i := range h {
			h[i] = ' '
		}
		copy(h, name)
		copy(h[48:], strconv.Itoa(len(body)))
		copy(h[58:], "`\n")
		pad := ""
		if len(body)%2 == 1 {
			pad = "\n"
		}
		return string(h) + body + pad
	}
	deb := []byte("!<arch>\n" + member("debian-binary", "2.0\n") + member("data.tar.xz", "xz!"))
	got, err := arMember(deb, "data.tar.xz")
	if err != nil || string(got) != "xz!" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := arMember(deb, "control.tar.xz"); err == nil {
		t.Error("a missing member was found")
	}
	if _, err := arMember([]byte("not ar"), "data.tar.xz"); err == nil {
		t.Error("not an ar archive was read")
	}
}
