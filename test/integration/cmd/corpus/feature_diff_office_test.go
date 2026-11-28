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
	if _, err := os.Stat(filepath.Join(goldens, "text")); err != nil {
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

// TestDiffFeature_OfficeReferencesScoreOneAgainstTheirGoldens proves the
// Office rig on its own references: each litho Office corpus's Microsoft
// PDF, handed to the diff feature as litho's conversion, passes
// Prepare's checks and scores 1 on pages, links and outline against the
// goldens made from it, 1 on layout and strict (both sides are the same
// PDF drawn by the same renderer), and clears the text bar but for the
// references below, where litho's reading of the PDF parts from PDF.js's. The list is exact, so a reference that comes
// to clear the bars, or one that drops under them, fails the test. A
// source that names an expected text is scored against it, not against
// the reference's text, and carries one because its reference does not hold
// the page's text: its reference must stay under the text bar, else the
// expected text is not needed. A source whose reference litho reads whole,
// where PDF.js reads it in pieces, carries one to correct the goldens: its
// reference stays under the bar against the goldens and clears it against
// the expected text. The
// libreoffice corpora are left to a corpus run: drawing all of them takes a
// minute.
//
// Under the race detector it scores one reference in
// [officeReferenceRaceSample] and every reference listed under the bars:
// drawing all of them takes each corpus eight minutes of the package's ten
// there, and the detector has nothing to find in a score.
//
// # Corpus evidence
//
// The references are Microsoft's (Office for the web through Graph); the
// fixtures Microsoft refused have none and are skipped.
//
//   - litho/docx: of the 126 references, 122 clear both bars; text F1 is
//     under 0.85 on intl-rtl-arabic (0.346), intl-rtl-hebrew (0.240),
//     para-alignment (0.826) and tables-rtl (0.713).
//   - litho/xlsx: of the 97 references, 96 clear both bars; text F1 is under
//     0.85 on rtl_sheet (0.407).
//   - litho/pptx: of the 116 references, 115 clear both bars; text F1 is under
//     0.85 on text-rtl (0.384).
//   - litho/pptx text-warp-wordart: litho reads the glyphs the reference sets
//     one by one along its arch, circle and button warps as words, where
//     PDF.js reads one a line ("W o r d A r t"), so text F1 against the
//     goldens is 0.793.
func TestDiffFeature_OfficeReferencesScoreOneAgainstTheirGoldens(t *testing.T) {
	t.Parallel()

	underBars := map[string]map[string]bool{
		"docx": {"intl-rtl-arabic": true, "intl-rtl-hebrew": true, "para-alignment": true, "tables-rtl": true},
		"xlsx": {"rtl_sheet": true},
		"pptx": {"text-rtl": true},
	}
	pdfjsParts := map[string]map[string]bool{
		"pptx": {"text-warp-wordart": true},
	}
	for _, format := range []string{"docx", "xlsx", "pptx"} {
		corpus := "litho/" + format
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			f, fixtures := officeCorpusFeature(t, corpus)
			if err := f.Prepare(t.Context(), fixtures); err != nil {
				t.Fatal(err)
			}
			for i, fx := range fixtures {
				if !f.HasGolden(fx.ID) {
					continue // refused by Microsoft: no reference to score
				}
				if raceEnabled && i%officeReferenceRaceSample != 0 && !underBars[format][fx.ID] {
					continue
				}
				f.mu.RLock()
				_, borrows := f.borrowed[fx.ID]
				f.mu.RUnlock()
				if borrows {
					continue // scored against another fixture's reference, which this loop feeds back itself
				}
				ref, err := os.ReadFile(filepath.Join(f.testdataRoot, filepath.FromSlash(f.m.References.Dir), reportName(fx.ID)+".pdf"))
				if err != nil {
					t.Fatal(err)
				}
				f.convert = func(context.Context, []byte, htmlSettings) ([]byte, error) { return ref, nil }
				out, err := f.Run(t.Context(), fx.PDFPath, fx.ID)
				if err != nil {
					t.Fatal(err)
				}
				var judged diffOutput
				if err := json.Unmarshal(out, &judged); err != nil {
					t.Fatal(err)
				}
				v := f.Compare(fx.ID, out)
				s := v.Scores
				if s[scorePages] != 1 || s[scoreLinks] != 1 || s[scoreOutline] != 1 || s[scoreLayout] != 1 || s[scoreStrict] != 1 {
					t.Errorf("%s %s: %v %s", corpus, fx.ID, s, v.Reason)
				}
				under := underBars[format][fx.ID] || judged.ExpectedText
				if pdfjsParts[format][fx.ID] {
					under = !judged.ExpectedText
				}
				clears := s[scoreText] >= diffTextBar && s[scoreLayout] >= diffLayoutBar
				if clears == under {
					t.Errorf("%s %s: text %.3f, layout %.3f; listed under the bars: %v, expected text: %v", corpus, fx.ID, s[scoreText], s[scoreLayout], underBars[format][fx.ID], judged.ExpectedText)
				}
			}
			if len(fixtures) == 0 {
				t.Errorf("%s: no source", corpus)
			}
		})
	}
}

// officeReferenceRaceSample is the share of each corpus's references
// [TestDiffFeature_OfficeReferencesScoreOneAgainstTheirGoldens] scores under
// the race detector: one in this many.
const officeReferenceRaceSample = 4

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
		}, "was refused by"},
		// A refusal of the current source, options and image would be
		// refused again: the fixture goes unscored, the cell runs.
		{"refused for good", func(m *referenceManifest, _ string) {
			delete(m.References, "memo")
			m.Failed = map[string]referenceFailure{"memo": {
				Status: "406", Answer: "ErrorCode=WordCorruptFile Url=x", Image: "img:1",
				SourceSHA256: sha("docx"), OptionsSHA256: sha(`{"landscape": true}`),
			}}
		}, ""},
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

// TestSetUpFeature_NamesTheCorpusEngine locks that a diff cell's report
// names the engine of its corpus's references, read after the manifest.
func TestSetUpFeature_NamesTheCorpusEngine(t *testing.T) {
	t.Parallel()
	for engine, want := range map[string]string{engineChromium: "chromium", engineLibreOffice: "libreoffice", engineMicrosoft: "microsoft"} {
		m := &Manifest{Name: "test/" + engine, References: &ReferenceSpec{Dir: "references", Engine: engine}}
		if got := setUpFeature(newDiffFeature(), m, runOptions{}).GoldenSource; got != want {
			t.Errorf("%s corpus: report names %q, want %q", engine, got, want)
		}
	}
}

func TestRefusalCause_KeepsTheErrorCode(t *testing.T) {
	t.Parallel()

	for answer, want := range map[string]string{
		`{"detail":"Error from Office Service. HttpCode=BadRequest ErrorCode=WordCantOpenDocument Url=https://x"}`: "WordCantOpenDocument",
		`{"error":{"Code":"notSupported","InnerError":{"Code":"InputFormatNotSupported"}}}`:                        "InputFormatNotSupported",
		"Microsoft Graph converts with no options\nmore":                                                           "Microsoft Graph converts with no options",
	} {
		if got := refusalCause(answer); got != want {
			t.Errorf("refusalCause(%q) = %q, want %q", answer, got, want)
		}
	}
}

// TestDiffFeature_BorrowsAReference checks a fixture whose options.json
// names reference_of: Prepare asks no reference of it and refuses none, its
// golden is the named fixture's, and a reference_of that names itself, no
// fixture, or a fixture that borrows in turn fails the cell.
func TestDiffFeature_BorrowsAReference(t *testing.T) {
	t.Parallel()

	sha := func(s string) string {
		h := sha256.Sum256([]byte(s))
		return hex.EncodeToString(h[:])
	}
	cases := []struct {
		name, options, reason string
	}{
		{"borrows", `{"landscape": true, "reference_of": "memo"}`, ""},
		{"itself", `{"reference_of": "wide"}`, "not another fixture"},
		{"no such fixture", `{"reference_of": "gone"}`, "not another fixture"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src, root := t.TempDir(), t.TempDir()
			writeFiles(t, src, map[string]string{
				"sources/memo/memo.docx":    "docx",
				"sources/wide/wide.docx":    "wide docx",
				"sources/wide/options.json": tc.options,
			})
			writeFiles(t, root, map[string]string{"pdfs/memo.pdf": "pdf memo"})
			man := referenceManifest{
				References: map[string]referenceRecord{"memo": {SourceSHA256: sha("docx"), PDFSHA256: sha("pdf memo"), Image: microsoftRenderer}},
				// The refusal an earlier build recorded for the borrower
				// is no stale reference.
				Failed: map[string]referenceFailure{"wide": {Status: "0", Answer: "Microsoft Graph converts with no options"}},
			}
			data, err := json.Marshal(man)
			if err != nil {
				t.Fatal(err)
			}
			writeFiles(t, root, map[string]string{"references/manifest.json": string(data)})
			f := newDiffFeature()
			f.m = &Manifest{Name: "test/docx", References: &ReferenceSpec{Dir: "references", Engine: engineMicrosoft}}
			f.testdataRoot, f.office, f.pins = root, true, map[string]string{}
			f.goldens = map[string]*diffGolden{"memo": {}}
			err = f.Prepare(t.Context(), []Fixture{
				{ID: "memo", PDFPath: filepath.Join(src, "sources", "memo")},
				{ID: "wide", PDFPath: filepath.Join(src, "sources", "wide")},
			})
			if tc.reason != "" {
				if err == nil || !errors.Is(err, errBorrowedReference) || !strings.Contains(err.Error(), tc.reason) {
					t.Errorf("Prepare error %v, want one naming %q", err, tc.reason)
				}
				return
			}
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if !f.HasGolden("wide") || f.golden("wide") != f.golden("memo") {
				t.Error("the borrower is not scored against memo's goldens")
			}
		})
	}

	chained := []Fixture{}
	src := t.TempDir()
	for name, options := range map[string]string{"a": `{"reference_of": "b"}`, "b": `{"reference_of": "c"}`, "c": `{}`} {
		writeFiles(t, src, map[string]string{name + "/" + name + ".docx": "docx", name + "/options.json": options})
		chained = append(chained, Fixture{ID: name, PDFPath: filepath.Join(src, name)})
	}
	if _, err := planReferences(chained); err == nil || !strings.Contains(err.Error(), "borrows a reference too") {
		t.Errorf("a chain of borrowers: %v", err)
	}
}
