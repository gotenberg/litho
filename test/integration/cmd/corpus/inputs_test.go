package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/test/integration/fixture"
)

func TestGlobInputFixtures_KeysOnTheWildcard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"sources/invoice/index.html": "",
		"sources/landing/index.html": "",
		"sources/landing/style.css":  "",
		"sources/empty/readme.txt":   "",
	})
	got, err := globInputFixtures(dir, "sources/*/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if ids := compareIDs(got, []string{"invoice", "landing"}); ids != "" {
		t.Error(ids)
	}
	for _, bad := range []string{"sources/index.html", "sources/*/*.html", "sources/?/index.html"} {
		if _, err := globInputFixtures(dir, bad); err == nil {
			t.Errorf("glob %q accepted", bad)
		}
	}
}

func TestListInputFixtures_NamesTheMakeTargetForMissingFiles(t *testing.T) {
	t.Parallel()

	src, root := t.TempDir(), t.TempDir()
	writeFiles(t, src, map[string]string{"list.json": `[{"id": "css/a.html"}, {"id": "css/b.html"}]`})
	writeFiles(t, root, map[string]string{"css/a.html": "", "css/b.html": ""})
	got, err := listInputFixtures(filepath.Join(src, "list.json"), root, "make corpus-fetch CORPUS_NAME=wpt")
	if err != nil {
		t.Fatal(err)
	}
	if ids := compareIDs(got, []string{"css/a.html", "css/b.html"}); ids != "" {
		t.Error(ids)
	}

	writeFiles(t, src, map[string]string{"list.json": `[{"id": "css/a.html"}, {"id": "css/c.html"}]`})
	_, err = listInputFixtures(filepath.Join(src, "list.json"), root, "make corpus-fetch CORPUS_NAME=wpt")
	if !errors.Is(err, errInputsUnfetched) || !strings.Contains(err.Error(), "make corpus-fetch CORPUS_NAME=wpt") {
		t.Errorf("missing source: %v", err)
	}

	for _, bad := range []string{`[{"id": "../x"}]`, `[{"id": "/x"}]`, `[{"id": "a"}, {"id": "a"}]`} {
		writeFiles(t, src, map[string]string{"list.json": bad})
		if _, err := listInputFixtures(filepath.Join(src, "list.json"), root, ""); err == nil {
			t.Errorf("list %s accepted", bad)
		}
	}
}

// TestWPTListAndHashesAgree pins that every file a reftest of
// corpus/wpt/reftests.json reads is in corpus/wpt/files.sha256, which is
// what `corpus fetch` downloads, and that the list holds the 300 to 1300 tests
// the corpus is sized for.
func TestWPTListAndHashesAgree(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(corpusSourceDir, "wpt")
	data, err := os.ReadFile(filepath.Join(dir, "reftests.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tests []*reftestEntry
	if err := json.Unmarshal(data, &tests); err != nil {
		t.Fatal(err)
	}
	if len(tests) < 300 || len(tests) > 1300 {
		t.Errorf("%d reftests, want 300 to 1300", len(tests))
	}
	hashes, err := os.ReadFile(filepath.Join(dir, "files.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	listed := make(map[string]bool)
	sc := bufio.NewScanner(bytes.NewReader(hashes))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 || len(fields[0]) != 64 {
			t.Fatalf("files.sha256: malformed line %q", sc.Text())
		}
		listed[fields[1]] = true
	}
	need := func(p string) {
		if !listed[p] {
			t.Errorf("%s is read by a reftest but not in files.sha256", p)
		}
	}
	need("fonts/Ahem.ttf")
	for _, e := range tests {
		need(e.ID)
		for _, r := range e.Resources {
			need(r)
		}
		for _, ref := range e.Refs {
			need(ref.Path)
			for _, r := range ref.Resources {
				need(r)
			}
		}
	}
}

// TestCommittedConverterCorporaLoad loads the weasyprint fixtures and the
// html sources the way the runner does, so a fixture the draw feature cannot
// read or a source directory without its Chromium reference fails here
// rather than in a corpus run.
func TestCommittedConverterCorporaLoad(t *testing.T) {
	t.Parallel()

	draw := filepath.Join(corpusSourceDir, "weasyprint")
	fixtures, err := globInputFixtures(draw, "fixtures/*.json")
	if err != nil {
		t.Fatal(err)
	}
	f := newDrawFeature()
	for _, fx := range fixtures {
		data, err := os.ReadFile(fx.PDFPath)
		if err != nil {
			t.Fatal(err)
		}
		var d drawFixture
		if err := json.Unmarshal(data, &d); err != nil {
			t.Fatalf("%s: %v", fx.ID, err)
		}
		if _, _, err := d.grid(); err != nil {
			t.Errorf("%s: %v", fx.ID, err)
		}
		f.fixtures[fx.ID] = &d
	}
	if len(f.fixtures) < 300 {
		t.Errorf("%d weasyprint fixtures", len(f.fixtures))
	}
	if _, err := os.Stat(filepath.Join(draw, "ua.css")); err != nil {
		t.Error(err)
	}

	var sources []Fixture
	for _, corpus := range []string{"litho/html", "gotenberg/html"} {
		dir := filepath.Join(corpusSourceDir, filepath.FromSlash(corpus))
		found, err := globInputFixtures(dir, "sources/*")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range found {
			if _, err := os.Stat(filepath.Join(dir, "references", s.ID+".pdf")); err != nil {
				t.Errorf("%s source %s has no Chromium reference: run make html-references", corpus, s.ID)
			}
		}
		sources = append(sources, found...)
	}
	for _, s := range sources {
		source, err := fixture.Load(s.PDFPath)
		if err != nil {
			t.Errorf("source %s: %v", s.ID, err)
			continue
		}
		src, err := os.ReadFile(source.Document)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(bytes.ToLower(src), []byte("<script")) {
			t.Errorf("source %s holds a script", s.ID)
		}
	}
}

// TestHostProvisionedCorporaReadTheirPin checks that a corpus the host
// provisions fetches its files at the pin sourcePins says they come from, as
// TestEverySourceReadsItsPin checks of download.sh, and that every fetch names
// a pin of versions.env.
func TestHostProvisionedCorporaReadTheirPin(t *testing.T) {
	t.Parallel()

	f, err := os.Open(filepath.Join(corpusSourceDir, versionsFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	entries, err := parsePins(f)
	if err != nil {
		t.Fatal(err)
	}
	pinned := map[string]bool{}
	for _, e := range entries {
		pinned[e.Key] = true
	}
	checked := 0
	for _, m := range readManifests(t) {
		if m.Fetch != nil && !pinned[m.Fetch.Pin] {
			t.Errorf("%s fetches at %s, which versions.env does not pin", m.Name, m.Fetch.Pin)
		}
		if m.ProvisionedBy == "" {
			continue
		}
		checked++
		if keys := sourcePins[m.Source]; len(keys) > 0 && (m.Fetch == nil || !slices.Equal(keys, []string{m.Fetch.Pin})) {
			t.Errorf("%s: its sources come from %v, its fetch reads %+v", m.Name, keys, m.Fetch)
		}
		if _, err := os.Stat(filepath.Join(corpusSourceDir, m.Source, "download.sh")); err == nil {
			t.Errorf("%s is provisioned on the host but ships a download.sh the image would run", m.Name)
		}
	}
	if checked != 3 {
		t.Errorf("checked %d host-provisioned corpora, want wpt, weasyprint and resvg", checked)
	}
}

func TestLoadInputFixtures_GlobsAndListsTogether(t *testing.T) {
	t.Parallel()

	src, root := t.TempDir(), t.TempDir()
	writeFiles(t, src, map[string]string{
		"sources/memo/memo.docx": "",
		"documents.json":         `[{"id": "export/a.docx"}]`,
	})
	writeFiles(t, root, map[string]string{"sources/export/a.docx": ""})
	m := &Manifest{Name: "office", Inputs: &InputSpec{Glob: "sources/*", List: "documents.json", ListRoot: "sources", FetchedBy: "make corpus-fetch"}}
	globbed, err := globInputFixtures(src, m.Inputs.Glob)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := listInputFixtures(filepath.Join(src, m.Inputs.List), filepath.Join(root, m.Inputs.ListRoot), m.Inputs.FetchedBy)
	if err != nil {
		t.Fatal(err)
	}
	if ids := compareIDs(append(globbed, listed...), []string{"memo", "export/a.docx"}); ids != "" {
		t.Error(ids)
	}
	if err := os.Remove(filepath.Join(root, "sources", "export", "a.docx")); err != nil {
		t.Fatal(err)
	}
	_, err = listInputFixtures(filepath.Join(src, m.Inputs.List), filepath.Join(root, m.Inputs.ListRoot), m.Inputs.FetchedBy)
	if !errors.Is(err, errInputsUnfetched) || !strings.Contains(err.Error(), "make corpus-fetch") {
		t.Errorf("missing fetched source: %v", err)
	}
}

// TestOfficeListsAndHashesAgree pins that every document the Office
// libreoffice corpora's documents.json lists is in their files.sha256, which
// is what `corpus fetch` downloads, with the licence, that the IDs name
// the directories, extensions and caps the libreoffice selector takes,
// and that the lists hold the documents the caps allow.
func TestOfficeListsAndHashesAgree(t *testing.T) {
	t.Parallel()

	// The caps of the libreoffice selector (libreOfficeDirs), by LibreOffice core
	// directory.
	caps := map[string]map[string]int{
		"docx": {"sw/qa/extras/ooxmlexport/data": 250, "sw/qa/extras/ooxmlimport/data": 60, "sw/qa/extras/layout/data": 40},
		"odt":  {"sw/qa/extras/odfexport/data": 80, "sw/qa/extras/odfimport/data": 40, "sw/qa/extras/layout/data": 30},
		"xlsx": {"sc/qa/unit/data/xlsx": 220, "chart2/qa/extras/data/xlsx": 60},
		"ods":  {"sc/qa/unit/data/ods": 200, "sc/qa/unit/data/fods": 16, "chart2/qa/extras/data/ods": 40},
		"csv":  {"sc/qa/unit/data/csv": 9},
		"pptx": {"sd/qa/unit/data/pptx": 200, "oox/qa/unit/data": 61, "chart2/qa/extras/data/pptx": 38, "sd/qa/unit/data": 20},
		"odp":  {"sd/qa/unit/data/odp": 150, "sd/qa/unit/data": 20},
	}
	exts := map[string][]string{
		"docx": {"docx"}, "odt": {"odt"}, "xlsx": {"xlsx"}, "ods": {"ods", "fods"}, "csv": {"csv"},
		"pptx": {"pptx"}, "odp": {"odp", "fodp"},
	}
	// corePath is the LibreOffice core path of a fixture ID: a Writer test
	// directory's name, the module of a Calc, Impress or chart test
	// directory named by the extension (Impress keeps its flat
	// presentations beside the packaged ones), the DrawingML tests' module,
	// or sd-data for the mixed Impress test directory itself.
	corePath := func(sub, name string) string {
		ext := name[strings.LastIndexByte(name, '.')+1:]
		switch sub {
		case "sc":
			return "sc/qa/unit/data/" + ext + "/" + name
		case "sd":
			return "sd/qa/unit/data/" + strings.TrimPrefix(ext, "f") + "/" + name
		case "sd-data":
			return "sd/qa/unit/data/" + name
		case "oox":
			return "oox/qa/unit/data/" + name
		case "chart2":
			return "chart2/qa/extras/data/" + ext + "/" + name
		}
		return "sw/qa/extras/" + sub + "/data/" + name
	}
	for corpus, dirs := range caps {
		dir := filepath.Join(corpusSourceDir, "libreoffice", corpus)
		data, err := os.ReadFile(filepath.Join(dir, "documents.json"))
		if err != nil {
			t.Fatal(err)
		}
		var docs []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &docs); err != nil {
			t.Fatal(err)
		}
		hashes, err := os.ReadFile(filepath.Join(dir, "files.sha256"))
		if err != nil {
			t.Fatal(err)
		}
		listed := make(map[string]bool)
		sc := bufio.NewScanner(bytes.NewReader(hashes))
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) != 2 || len(fields[0]) != 64 {
				t.Fatalf("%s files.sha256: malformed line %q", corpus, sc.Text())
			}
			listed[fields[1]] = true
		}
		if !listed["COPYING.MPL"] {
			t.Errorf("%s files.sha256 lacks the licence", corpus)
		}
		per := make(map[string]int)
		for _, d := range docs {
			sub, name, ok := strings.Cut(d.ID, "/")
			ext := name[strings.LastIndexByte(name, '.')+1:]
			if !ok || !validInputID(d.ID) || !strings.Contains(name, ".") || !slices.Contains(exts[corpus], ext) {
				t.Errorf("%s: id %q is not <dir>/<file>.%v", corpus, d.ID, exts[corpus])
				continue
			}
			p := corePath(sub, name)
			per[p[:strings.LastIndexByte(p, '/')]]++
			if !listed[p] {
				t.Errorf("%s: %s is listed but not in files.sha256", corpus, p)
			}
		}
		if len(listed) != len(docs)+1 {
			t.Errorf("%s: files.sha256 holds %d files, documents.json %d documents and the licence", corpus, len(listed), len(docs))
		}
		for sub, n := range per {
			if limit, ok := dirs[sub]; !ok || n > limit {
				t.Errorf("%s: %d documents from %s, cap %d", corpus, n, sub, limit)
			}
		}
	}
}

// TestCommittedOfficeSourcesLoad loads every gotenberg Office source as the
// diff feature does: one document of its corpus's format and valid options.
func TestCommittedOfficeSourcesLoad(t *testing.T) {
	t.Parallel()

	formats := map[string]pdf.Format{
		"docx": pdf.FormatDOCX, "odt": pdf.FormatODT, "xlsx": pdf.FormatXLSX, "ods": pdf.FormatODS, "csv": pdf.FormatCSV,
		"pptx": pdf.FormatPPTX, "odp": pdf.FormatODP,
	}
	for corpus, want := range formats {
		sources, err := globInputFixtures(filepath.Join(corpusSourceDir, "gotenberg", corpus), "sources/*")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range sources {
			o, err := fixture.LoadOffice(s.PDFPath)
			if err != nil {
				t.Errorf("%s %s: %v", corpus, s.ID, err)
				continue
			}
			if o.Format != want {
				t.Errorf("%s %s: format %s", corpus, s.ID, o.Format)
			}
			if info, err := os.Stat(o.Document); err != nil || info.Size() > 200<<10 {
				t.Errorf("%s %s: %v, over 200 KiB or unreadable", corpus, s.ID, err)
			}
		}
	}
}

// TestFetchedOfficeListsAgree pins that the poi, openxmlsdk and govdocs
// Office corpora list each document once, by a plain ASCII ID of the
// corpus's format and the path its source's fetch script downloads, that
// files.sha256 holds exactly those paths and the source's licence files, and
// that the manifest names the pin the documents come from.
func TestFetchedOfficeListsAgree(t *testing.T) {
	t.Parallel()

	plain := regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	sources := map[string]struct {
		pin     string
		formats map[string][]string
		licence []string
	}{
		"poi": {"POI_COMMIT", map[string][]string{
			"docx": {".docx", ".dotx"}, "xlsx": {".xlsx", ".xltx"}, "pptx": {".pptx", ".potx"},
			"doc": {".doc"}, "xls": {".xls"}, "ppt": {".ppt"},
		}, []string{"legal/LICENSE", "legal/NOTICE"}},
		"openxmlsdk": {"OPENXMLSDK_COMMIT", map[string][]string{
			"docx": {".docx", ".dotx"}, "xlsx": {".xlsx", ".xltx"}, "pptx": {".pptx", ".potx"},
		}, []string{"LICENSE"}},
		"govdocs": {"GOVDOCS_THREADS", map[string][]string{"doc": {".doc"}, "xls": {".xls"}, "ppt": {".ppt"}}, nil},
	}
	for source, s := range sources {
		for format, exts := range s.formats {
			dir := filepath.Join(corpusSourceDir, source, format)
			data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var m Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			if m.References == nil || m.References.SourcePin != s.pin {
				t.Errorf("%s/%s: references %+v, want source_pin %s", source, format, m.References, s.pin)
			}
			if data, err = os.ReadFile(filepath.Join(dir, "documents.json")); err != nil {
				t.Fatal(err)
			}
			var docs []struct {
				ID   string `json:"id"`
				Path string `json:"path"`
			}
			if err := json.Unmarshal(data, &docs); err != nil {
				t.Fatal(err)
			}
			if len(docs) == 0 {
				t.Errorf("%s/%s lists no document", source, format)
			}
			hashes, err := os.ReadFile(filepath.Join(dir, "files.sha256"))
			if err != nil {
				t.Fatal(err)
			}
			listed := make(map[string]bool)
			for line := range strings.SplitSeq(strings.TrimSpace(string(hashes)), "\n") {
				digest, path, ok := strings.Cut(line, "  ")
				if !ok || len(digest) != 64 || listed[path] {
					t.Fatalf("%s/%s files.sha256: malformed or repeated line %q", source, format, line)
				}
				listed[path] = true
			}
			ids := make(map[string]bool)
			for _, d := range docs {
				ext := strings.ToLower(filepath.Ext(d.ID))
				if !plain.MatchString(d.ID) || !validInputID(d.ID) || !slices.Contains(exts, ext) || ids[d.ID] {
					t.Errorf("%s/%s: id %q is not a plain, unique ID of %v", source, format, d.ID, exts)
				}
				ids[d.ID] = true
				if !listed[d.Path] {
					t.Errorf("%s/%s: %s is listed but not in files.sha256", source, format, d.Path)
				}
				delete(listed, d.Path)
			}
			for _, l := range s.licence {
				if !listed[l] {
					t.Errorf("%s/%s files.sha256 lacks %s", source, format, l)
				}
				delete(listed, l)
			}
			if len(listed) != 0 {
				t.Errorf("%s/%s files.sha256 holds %d files no document names", source, format, len(listed))
			}
		}
	}
}

// TestFetchListsAgree pins that every corpus that declares a fetch and a
// documents list lists each document once, by a valid ID, at a path its
// files.sha256 hashes, and that a PDF corpus's fixtures are PDFs.
func TestFetchListsAgree(t *testing.T) {
	t.Parallel()

	checked := 0
	for _, m := range readManifests(t) {
		list, _ := m.listAndRoot()
		// A corpus whose paths a map rewrites (wpt, resvg) lists tests, not
		// fetched documents.
		if m.Fetch == nil || list == "" || len(m.Fetch.Map) > 0 {
			continue
		}
		checked++
		dir := filepath.Join(corpusSourceDir, filepath.FromSlash(m.Source))
		docs, err := readDocumentList(filepath.Join(dir, list))
		if err != nil {
			t.Fatal(err)
		}
		hashes, err := readHashList(filepath.Join(dir, "files.sha256"))
		if err != nil {
			t.Fatal(err)
		}
		listed := map[string]bool{}
		for _, h := range hashes {
			listed[h.path] = true
		}
		ids := map[string]bool{}
		for _, d := range docs {
			if !validInputID(d.ID) || ids[d.ID] || !listed[d.Path] {
				t.Errorf("%s: %q (%s) is not a unique, valid ID of a hashed path", m.Name, d.ID, d.Path)
			}
			if m.Inputs == nil && !strings.HasSuffix(strings.ToLower(d.ID), ".pdf") {
				t.Errorf("%s: %q is not a PDF", m.Name, d.ID)
			}
			if d.Optional && d.URL == "" {
				t.Errorf("%s: %q is optional without a URL", m.Name, d.ID)
			}
			ids[d.ID] = true
		}
	}
	if checked < 20 {
		t.Errorf("checked %d fetched lists, want the Office and PDF corpora", checked)
	}
}
