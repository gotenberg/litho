package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// officeCorpusFeature returns the diff feature of a provisioned Office
// corpus, its goldens loaded, and the corpus's fixtures; it skips the test
// when the corpus is not provisioned.
func officeCorpusFeature(t *testing.T, corpus string) (*diffFeature, []Fixture) {
	t.Helper()
	src := filepath.Join(corpusSourceDir, corpus)
	data, err := os.ReadFile(filepath.Join(src, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "testdata", corpus)
	goldens := filepath.Join(root, filepath.FromSlash(m.Goldens["diff"]))
	if _, err := os.Stat(filepath.Join(goldens, "render")); err != nil {
		t.Skipf("%s is not provisioned (make %s-fetch %s-libreoffice, then corpus init --corpus=%s)", corpus, corpus, corpus, corpus)
	}
	pf, err := os.Open(filepath.Join(corpusSourceDir, versionsFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer pf.Close()
	entries, err := parsePins(pf)
	if err != nil {
		t.Fatal(err)
	}
	pins, err := resolvePins(entries, os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	f := newDiffFeature()
	f.refs, f.testdataRoot, f.office, f.pins = m.References, root, true, pins.Values
	if err := f.LoadGoldens(goldens); err != nil {
		t.Fatal(err)
	}
	fixtures, err := globInputFixtures(src, m.Inputs.Glob)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := listInputFixtures(filepath.Join(src, m.Inputs.List), filepath.Join(root, m.Inputs.ListRoot), m.Inputs.FetchedBy)
	if err != nil {
		t.Skip(err)
	}
	return f, append(fixtures, listed...)
}

// TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens proves
// the docx and odt rig on its own references: each committed source's
// LibreOffice PDF, handed to the diff feature as litho's conversion, passes
// Prepare's checks and scores 1 on pages, text, links, strict and outline
// against the goldens made from it, and clears the layout bar, where the
// two sides differ only by litho drawing what poppler drew. The fetched
// documents are left to a corpus run: drawing all of them takes a minute.
//
// # Corpus evidence
//
//   - docx, odt: each of the 514 references (360 and 154) converted back
//     through the feature passes on 498. Pages, links and outline score 1 on
//     all; text F1 is below 1 on 18, where litho's text extraction and
//     PDF.js's split words apart (i18n's Thai and Devanagari at 0.786,
//     dkvert.docx's vertical text and layout/tdf137025.docx at 0), and
//     strict fails on 6 whose hairlines and hatches litho draws lighter than
//     poppler (odfexport/sw_hatch.odt layout 0.037); layout averages 0.966,
//     below 0.75 on 5 (ooxmlexport/fdo67737.docx 0.32).
func TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens(t *testing.T) {
	t.Parallel()

	for _, corpus := range []string{"docx", "odt"} {
		f, fixtures := officeCorpusFeature(t, corpus)
		if err := f.Prepare(t.Context(), fixtures); err != nil {
			t.Fatal(err)
		}
		checked := 0
		for _, fx := range fixtures {
			if strings.Contains(fx.ID, "/") {
				continue
			}
			checked++
			ref, err := os.ReadFile(filepath.Join(f.testdataRoot, "libreoffice", reportName(fx.ID)+".pdf"))
			if err != nil {
				t.Fatal(err)
			}
			f.convert = func(context.Context, []byte, htmlSettings) ([]byte, error) { return ref, nil }
			out, err := f.Run(t.Context(), fx.PDFPath, fx.ID)
			if err != nil {
				t.Fatal(err)
			}
			v := f.Compare(fx.ID, out)
			s := v.Scores
			// i18n's Thai and Devanagari: litho's text extraction and
			// PDF.js put the spaces of a cluster apart.
			wantText := 1.0
			if fx.ID == "i18n" {
				wantText = 0.78
			}
			if s[scorePages] != 1 || s[scoreText] < wantText || s[scoreLinks] != 1 || s[scoreStrict] != 1 || s[scoreOutline] != 1 || s[scoreLayout] < diffLayoutBar {
				t.Errorf("%s %s: %s", corpus, fx.ID, v.Reason)
			}
		}
		if checked == 0 {
			t.Errorf("%s: no committed source", corpus)
		}
	}
}

func TestDiffFeature_PrepareRefusesStaleReferences(t *testing.T) {
	t.Parallel()

	sha := func(s string) string {
		h := sha256.Sum256([]byte(s))
		return hex.EncodeToString(h[:])
	}
	pins := map[string]string{"GOTENBERG_IMAGE": "img:1", "LIBREOFFICE_CORE_COMMIT": "c1"}
	good := func() referenceManifest {
		return referenceManifest{References: map[string]referenceRecord{
			"memo":           {SourceSHA256: sha("docx"), OptionsSHA256: sha(`{"landscape": true}`), PDFSHA256: sha("pdf memo"), Image: "img:1"},
			"export__a.docx": {SourceSHA256: sha("fetched"), PDFSHA256: sha("pdf a"), Image: "img:1", CoreCommit: "c1"},
		}}
	}
	cases := []struct {
		name   string
		edit   func(m *referenceManifest, root string)
		reason string
	}{
		{"current", func(*referenceManifest, string) {}, ""},
		{"another image", func(m *referenceManifest, _ string) {
			r := m.References["memo"]
			r.Image = "img:0"
			m.References["memo"] = r
		}, "built with img:0"},
		{"another core commit", func(m *referenceManifest, _ string) {
			r := m.References["export__a.docx"]
			r.CoreCommit = "c0"
			m.References["export__a.docx"] = r
		}, "LibreOffice core c0"},
		{"source changed", func(m *referenceManifest, _ string) {
			r := m.References["memo"]
			r.SourceSHA256 = sha("older")
			m.References["memo"] = r
		}, "source changed"},
		{"options changed", func(m *referenceManifest, _ string) {
			r := m.References["memo"]
			r.OptionsSHA256 = ""
			m.References["memo"] = r
		}, "options changed"},
		{"refused", func(m *referenceManifest, _ string) {
			delete(m.References, "memo")
			m.Failed = map[string]referenceFailure{"memo": {Status: "400", Answer: "could not read"}}
		}, "refused by the route"},
		{"goldens from another build", func(_ *referenceManifest, root string) {
			writeFiles(t, root, map[string]string{"pdfs/memo.pdf": "pdf older"})
		}, "goldens were not made"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src, root := t.TempDir(), t.TempDir()
			writeFiles(t, src, map[string]string{
				"sources/memo/memo.docx":    "docx",
				"sources/memo/options.json": `{"landscape": true}`,
			})
			writeFiles(t, root, map[string]string{
				"sources/export/a.docx":   "fetched",
				"pdfs/memo.pdf":           "pdf memo",
				"pdfs/export__a.docx.pdf": "pdf a",
			})
			m := good()
			tc.edit(&m, root)
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			writeFiles(t, root, map[string]string{"libreoffice/manifest.json": string(data)})
			f := newDiffFeature()
			f.refs = &ReferenceSpec{Dir: "libreoffice", BuiltBy: "make office-libreoffice"}
			f.testdataRoot, f.office, f.pins = root, true, pins
			fixtures := []Fixture{
				{ID: "memo", PDFPath: filepath.Join(src, "sources", "memo")},
				{ID: "export/a.docx", PDFPath: filepath.Join(root, "sources", "export", "a.docx")},
			}
			err = f.Prepare(t.Context(), fixtures)
			switch {
			case tc.reason == "" && err != nil:
				t.Errorf("Prepare: %v", err)
			case tc.reason != "" && (err == nil || !errors.Is(err, errStaleReferences) || !strings.Contains(err.Error(), tc.reason)):
				t.Errorf("Prepare error %v, want one naming %q", err, tc.reason)
			}
		})
	}

	// A corpus whose references git versions has nothing to check.
	if err := newDiffFeature().Prepare(t.Context(), nil); err != nil {
		t.Errorf("html corpus: %v", err)
	}
}
