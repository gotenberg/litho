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
// outnumber the rest, do not crowd out the import and layout tests.
type loDir struct {
	path string
	cap  int
}

// libreOfficeDirs lists, per corpus, the directories read in order.
//
//nolint:gochecknoglobals // static table
var libreOfficeDirs = map[string][]loDir{
	"docx": {{"sw/qa/extras/ooxmlexport/data", 250}, {"sw/qa/extras/ooxmlimport/data", 60}, {"sw/qa/extras/layout/data", 40}},
	"odt":  {{"sw/qa/extras/odfexport/data", 80}, {"sw/qa/extras/odfimport/data", 40}, {"sw/qa/extras/layout/data", 30}},
	"xlsx": {{"sc/qa/unit/data/xlsx", 220}, {"chart2/qa/extras/data/xlsx", 60}},
	"ods":  {{"sc/qa/unit/data/ods", 200}, {"sc/qa/unit/data/fods", 16}, {"chart2/qa/extras/data/ods", 40}},
	"csv":  {{"sc/qa/unit/data/csv", 9}},
	"pptx": {{"sd/qa/unit/data/pptx", 200}, {"oox/qa/unit/data", 61}, {"chart2/qa/extras/data/pptx", 38}, {"sd/qa/unit/data", 20}},
	"odp":  {{"sd/qa/unit/data/odp", 150}, {"sd/qa/unit/data", 20}},
}

// libreOfficeExtensions are the extensions each corpus reads.
//
//nolint:gochecknoglobals // static table
var libreOfficeExtensions = map[string][]string{
	"docx": {".docx"}, "odt": {".odt"}, "xlsx": {".xlsx"}, "ods": {".ods", ".fods"},
	"csv": {".csv"}, "pptx": {".pptx"}, "odp": {".odp", ".fodp"},
}

// libreOfficeDirExtensions lists the directories named for a format that
// hold another extension of the corpus's too: Impress keeps its flat
// presentations beside the packaged ones, where Calc gives them a directory
// of their own.
//
//nolint:gochecknoglobals // static table
var libreOfficeDirExtensions = map[string][]string{"sd/qa/unit/data/odp": {".odp", ".fodp"}}

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

// libreOfficeDirExts returns the extensions read in the core directory d: a
// test directory named for a format gives that extension alone, so a fixture
// ID's extension names the directory, unless libreOfficeDirExtensions lists
// it; any other directory, a Writer test's or a mixed one, gives the
// corpus's extensions.
func libreOfficeDirExts(d string, exts []string) []string {
	if e, ok := libreOfficeDirExtensions[d]; ok {
		return e
	}
	own := "." + d[strings.LastIndex(d, "/")+1:]
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
		exts := libreOfficeDirExts(d.path, libreOfficeExtensions[format])
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
