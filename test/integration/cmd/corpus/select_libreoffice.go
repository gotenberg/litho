package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// loDir is one LibreOffice core test data directory a libreoffice corpus
// reads, and the most documents it keeps, so the export tests, which
// outnumber the rest, do not crowd out the import and layout tests. exts,
// when set, are the only extensions read there: a Writer directory is read
// once per extension, so the flat documents have caps of their own and do
// not displace the packaged ones.
type loDir struct {
	path string
	cap  int
	exts []string
}

// libreOfficeDirs lists, per corpus, the directories read in order.
//
//nolint:gochecknoglobals // static table
var libreOfficeDirs = map[string][]loDir{
	"docx": {{"sw/qa/extras/ooxmlexport/data", 250, nil}, {"sw/qa/extras/ooxmlimport/data", 60, nil}, {"sw/qa/extras/layout/data", 40, nil}},
	"odt": {
		{"sw/qa/extras/odfexport/data", 80, []string{".odt"}}, {"sw/qa/extras/odfimport/data", 40, []string{".odt"}},
		{"sw/qa/extras/layout/data", 30, []string{".odt"}},
		{"sw/qa/extras/odfexport/data", 15, []string{".fodt"}}, {"sw/qa/extras/odfimport/data", 10, []string{".fodt"}},
		{"sw/qa/extras/layout/data", 20, []string{".fodt"}},
	},
	"xlsx": {{"sc/qa/unit/data/xlsx", 220, nil}, {"chart2/qa/extras/data/xlsx", 60, nil}},
	"ods":  {{"sc/qa/unit/data/ods", 200, nil}, {"sc/qa/unit/data/fods", 16, nil}, {"chart2/qa/extras/data/ods", 40, nil}},
	"csv":  {{"sc/qa/unit/data/csv", 9, nil}},
	"pptx": {{"sd/qa/unit/data/pptx", 200, nil}, {"oox/qa/unit/data", 61, nil}, {"chart2/qa/extras/data/pptx", 38, nil}, {"sd/qa/unit/data", 20, nil}},
	// Impress keeps its flat presentations beside the packaged ones, where
	// Calc gives them a directory of their own.
	"odp": {{"sd/qa/unit/data/odp", 150, []string{".odp", ".fodp"}}, {"sd/qa/unit/data", 20, nil}},
}

// libreOfficeExtensions are the extensions each corpus reads.
//
//nolint:gochecknoglobals // static table
var libreOfficeExtensions = map[string][]string{
	"docx": {".docx"}, "odt": {".odt", ".fodt"}, "xlsx": {".xlsx"}, "ods": {".ods", ".fods"},
	"csv": {".csv"}, "pptx": {".pptx"}, "odp": {".odp", ".fodp"},
}

// libreOfficeMixedDirs are the module test data directories that hold
// documents of several formats beside the format directories under them;
// their fixture IDs take "<module>-data", so an ID never names two files.
//
//nolint:gochecknoglobals // static table
var libreOfficeMixedDirs = []string{"sd/qa/unit/data"}

// libreOfficeLeftOut are the documents the route refuses or whose reference
// the diff feature cannot score.
//
//nolint:gochecknoglobals // static table
var libreOfficeLeftOut = []string{
	// Malformed on purpose: the route answers 400, "LibreOffice could not
	// read the document".
	"sw/qa/extras/ooxmlimport/data/math-malformed_xml.docx",
	// A slide of 500 by 500 cm, Impress's largest: drawn at 72 dpi it is
	// 14174 pixels square, four times the render budget (pdf.Limits'
	// MaxRenderPixels, 50 million) the cell draws a page within.
	"sd/qa/unit/data/odp/canvas-slide.odp",
}

// libreOfficeDirExts returns the extensions read in the core directory d:
// its own when it lists them; a test directory named for a format gives that
// extension alone, so a fixture ID's extension names the directory; any
// other directory, a mixed one, gives the corpus's extensions.
func libreOfficeDirExts(d loDir, exts []string) []string {
	if d.exts != nil {
		return d.exts
	}
	own := "." + d.path[strings.LastIndex(d.path, "/")+1:]
	if slices.Contains(exts, own) {
		return []string{own}
	}
	return exts
}

// libreOfficeShortDir returns the <dir> of the fixture IDs of the core
// directory d: the Writer test directory, "<module>-data" for a mixed
// directory, or the module.
func libreOfficeShortDir(d string) string {
	parts := strings.Split(d, "/")
	switch {
	case parts[0] == "sw":
		return parts[3]
	case slices.Contains(libreOfficeMixedDirs, d):
		return parts[0] + "-data"
	}
	return parts[0]
}

// selectLibreOffice selects from the test data directories of a LibreOffice
// core checkout the documents of the corpus: each directory's qualifying
// documents of at most officeMaxBytes with a plain name, in name order, the
// first of its cap kept.
func selectLibreOffice(_ context.Context, checkout, format, dir string, _ map[string]string) error {
	dirs, ok := libreOfficeDirs[format]
	if !ok {
		return fmt.Errorf("no libreoffice corpus of format %q", format)
	}
	qualifies := officeQualifiesBy[format]
	var kept []selected
	for _, d := range dirs {
		exts := libreOfficeDirExts(d, libreOfficeExtensions[format])
		full := filepath.Join(checkout, filepath.FromSlash(d.path))
		entries, err := os.ReadDir(full)
		if err != nil {
			return err
		}
		n, qualifying := 0, 0
		for _, e := range entries {
			name := e.Name()
			p := filepath.Join(full, name)
			info, err := os.Stat(p)
			if err != nil || !info.Mode().IsRegular() || !hasAnySuffix(name, exts) || !plainName.MatchString(name) ||
				slices.Contains(libreOfficeLeftOut, d.path+"/"+name) || info.Size() > officeMaxBytes || !qualifies(p) {
				continue
			}
			qualifying++
			if n == d.cap {
				continue
			}
			digest, err := fileDigest(p)
			if err != nil {
				return err
			}
			kept = append(kept, selected{id: libreOfficeShortDir(d.path) + "/" + name, path: d.path + "/" + name, sha256: digest})
			n++
		}
		fmt.Fprintf(os.Stderr, "%s: %d of at most %d (%d qualifying)\n", d.path, n, d.cap, qualifying)
	}
	licence, err := licenceFiles(checkout, "COPYING.MPL")
	if err != nil {
		return err
	}
	return writeLists(dir, kept, licence)
}
