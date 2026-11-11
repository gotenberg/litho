package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// planSources writes one source directory per entry of sources under a new
// directory, each a map of file names to contents, and returns their
// fixtures, keyed on the directory name, in name order.
func planSources(t *testing.T, sources map[string]map[string]string) []Fixture {
	t.Helper()
	root := t.TempDir()
	var fixtures []Fixture
	for _, id := range slices.Sorted(maps.Keys(sources)) {
		files := map[string]string{}
		for name, content := range sources[id] {
			files[id+"/"+name] = content
		}
		writeFiles(t, root, files)
		fixtures = append(fixtures, Fixture{ID: id, PDFPath: filepath.Join(root, id)})
	}
	return fixtures
}

// TestPlanReferences_ReadsEitherKindOfSource checks that an HTML, a
// Markdown and an Office source borrow a reference by the same rule, that a
// source setting litho's own options without reference_of is listed with
// them, and that a reference_of naming itself, no fixture, a borrower or a
// source without a reference fails, whatever the kind.
func TestPlanReferences_ReadsEitherKindOfSource(t *testing.T) {
	t.Parallel()

	fixtures := planSources(t, map[string]map[string]string{
		"page":        {"index.html": "<p>x</p>"},
		"page-styled": {"index.html": "<p>x</p>", "user.css": "p {}", "options.json": `{"userStylesheets": ["user.css"], "reference_of": "page"}`},
		"styled":      {"index.html": "<p>x</p>", "user.css": "p {}", "options.json": `{"userStylesheets": ["user.css"]}`},
		"notes":       {"index.md": "# x", "page.tmpl": "{{ toHTML }}", "options.json": `{"markdownTemplate": "page.tmpl", "reference_of": "page"}`},
		"memo":        {"memo.docx": "PK"},
		"memo-marked": {"memo.docx": "PK", "options.json": `{"trackedChanges": "marked", "reference_of": "memo"}`},
		"draft":       {"draft.docx": "PK", "options.json": `{"trackedChanges": "marked", "date": "2026-11-11T00:00:00Z"}`},
		"accepted":    {"memo.docx": "PK", "options.json": `{"trackedChanges": "accepted"}`},
	})
	p, err := planReferences(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"page-styled": "page", "notes": "page", "memo-marked": "memo"}; !maps.Equal(p.borrowed, want) {
		t.Errorf("borrowed %v, want %v", p.borrowed, want)
	}
	if want := map[string][]string{"styled": {"userStylesheets"}, "draft": {"trackedChanges", "date"}}; !maps.EqualFunc(p.lithoOnly, want, slices.Equal) {
		t.Errorf("litho only %v, want %v", p.lithoOnly, want)
	}
	for _, id := range []string{"page-styled", "styled", "notes", "memo-marked", "draft"} {
		if !p.ownsNoReference(id) {
			t.Errorf("%s owns a reference", id)
		}
	}
	for _, id := range []string{"page", "memo", "accepted"} {
		if p.ownsNoReference(id) {
			t.Errorf("%s owns no reference", id)
		}
	}
	notice := p.lithoOnlyNotice("probe/html", fixtures)
	for _, want := range []string{"probe/html: 2 fixtures", "draft (trackedChanges, date)", "styled (userStylesheets)", "not scored"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q does not name %q", notice, want)
		}
	}
	if notice := (referencePlan{}).lithoOnlyNotice("probe/html", fixtures); notice != "" {
		t.Errorf("a plan without litho-only fixtures notices %q", notice)
	}

	for _, tc := range []struct {
		name    string
		sources map[string]map[string]string
		reason  string
	}{
		{"itself", map[string]map[string]string{
			"page": {"index.html": "x", "options.json": `{"reference_of": "page"}`},
		}, "not another fixture"},
		{"no such fixture", map[string]map[string]string{
			"notes": {"index.md": "x", "options.json": `{"reference_of": "gone"}`},
		}, "not another fixture"},
		{"a borrower", map[string]map[string]string{
			"a": {"index.html": "x", "options.json": `{"reference_of": "b"}`},
			"b": {"index.md": "x", "options.json": `{"reference_of": "c"}`},
			"c": {"index.html": "x"},
		}, "borrows a reference too"},
		{"a source without a reference", map[string]map[string]string{
			"a": {"index.html": "x", "options.json": `{"reference_of": "b"}`},
			"b": {"index.html": "x", "user.css": "", "options.json": `{"userStylesheets": ["user.css"]}`},
		}, "has no reference: it sets userStylesheets"},
		{"an Office source without a reference", map[string]map[string]string{
			"a": {"a.pptx": "PK", "options.json": `{"reference_of": "b"}`},
			"b": {"b.pptx": "PK", "options.json": `{"handouts": 3}`},
		}, "has no reference: it sets handouts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := planReferences(planSources(t, tc.sources))
			if !errors.Is(err, errBorrowedReference) || !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("planReferences error %v, want one naming %q", err, tc.reason)
			}
		})
	}
}

// chromiumReference returns the record of a reference built for the HTML
// source dir at image, with the PDF pdf.
func chromiumReference(t *testing.T, dir, image, pdf string) referenceRecord {
	t.Helper()
	src, opt, err := sourceSums(engineChromium, dir)
	if err != nil {
		t.Fatal(err)
	}
	return referenceRecord{SourceSHA256: src, OptionsSHA256: opt, PDFSHA256: sha256Hex(pdf), Image: image}
}

// sha256Hex returns the hex SHA-256 of s.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestDiffFeature_HTMLSourcesBorrowAndGoUnscored checks the diff cell of an
// HTML corpus: a source naming reference_of is scored against the named
// fixture's goldens and needs no reference of its own, and a source setting
// litho's own options without one has no golden, even one left from an
// earlier reference, and fails no check for want of a reference.
func TestDiffFeature_HTMLSourcesBorrowAndGoUnscored(t *testing.T) {
	t.Parallel()

	fixtures := planSources(t, map[string]map[string]string{
		"page":        {"index.html": "<p>x</p>"},
		"page-styled": {"index.html": "<p>x</p>", "user.css": "p {}", "options.json": `{"userStylesheets": ["user.css"], "reference_of": "page"}`},
		"styled":      {"index.html": "<p>x</p>", "user.css": "p {}", "options.json": `{"userStylesheets": ["user.css"]}`},
	})
	root := t.TempDir()
	man := referenceManifest{References: map[string]referenceRecord{"page": chromiumReference(t, fixtures[0].PDFPath, "img:1", "pdf page")}}
	data, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, root, map[string]string{"references/manifest.json": string(data), "pdfs/page.pdf": "pdf page"})
	f := newDiffFeature()
	f.m = &Manifest{Name: "probe/html", References: &ReferenceSpec{Dir: "references", Engine: engineChromium}}
	f.testdataRoot, f.pins = root, map[string]string{"GOTENBERG_IMAGE": "img:1"}
	f.goldens = map[string]*diffGolden{"page": {}, "styled": {}}
	if err := f.Prepare(t.Context(), fixtures); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !f.HasGolden("page-styled") || f.golden("page-styled") != f.golden("page") {
		t.Error("the borrower is not scored against page's goldens")
	}
	if f.HasGolden("styled") {
		t.Error("the source setting litho's own options is scored")
	}
}

// TestReferenceProblems_RecordsLithoOnlySources checks that a source
// setting litho's own options is never stale: it asks for a build only
// while the reference manifest does not record it, for its current source
// and options, as a source without a reference.
func TestReferenceProblems_RecordsLithoOnlySources(t *testing.T) {
	t.Parallel()

	fixtures := planSources(t, map[string]map[string]string{
		"draft": {"draft.docx": "PK", "options.json": `{"trackedChanges": "marked"}`},
	})
	src, opt, err := sourceSums(engineMicrosoft, fixtures[0].PDFPath)
	if err != nil {
		t.Fatal(err)
	}
	recorded := referenceFailure{SourceSHA256: src, OptionsSHA256: opt, Status: "0", LithoOptions: []string{"trackedChanges"}}
	otherOptions := recorded
	otherOptions.OptionsSHA256 = "other"
	otherKeys := recorded
	otherKeys.LithoOptions = []string{"date"}
	for _, tc := range []struct {
		name    string
		man     referenceManifest
		rebuild bool
	}{
		{"no record", referenceManifest{}, true},
		{"recorded", referenceManifest{Failed: map[string]referenceFailure{"draft": recorded}}, false},
		{"recorded for other options", referenceManifest{Failed: map[string]referenceFailure{"draft": otherOptions}}, true},
		{"recorded for other keys", referenceManifest{Failed: map[string]referenceFailure{"draft": otherKeys}}, true},
		{"a reference built before", referenceManifest{
			References: map[string]referenceRecord{"draft": {SourceSHA256: src, Image: microsoftRenderer}},
			Failed:     map[string]referenceFailure{"draft": recorded},
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			data, err := json.Marshal(tc.man)
			if err != nil {
				t.Fatal(err)
			}
			writeFiles(t, root, map[string]string{"references/manifest.json": string(data)})
			m := &Manifest{Name: "probe/docx", References: &ReferenceSpec{Dir: "references", Engine: engineMicrosoft}}
			check, err := referenceProblems(m, root, fixtures, map[string]string{})
			if err != nil {
				t.Fatal(err)
			}
			if len(check.stale) > 0 || len(check.refused) > 0 || len(check.regolden) > 0 {
				t.Errorf("%+v, want no stale, refused or regolden fixture", check)
			}
			if got := slices.Equal(check.rebuild, []string{"draft"}); got != tc.rebuild {
				t.Errorf("rebuild %v, want a build %v", check.rebuild, tc.rebuild)
			}
		})
	}
}

// recordingConverter converts every source into a one-page PDF and records
// the names it was asked for.
type recordingConverter struct {
	mu    sync.Mutex
	names []string
}

func (c *recordingConverter) convert(_ context.Context, _ string, j referenceJob) (int, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.names = append(c.names, j.name)
	return 200, []byte("%PDF-1.7 " + j.name), nil
}

func (c *recordingConverter) builder() string                        { return "img:1" }
func (c *recordingConverter) version(context.Context, string) string { return "" }
func (c *recordingConverter) close()                                 {}

// stubCorpus writes the sources of a corpus named name under dir, laid out
// as the repository lays them out, and returns its manifest.
func stubCorpus(t *testing.T, dir, name, engine string, sources map[string]map[string]string) *Manifest {
	t.Helper()
	files := map[string]string{}
	for id, source := range sources {
		for file, content := range source {
			files["test/integration/corpus/"+name+"/sources/"+id+"/"+file] = content
		}
	}
	writeFiles(t, dir, files)
	if err := os.MkdirAll(filepath.Join(dir, "test", "integration", "testdata", filepath.FromSlash(name)), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Manifest{
		Name: name, Source: name,
		Inputs:     &InputSpec{Glob: "sources/*"},
		References: &ReferenceSpec{Dir: "references", Engine: engine},
	}
}

// readReferenceManifest reads the reference manifest of m.
func readReferenceManifest(t *testing.T, m *Manifest) referenceManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.TestdataRoot(), m.References.Dir, referenceManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var man referenceManifest
	if err := json.Unmarshal(data, &man); err != nil {
		t.Fatal(err)
	}
	return man
}

// The sources of each engine's stub corpus: one with a reference, one that
// borrows it and one that sets litho's own options without a reference.
//
//nolint:gochecknoglobals // static test table
var lithoOnlySources = map[string]map[string]map[string]string{
	engineChromium: {
		"page":        {"index.html": "<p>x</p>"},
		"page-styled": {"index.html": "<p>x</p>", "user.css": "p {}", "options.json": `{"userStylesheets": ["user.css"], "reference_of": "page"}`},
		"styled":      {"index.html": "<p>x</p>", "user.css": "p {}", "options.json": `{"userStylesheets": ["user.css"]}`},
	},
	engineLibreOffice: {
		"page":        {"page.ods": "PK"},
		"page-styled": {"page.ods": "PK", "options.json": `{"sheets": ["One"], "reference_of": "page"}`},
		"styled":      {"page.ods": "PK", "options.json": `{"recalculation": "always"}`},
	},
	engineMicrosoft: {
		"page":        {"page.docx": "PK"},
		"page-styled": {"page.docx": "PK", "options.json": `{"trackedChanges": "rejected", "reference_of": "page"}`},
		"styled":      {"page.docx": "PK", "options.json": `{"trackedChanges": "marked"}`},
	},
}

// Not parallel: it changes the working directory, where the corpus and its
// testdata are found.
//
// TestBuildReferences_RecordsLithoOnlySourcesWithoutAnEngine checks, for
// each engine, that a build converts the source with a reference alone,
// records the source setting litho's own options as having none, removing
// a reference built for it before, and that a build of that source alone
// starts no converter.
func TestBuildReferences_RecordsLithoOnlySourcesWithoutAnEngine(t *testing.T) { //nolint:paralleltest // t.Chdir
	for _, engine := range []string{engineChromium, engineLibreOffice, engineMicrosoft} {
		t.Run(engine, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			m := stubCorpus(t, dir, "stub/"+engine, engine, lithoOnlySources[engine])
			writeFiles(t, m.TestdataRoot(), map[string]string{"references/styled.pdf": "%PDF- built before"})
			pins := &resolvedPins{Values: map[string]string{}}
			conv := &recordingConverter{}
			if err := buildReferences(t.Context(), func(context.Context) (referenceConverter, error) { return conv, nil }, m, pins, nil); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(conv.names, []string{"page"}) {
				t.Errorf("converted %v, want page alone", conv.names)
			}
			man := readReferenceManifest(t, m)
			fail, ok := man.Failed["styled"]
			if !ok || hasKey(man.References, "styled") || len(fail.LithoOptions) != 1 || fail.Status != "0" || !strings.Contains(fail.Answer, "litho's own") {
				t.Errorf("styled recorded as %+v, want a failure naming its litho option", fail)
			}
			if _, err := os.Stat(filepath.Join(m.TestdataRoot(), "references", "styled.pdf")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the reference built before the option stays: %v", err)
			}
			if hasKey(man.References, "page-styled") || hasKey(man.Failed, "page-styled") {
				t.Error("the borrower has a reference of its own")
			}

			noEngine := func(context.Context) (referenceConverter, error) {
				return nil, errors.New("no converter may start")
			}
			if err := buildReferences(t.Context(), noEngine, m, pins, []string{"styled"}); err != nil {
				t.Fatalf("a build of the litho-only source alone: %v", err)
			}
			if again := readReferenceManifest(t, m); !hasKey(again.References, "page") || !hasKey(again.Failed, "styled") {
				t.Errorf("the second build lost an entry: %+v", again)
			}
		})
	}
}

// Not parallel: it changes the working directory and the environment.
//
// TestPrepareReferences_RecordsLithoOnlySourcesWithoutAnEngine checks, for
// each engine and without a Microsoft sign-in or a pinned image, that
// prepare records a new source setting litho's own options without asking
// any converter, and finds the references up to date once it has.
func TestPrepareReferences_RecordsLithoOnlySourcesWithoutAnEngine(t *testing.T) { //nolint:paralleltest // t.Chdir, t.Setenv
	t.Setenv(msGraphTokenEnv, "")
	t.Setenv(msGraphClientIDEnv, "")
	for _, engine := range []string{engineChromium, engineLibreOffice, engineMicrosoft} {
		t.Run(engine, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			m := stubCorpus(t, dir, "stub/"+engine, engine, lithoOnlySources[engine])
			pins := &resolvedPins{Values: map[string]string{}}
			src, opt, err := sourceSums(engine, filepath.Join(m.SourceDir(), "sources", "page"))
			if err != nil {
				t.Fatal(err)
			}
			man := referenceManifest{Format: 1, References: map[string]referenceRecord{
				"page": {SourceSHA256: src, OptionsSHA256: opt, PDFSHA256: sha256Hex("%PDF- page"), Image: referenceBuilder(engine, pins.Values)},
			}}
			data, err := json.Marshal(man)
			if err != nil {
				t.Fatal(err)
			}
			writeFiles(t, m.TestdataRoot(), map[string]string{"references/manifest.json": string(data), "references/page.pdf": "%PDF- page"})

			if err := prepareReferences(t.Context(), []*Manifest{m}, pins); err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if got, want := readReferenceManifest(t, m), lithoOnlyKeys(t, m); len(want) == 0 || !slices.Equal(got.Failed["styled"].LithoOptions, want) {
				t.Errorf("styled recorded as %+v", got.Failed["styled"])
			}
			fixtures, err := loadInputFixtures(m)
			if err != nil {
				t.Fatal(err)
			}
			check, err := referenceProblems(m, m.TestdataRoot(), fixtures, pins.Values)
			if err != nil || len(check.rebuild) > 0 || len(check.stale) > 0 {
				t.Errorf("after prepare: %+v, %v; want nothing to build", check, err)
			}
			if err := prepareReferences(t.Context(), []*Manifest{m}, pins); err != nil {
				t.Fatalf("prepare again: %v", err)
			}
		})
	}
}

// lithoOnlyKeys returns the litho-only keys of the fixture styled of m.
func lithoOnlyKeys(t *testing.T, m *Manifest) []string {
	t.Helper()
	fixtures, err := loadInputFixtures(m)
	if err != nil {
		t.Fatal(err)
	}
	p, err := planReferences(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	return p.lithoOnly["styled"]
}
