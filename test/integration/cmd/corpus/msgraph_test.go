package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Not parallel: it sets the token's environment variable.
func TestMSGraphCall_AnExpiredStaticTokenStopsTheRun(t *testing.T) { //nolint:paralleltest // t.Setenv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	t.Setenv(msGraphTokenEnv, "Bearer tok")
	g, err := newMSGraph(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.call(t.Context(), http.MethodGet, srv.URL, nil); !errors.Is(err, errMSGraphSignIn) {
		t.Errorf("call error %v, want errMSGraphSignIn", err)
	}
}

func TestDocumentFromTemplate_RenamesTheMainPart(t *testing.T) {
	t.Parallel()

	tpl := msGraphTemplates[".potx"]
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"[Content_Types].xml":  `<Override PartName="/ppt/presentation.xml" ContentType="` + tpl.template + `"/>`,
		"ppt/presentation.xml": "<p:presentation/>",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := documentFromTemplate(buf.Bytes(), tpl)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[f.Name] = string(b)
	}
	if !strings.Contains(got["[Content_Types].xml"], tpl.document) || strings.Contains(got["[Content_Types].xml"], tpl.template) {
		t.Errorf("content types %q", got["[Content_Types].xml"])
	}
	if got["ppt/presentation.xml"] != "<p:presentation/>" {
		t.Errorf("presentation %q", got["ppt/presentation.xml"])
	}
	// A package whose content types name no template is refused.
	if _, err := documentFromTemplate(out, tpl); err == nil {
		t.Error("a document converted again")
	}
}

func TestWithLetterPageSetup_PlacesItWhereTheSchemaDoes(t *testing.T) {
	t.Parallel()

	const letter = `<pageSetup paperSize="1"/>`
	for name, tc := range map[string]struct{ in, want string }{
		"after pageMargins": {
			`<worksheet><sheetData/><pageMargins left="1"/><drawing r:id="a"/></worksheet>`,
			`<worksheet><sheetData/><pageMargins left="1"/>` + letter + `<drawing r:id="a"/></worksheet>`,
		},
		"before a later child, not a nested extLst": {
			`<worksheet><sheetData/><conditionalFormatting><extLst/></conditionalFormatting><headerFooter/></worksheet>`,
			`<worksheet><sheetData/><conditionalFormatting><extLst/></conditionalFormatting>` + letter + `<headerFooter/></worksheet>`,
		},
		"at the end, with the root's prefix": {
			`<x:worksheet xmlns:x="u"><x:sheetData/></x:worksheet>`,
			`<x:worksheet xmlns:x="u"><x:sheetData/><x:pageSetup paperSize="1"/></x:worksheet>`,
		},
		"an existing pageSetup is kept": {
			`<worksheet><pageMargins/><pageSetup orientation="landscape"/></worksheet>`,
			`<worksheet><pageMargins/><pageSetup orientation="landscape"/></worksheet>`,
		},
	} {
		got, err := withLetterPageSetup([]byte(tc.in))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, tc.want)
		}
	}
}
