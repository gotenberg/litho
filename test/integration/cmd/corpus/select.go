package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// selector writes the lists of one corpus (documents.json or its own list,
// and files.sha256) into the corpus directory dir, choosing its documents
// from a checkout of its source at the pinned commit; a selector that reads
// remote archives takes no checkout.
type selector func(ctx context.Context, checkout, format, dir string, pins map[string]string) error

// selectors maps a corpus source to the selector of its corpora.
//
//nolint:gochecknoglobals // static dispatch table
var selectors = map[string]selector{
	"libreoffice": selectLibreOffice,
	"poi":         selectPOI,
	"openxmlsdk":  selectOpenXMLSDK,
	"govdocs":     selectGovdocs,
	"wpt":         selectWPT,
	"resvg":       selectResvg,
	"verapdf":     selectVeraPDF,
	"pdf20":       selectPDF20,
	"pdfcpu":      selectPDFCPU,
	"facturx":     selectFacturX,
	"pdfjs":       selectPDFJS,
	"weasyprint":  selectWeasyPrint,
}

// plainName is a file name every shell and form field carries intact.
var plainName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// unsafeID finds the runs of characters a fixture ID does not carry.
var unsafeID = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func runUpdate(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	corpus := fs.String("corpus", "", "The corpus whose lists to write (libreoffice/docx, poi/xls, wpt)")
	checkout := fs.String("checkout", "", "A checkout of the corpus's source at its pin (none for govdocs)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *corpus == "" {
		fmt.Fprintln(os.Stderr, "update: --corpus is required")
		return 2
	}
	if err := selectCorpus(ctx, *corpus, *checkout); err != nil {
		fmt.Fprintf(os.Stderr, "update: %v\n", err)
		return 1
	}
	return 0
}

// selectCorpus writes the lists of the named corpus.
func selectCorpus(ctx context.Context, name, checkout string) error {
	m, err := LoadManifest(name)
	if err != nil {
		return err
	}
	source, format, _ := strings.Cut(name, "/")
	sel, ok := selectors[source]
	if !ok {
		return fmt.Errorf("no selector for the %s corpora", source)
	}
	if checkout == "" && source != "govdocs" && source != "weasyprint" {
		return errors.New("--checkout is required")
	}
	pins, err := loadPins()
	if err != nil {
		return err
	}
	return sel(ctx, checkout, format, m.SourceDir(), pins.Values)
}

// plainID returns the fixture ID of the "/"-separated path rel: each segment
// with its runs of other characters written "_".
func plainID(rel string) string {
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		s = strings.Trim(unsafeID.ReplaceAllString(s, "_"), "_")
		if s == "" {
			s = "_"
		}
		segs[i] = s
	}
	return strings.Join(segs, "/")
}

// selected is one document a selector keeps.
type selected struct {
	id, path, sha256 string
}

func fileDigest(p string) (string, error) {
	f, err := os.Open(p) //nolint:gosec // a document of a source checkout
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// walkFiles calls fn on the regular files under root, a directory's files in
// name order before its subdirectories, which are walked in name order.
func walkFiles(root string, fn func(path string) error) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var dirs []string
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		switch {
		case e.IsDir():
			dirs = append(dirs, p)
		case e.Type().IsRegular():
			if err := fn(p); err != nil {
				return err
			}
		}
	}
	for _, d := range dirs {
		if err := walkFiles(d, fn); err != nil {
			return err
		}
	}
	return nil
}

// selectTree walks subdir of the checkout and returns the documents of
// format to keep: those of an extension in exts, at most maxBytes, not
// skipped, and qualifying, one each of identical bytes, sorted by ID, the
// first limit kept. A document's path is its path in the checkout, its ID its
// path under subdir made plain; skip reads the path under subdir.
func selectTree(checkout, subdir, format string, exts []string, limit int, maxBytes int64, skip func(string) bool) ([]selected, int, int, error) {
	qualifies := officeQualifiesBy[format]
	root := filepath.Join(checkout, filepath.FromSlash(subdir))
	seen := map[string]bool{}
	var found []selected
	candidates := 0
	err := walkFiles(root, func(p string) error {
		if !hasAnySuffix(strings.ToLower(filepath.Base(p)), exts) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		if skip(rel) || info.Size() > maxBytes {
			return nil
		}
		candidates++
		if !qualifies(p) {
			return nil
		}
		digest, err := fileDigest(p)
		if err != nil {
			return err
		}
		if seen[digest] {
			return nil
		}
		seen[digest] = true
		repo, err := filepath.Rel(checkout, p)
		if err != nil {
			return err
		}
		found = append(found, selected{id: plainID(rel), path: filepath.ToSlash(repo), sha256: digest})
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	sort.Slice(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.id != b.id {
			return a.id < b.id
		}
		if a.path != b.path {
			return a.path < b.path
		}
		return a.sha256 < b.sha256
	})
	for i := 1; i < len(found); i++ {
		if found[i].id == found[i-1].id {
			return nil, 0, 0, fmt.Errorf("two documents of %s share the ID %s once made plain", format, found[i].id)
		}
	}
	return found[:min(limit, len(found))], candidates, len(found), nil
}

// writeLists writes documents.json ({"id", "path"} each) and files.sha256
// ("<sha256>  <path>" for each document, then the licence files) into dir.
func writeLists(dir string, kept []selected, licence []selected) error {
	docs := make([]documentEntry, len(kept))
	var sums strings.Builder
	for i, k := range kept {
		docs[i] = documentEntry{ID: k.id, Path: k.path}
		fmt.Fprintf(&sums, "%s  %s\n", k.sha256, k.path)
	}
	for _, l := range licence {
		fmt.Fprintf(&sums, "%s  %s\n", l.sha256, l.path)
	}
	if err := writeJSON(filepath.Join(dir, "documents.json"), docs); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "files.sha256"), []byte(sums.String()))
}

// writeJSON writes v as indented JSON; the Make target formats it with
// Prettier, as every committed JSON file is.
func writeJSON(p string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode %s: %w", p, err)
	}
	return writeFileAtomic(p, buf.Bytes())
}

// licenceFiles hashes the licence files of a checkout.
func licenceFiles(checkout string, paths ...string) ([]selected, error) {
	out := make([]selected, len(paths))
	for i, p := range paths {
		digest, err := fileDigest(filepath.Join(checkout, filepath.FromSlash(p)))
		if err != nil {
			return nil, err
		}
		out[i] = selected{path: p, sha256: digest}
	}
	return out, nil
}

// officeExtensions are the extensions the poi and openxmlsdk corpora read
// per format: a template converts as its document does.
//
//nolint:gochecknoglobals // static table
var officeExtensions = map[string][]string{
	"docx": {".docx", ".dotx"}, "xlsx": {".xlsx", ".xltx"}, "pptx": {".pptx", ".potx"},
	"doc": {".doc"}, "xls": {".xls"}, "ppt": {".ppt"},
}

// officeMaxBytes bounds a test-suite document, as the libreoffice corpora's:
// 150 KiB keeps a corpus a few tens of megabytes and a run within minutes,
// and leaves out the large documents whose size is mostly pictures and
// embedded fonts.
const officeMaxBytes = 150 << 10

// poiCap and openXMLSDKCap bound the documents of each format.
const (
	poiCap        = 200
	openXMLSDKCap = 250
)

// poiRefused and openXMLSDKRefused are the documents Gotenberg's LibreOffice
// route refuses ("could not read the document", HTTP 400), which have no
// reference to score litho against, by path under the test data directory.
//
//nolint:gochecknoglobals // static lists
var (
	poiRefused = []string{
		"document/ExternalEntityInText.docx",
		"slideshow/7ffe3cabb976ebc593dfe2f9461bdeac980a626c.ppt",
		"slideshow/backgrounds.ppt",
		"slideshow/pictures.ppt",
	}
	openXMLSDKRefused = []string{
		"v2FxTestFiles/spreadsheet/O12 templates/ProjectStatusReport_TP10094814.xltx",
	}
)

// selectPOI selects from test-data/ of an Apache POI checkout the documents
// of the format: not listed by POI's poi-integration-exceptions.csv (the
// encrypted, malformed and fuzzed files POI expects to fail on) nor refused
// by the route.
func selectPOI(_ context.Context, checkout, format, dir string, _ map[string]string) error {
	exts, ok := officeExtensions[format]
	if !ok {
		return fmt.Errorf("no poi corpus of format %q", format)
	}
	f, err := os.Open(filepath.Join(checkout, "test-data", "poi-integration-exceptions.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	cr := csv.NewReader(stripBOM(f))
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		return fmt.Errorf("read the integration exceptions: %w", err)
	}
	excluded := map[string]bool{}
	for i, r := range rows {
		if i > 0 && len(r) > 0 {
			excluded[r[0]] = true
		}
	}
	kept, candidates, qualifying, err := selectTree(checkout, "test-data", format, exts, poiCap, officeMaxBytes, func(rel string) bool {
		return excluded[rel] || slices.Contains(poiRefused, rel)
	})
	if err != nil {
		return err
	}
	licence, err := licenceFiles(checkout, "legal/LICENSE", "legal/NOTICE")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "poi/%s: %d of at most %d (%d candidates, %d qualifying)\n", format, len(kept), poiCap, candidates, qualifying)
	return writeLists(dir, kept, licence)
}

// selectOpenXMLSDK selects from the Open XML SDK's test data storage the
// documents of the format: not under Robustness/ (malformed on purpose) nor
// refused by the route.
func selectOpenXMLSDK(_ context.Context, checkout, format, dir string, _ map[string]string) error {
	if format != "docx" && format != "xlsx" && format != "pptx" {
		return fmt.Errorf("no openxmlsdk corpus of format %q", format)
	}
	kept, candidates, qualifying, err := selectTree(checkout, "test/DocumentFormat.OpenXml.Tests.Assets/assets/TestDataStorage", format,
		officeExtensions[format], openXMLSDKCap, officeMaxBytes, func(rel string) bool {
			return strings.HasPrefix(rel, "Robustness/") || slices.Contains(openXMLSDKRefused, rel)
		})
	if err != nil {
		return err
	}
	licence, err := licenceFiles(checkout, "LICENSE")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "openxmlsdk/%s: %d of at most %d (%d candidates, %d qualifying)\n", format, len(kept), openXMLSDKCap, candidates, qualifying)
	return writeLists(dir, kept, licence)
}

// stripBOM skips a UTF-8 byte order mark at the head of r.
func stripBOM(r io.Reader) io.Reader {
	buf := make([]byte, 3)
	n, _ := io.ReadFull(r, buf)
	if n == 3 && bytes.Equal(buf, []byte{0xEF, 0xBB, 0xBF}) {
		return r
	}
	return io.MultiReader(bytes.NewReader(buf[:n]), r)
}

// govdocsCap bounds the documents of each govdocs Office format; govdocsMaxBytes
// a candidate: real documents run larger than test cases (the pinned threads'
// presentations average 3 MB), and 2 MiB keeps most word processing
// documents and workbooks and the lighter half of the presentations.
const (
	govdocsCap      = 100
	govdocsMaxBytes = 2 << 20
)

// govdocsArchives is where the Govdocs1 thread archives are read from.
const govdocsArchives = "https://digitalcorpora.s3.amazonaws.com/corpora/files/govdocs1/zipfiles"

// selectGovdocs samples the documents of the format out of the thread
// archives GOVDOCS_THREADS pins, read through HTTP range requests: the files
// of the format's extension of at most govdocsMaxBytes, in the order of the
// SHA-256 of their path, each downloaded and kept when it qualifies, until
// govdocsCap are kept.
func selectGovdocs(ctx context.Context, _, format, dir string, pins map[string]string) error {
	if format == "pdf" {
		return selectGovdocsPDF(ctx, pins, dir)
	}
	if format != "doc" && format != "xls" && format != "ppt" {
		return fmt.Errorf("no govdocs Office corpus of format %q", format)
	}
	threads := strings.Split(pins["GOVDOCS_THREADS"], ",")
	zips := newZipCache(http.DefaultClient)
	type candidate struct{ key, thread, entry, path string }
	var cands []candidate
	for _, t := range threads {
		u := govdocsArchives + "/" + t + ".zip"
		names, err := zips.list(ctx, u)
		if err != nil {
			return err
		}
		for _, f := range names {
			base := filepath.Base(f.Name)
			if !strings.HasSuffix(strings.ToLower(base), "."+format) || f.UncompressedSize64 == 0 || f.UncompressedSize64 > govdocsMaxBytes || !plainName.MatchString(base) {
				continue
			}
			p := t + ".zip/" + f.Name
			sum := sha256.Sum256([]byte(p))
			cands = append(cands, candidate{key: hex.EncodeToString(sum[:]), thread: t, entry: f.Name, path: p})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].key < cands[j].key })
	tmp, err := os.MkdirTemp("", "litho-govdocs-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var kept []selected
	tried := 0
	for _, c := range cands {
		if len(kept) == govdocsCap {
			break
		}
		tried++
		data, err := zips.read(ctx, govdocsArchives+"/"+c.thread+".zip", c.entry)
		if err != nil {
			return err
		}
		local := filepath.Join(tmp, filepath.Base(c.entry))
		if err := os.WriteFile(local, data, 0o600); err != nil {
			return err
		}
		if officeQualifiesBy[format](local) {
			sum := sha256.Sum256(data)
			kept = append(kept, selected{id: c.thread + "/" + filepath.Base(c.entry), path: c.path, sha256: hex.EncodeToString(sum[:])})
		}
		if err := os.Remove(local); err != nil {
			return err
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].id < kept[j].id })
	fmt.Fprintf(os.Stderr, "govdocs/%s: %d of at most %d (%d candidates, %d tried)\n", format, len(kept), govdocsCap, len(cands), tried)
	return writeLists(dir, kept, nil)
}
