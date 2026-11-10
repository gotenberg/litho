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
	src := filepath.Join(corpusSourceDir, filepath.FromSlash(corpus))
	data, err := os.ReadFile(filepath.Join(src, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "testdata", filepath.FromSlash(corpus))
	goldens := filepath.Join(root, filepath.FromSlash(m.Goldens["diff"]))
	if _, err := os.Stat(filepath.Join(goldens, "render")); err != nil {
		t.Skipf("%s is not provisioned (run %s)", corpus, prepareRemedy(corpus))
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
	m.Name = corpus
	f.m, f.testdataRoot, f.office, f.pins = &m, root, true, pins.Values
	if err := f.LoadGoldens(goldens); err != nil {
		t.Fatal(err)
	}
	var fixtures []Fixture
	if m.Inputs.Glob != "" {
		if fixtures, err = globInputFixtures(src, m.Inputs.Glob); err != nil {
			t.Fatal(err)
		}
	}
	if m.Inputs.Build != "" {
		// The documents corpus build zipped from parts/, its hostile ones
		// left out: they have no reference.
		entries, err := os.ReadDir(filepath.Join(src, m.Inputs.Build))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() {
				fixtures = append(fixtures, Fixture{ID: e.Name(), PDFPath: filepath.Join(root, m.Inputs.ListRoot, e.Name())})
			}
		}
	}
	if m.Inputs.List != "" {
		listed, err := listInputFixtures(filepath.Join(src, m.Inputs.List), filepath.Join(root, m.Inputs.ListRoot), prepareRemedy(corpus))
		if err != nil {
			t.Skip(err)
		}
		fixtures = append(fixtures, listed...)
	}
	return f, fixtures
}

// TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens proves
// the Office rig on its own references: each litho Office corpus's
// LibreOffice PDF, handed to the diff feature as litho's conversion, passes
// Prepare's checks and scores 1 on pages, links and outline against the
// goldens made from it, and clears the text and layout bars but for the
// references below, where litho's reading and drawing of the PDF part from
// PDF.js's and poppler's. The list is exact, so a reference that comes
// to clear the bars, or one that drops under them, fails the test. The
// libreoffice corpora are left to a corpus run: drawing all of them takes a
// minute.
//
// # Corpus evidence
//
//   - litho/docx: of the 122 references, 119 clear both bars; text F1 is
//     under 0.85 on intl-rtl-arabic (0.809), intl-rtl-hebrew (0.829) and
//     intl-scripts (0.774).
//   - litho/xlsx: of the 90 references, 85 clear both bars; text F1 is under
//     0.85 on chartex_region_map (0.500), column_widths (0.830), fonts
//     (0.494), groups_connectors (0.500) and rtl_sheet (0.679).
//   - litho/pptx: of the 114 references, 112 clear both bars;
//     background-pattern's layout is 0.302 and text-complex-scripts's text
//     F1 0.792.
func TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens(t *testing.T) {
	t.Parallel()

	underBars := map[string]map[string]bool{
		"docx": {"intl-rtl-arabic": true, "intl-rtl-hebrew": true, "intl-scripts": true},
		"xlsx": {"chartex_region_map": true, "column_widths": true, "fonts": true, "groups_connectors": true, "rtl_sheet": true},
		"pptx": {"background-pattern": true, "text-complex-scripts": true},
	}
	for _, format := range []string{"docx", "xlsx", "pptx"} {
		corpus := "litho/" + format
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f, fixtures := officeCorpusFeature(t, corpus)
			if err := f.Prepare(t.Context(), fixtures); err != nil {
				t.Fatal(err)
			}
			for _, fx := range fixtures {
				ref, err := os.ReadFile(filepath.Join(f.testdataRoot, filepath.FromSlash(f.m.References.Dir), reportName(fx.ID)+".pdf"))
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
				if s[scorePages] != 1 || s[scoreLinks] != 1 || s[scoreOutline] != 1 {
					t.Errorf("%s %s: %v %s", corpus, fx.ID, s, v.Reason)
				}
				clears := s[scoreText] >= diffTextBar && s[scoreLayout] >= diffLayoutBar
				if clears == underBars[format][fx.ID] {
					t.Errorf("%s %s: text %.3f, layout %.3f; listed under the bars: %v", corpus, fx.ID, s[scoreText], s[scoreLayout], underBars[format][fx.ID])
				}
			}
			if len(fixtures) == 0 {
				t.Errorf("%s: no source", corpus)
			}
		})
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
			"memo":           {SourceSHA256: sha("docx"), OptionsSHA256: sha(`{"landscape": true}`), PDFSHA256: sha("pdf memo"), Image: "img:1", SourcePin: "LIBREOFFICE_CORE_COMMIT", SourcePinValue: "c1"},
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
		}, `LIBREOFFICE_CORE_COMMIT "c0"`},
		{"another pin value", func(m *referenceManifest, _ string) {
			r := m.References["memo"]
			r.SourcePinValue = "c0"
			m.References["memo"] = r
		}, `LIBREOFFICE_CORE_COMMIT "c0"`},
		{"another pin", func(m *referenceManifest, _ string) {
			r := m.References["memo"]
			r.SourcePin = "POI_COMMIT"
			m.References["memo"] = r
		}, `LIBREOFFICE_CORE_COMMIT ""`},
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
			writeFiles(t, root, map[string]string{"references/manifest.json": string(data)})
			f := newDiffFeature()
			f.m = &Manifest{Name: "test/docx", References: &ReferenceSpec{Dir: "references", Engine: engineLibreOffice, SourcePin: "LIBREOFFICE_CORE_COMMIT"}}
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

	// A corpus without references has nothing to check.
	if err := newDiffFeature().Prepare(t.Context(), nil); err != nil {
		t.Errorf("no references: %v", err)
	}
}

func TestCheckReference_MicrosoftReferencesIgnoreTheImagePin(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	writeFiles(t, src, map[string]string{"sources/memo/memo.docx": "docx"})
	h := sha256.Sum256([]byte("docx"))
	fx := Fixture{ID: "memo", PDFPath: filepath.Join(src, "sources", "memo")}
	refs := &ReferenceSpec{Dir: "references", Engine: engineMicrosoft}
	pins := map[string]string{"GOTENBERG_IMAGE": "img:2"}
	man := func(builder string) referenceManifest {
		return referenceManifest{References: map[string]referenceRecord{
			"memo": {SourceSHA256: hex.EncodeToString(h[:]), Image: builder, BuiltAt: "2026-11-10"},
		}}
	}
	if why, _ := checkReference(man(microsoftRenderer), refs, "memo", fx, pins); why != "" {
		t.Errorf("a Microsoft reference is stale: %s", why)
	}
	// A LibreOffice reference of a corpus that moved to Microsoft is stale.
	if why, rebuild := checkReference(man("img:2"), refs, "memo", fx, pins); !rebuild || !strings.Contains(why, "built with img:2") {
		t.Errorf("a LibreOffice reference in a Microsoft corpus: %q, rebuild %v", why, rebuild)
	}
}

func TestDiffFeature_GoldenSourceNamesTheReferenceEngine(t *testing.T) {
	t.Parallel()
	f := newDiffFeature()
	if got := f.GoldenSource(); got != "chromium" {
		t.Fatalf("GoldenSource = %q, want chromium", got)
	}
	f.SetManifest(&Manifest{References: &ReferenceSpec{Engine: engineChromium}})
	if got := f.GoldenSource(); got != "chromium" {
		t.Fatalf("GoldenSource = %q, want chromium", got)
	}
	f.SetManifest(&Manifest{References: &ReferenceSpec{Engine: engineLibreOffice}})
	if got := f.GoldenSource(); got != "libreoffice" {
		t.Fatalf("GoldenSource = %q, want libreoffice", got)
	}
	f.SetManifest(&Manifest{References: &ReferenceSpec{Engine: engineMicrosoft}})
	if got := f.GoldenSource(); got != "microsoft" || !f.office {
		t.Fatalf("GoldenSource = %q, office %v, want microsoft, an Office corpus", got, f.office)
	}
}
