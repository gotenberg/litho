package main

import (
	"context"
	"crypto/md5" //nolint:gosec // pdf.js's manifest checks its linked files by MD5
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The selectors of the PDF corpora: every PDF of their source at its pin,
// each under the name the corpus has always given it, the fixture ID its
// goldens and baseline cells are keyed by.

// pdfSelected walks dir of the checkout and returns, for each file name
// keeps, its fixture ID (named) and its path in the checkout.
func pdfSelected(checkout, dir string, recursive bool, keep func(name string) bool, named func(rel string) string) ([]selected, error) {
	root := filepath.Join(checkout, filepath.FromSlash(dir))
	var out []selected
	add := func(p string) error {
		if !keep(filepath.Base(p)) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		repo, err := filepath.Rel(checkout, p)
		if err != nil {
			return err
		}
		digest, err := fileDigest(p)
		if err != nil {
			return err
		}
		out = append(out, selected{id: named(filepath.ToSlash(rel)), path: filepath.ToSlash(repo), sha256: digest})
		return nil
	}
	if recursive {
		return out, walkFiles(root, add)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			if err := add(filepath.Join(root, e.Name())); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func lowerPDF(name string) bool { return strings.HasSuffix(name, ".pdf") }

func anyCasePDF(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".pdf") }

// flatName writes the "/" and the spaces of a path as "_".
func flatName(s string) string { return strings.NewReplacer("/", "_", " ", "_").Replace(s) }

// writePDFLists sorts the documents by ID, refuses two of one ID, and writes
// the lists.
func writePDFLists(corpus, dir string, docs []selected) error {
	sort.Slice(docs, func(i, j int) bool { return docs[i].id < docs[j].id })
	for i := 1; i < len(docs); i++ {
		if docs[i].id == docs[i-1].id {
			return fmt.Errorf("%s: %s and %s share the name %s", corpus, docs[i-1].path, docs[i].path, docs[i].id)
		}
	}
	fmt.Fprintf(os.Stderr, "%s: %d PDFs\n", corpus, len(docs))
	return writeLists(dir, docs, nil)
}

// selectVeraPDF lists the PDFs of the veraPDF corpus's PDF/A and PDF/UA
// directories, each named "<directory>__<file name>", spaces as "_".
func selectVeraPDF(_ context.Context, checkout, _, dir string, _ map[string]string) error {
	entries, err := os.ReadDir(checkout)
	if err != nil {
		return err
	}
	var docs []selected
	for _, e := range entries {
		top := e.Name()
		if !e.IsDir() || (!strings.HasPrefix(top, "PDF_A-") && !strings.HasPrefix(top, "PDF_UA-")) {
			continue
		}
		found, err := pdfSelected(checkout, top, true, lowerPDF, func(rel string) string {
			return top + "__" + strings.ReplaceAll(path.Base(rel), " ", "_")
		})
		if err != nil {
			return err
		}
		docs = append(docs, found...)
	}
	return writePDFLists("verapdf", dir, docs)
}

// selectPDF20 lists every PDF of the PDF Association's pdf20examples, named
// by its path, "/" and spaces as "_".
func selectPDF20(_ context.Context, checkout, _, dir string, _ map[string]string) error {
	docs, err := pdfSelected(checkout, ".", true, anyCasePDF, flatName)
	if err != nil {
		return err
	}
	kept := docs[:0]
	for _, d := range docs {
		if !strings.HasPrefix(d.path, ".git/") {
			kept = append(kept, d)
		}
	}
	return writePDFLists("pdf20", dir, kept)
}

// selectPDFCPU lists the PDFs at the top of pdfcpu's bookmark samples and
// test data, each named "<directory>__<stem>.pdf", the directory's "/" as
// "_".
func selectPDFCPU(_ context.Context, checkout, _, dir string, _ map[string]string) error {
	var docs []selected
	for _, root := range []string{"pkg/samples/bookmarks", "pkg/testdata"} {
		found, err := pdfSelected(checkout, root, false, anyCasePDF, func(rel string) string {
			return flatName(root) + "__" + rel[:len(rel)-len(".pdf")] + ".pdf"
		})
		if err != nil {
			return err
		}
		docs = append(docs, found...)
	}
	return writePDFLists("pdfcpu", dir, docs)
}

// selectFacturX lists the PDFs of the ZUGFeRD corpus's v2, XML-Rechnung and
// v1 trees, each named "<prefix>__<directory>_<stem>.pdf", "/" and spaces as
// "_". A PDF at the top of a tree is named "<prefix>__<file name>_<stem>.pdf",
// as the corpus has always named one.
func selectFacturX(_ context.Context, checkout, _, dir string, _ map[string]string) error {
	var docs []selected
	for _, t := range []struct{ sub, prefix string }{{"ZUGFeRDv2", "v2"}, {"XML-Rechnung", "xmlrechnung"}, {"ZUGFeRDv1", "v1"}} {
		found, err := pdfSelected(checkout, t.sub, true, anyCasePDF, func(rel string) string {
			base := path.Base(rel)
			d := rel
			if i := strings.LastIndex(rel, "/"); i >= 0 {
				d = rel[:i]
			}
			return t.prefix + "__" + flatName(d+"/"+base[:len(base)-len(".pdf")]) + ".pdf"
		})
		if err != nil {
			return err
		}
		docs = append(docs, found...)
	}
	return writePDFLists("facturx", dir, docs)
}

// pdfjsLinkJobs bounds the linked files read at once: most are on the
// Internet Archive, which refuses a burst.
const pdfjsLinkJobs = 4

// selectPDFJS lists the PDFs of pdf.js's test/pdfs, named by file name, and
// the files its test manifest links to: each read from the URL its .link
// file names and kept when it has the MD5 the manifest gives it, marked
// optional, since a third party's link can go.
func selectPDFJS(ctx context.Context, checkout, _, dir string, _ map[string]string) error {
	docs, err := pdfSelected(checkout, "test/pdfs", true, lowerPDF, path.Base)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(checkout, "test", "test_manifest.json"))
	if err != nil {
		return err
	}
	var manifest []struct {
		File string     `json:"file"`
		MD5  string     `json:"md5"`
		Link jsonTruthy `json:"link"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode the pdf.js test manifest: %w", err)
	}
	md5s := map[string]string{}
	var linked []string
	seen := map[string]bool{}
	for _, e := range manifest {
		if e.MD5 != "" {
			md5s[e.File] = e.MD5
		}
		if !bool(e.Link) || seen[e.File] {
			continue
		}
		seen[e.File] = true
		if _, err := os.Stat(filepath.Join(checkout, "test", filepath.FromSlash(e.File))); err == nil {
			continue
		}
		linked = append(linked, e.File)
	}
	type result struct {
		doc selected
		url string
		ok  bool
	}
	results := make([]result, len(linked))
	work := make(chan int)
	var wg sync.WaitGroup
	for range pdfjsLinkJobs {
		wg.Go(func() {
			for i := range work {
				file := linked[i]
				link, err := os.ReadFile(filepath.Join(checkout, "test", filepath.FromSlash(file)+".link"))
				if err != nil {
					continue
				}
				u := strings.TrimSpace(string(link))
				body, err := fetchVerified(ctx, http.DefaultClient, u, "", "")
				if err != nil {
					fmt.Fprintf(os.Stderr, "  pdfjs: %s left out: %v\n", file, err)
					continue
				}
				sum := md5.Sum(body) //nolint:gosec // the manifest's own check
				if want := md5s[file]; want != "" && hex.EncodeToString(sum[:]) != want {
					fmt.Fprintf(os.Stderr, "  pdfjs: %s left out: its MD5 is not the manifest's\n", file)
					continue
				}
				digest := sha256.Sum256(body)
				results[i] = result{selected{id: path.Base(file), path: "test/" + file, sha256: hex.EncodeToString(digest[:])}, u, true}
			}
		})
	}
	for i := range linked {
		work <- i
	}
	close(work)
	wg.Wait()
	urls := map[string]string{}
	for _, r := range results {
		if r.ok {
			docs = append(docs, r.doc)
			urls[r.doc.path] = r.url
		}
	}
	fmt.Fprintf(os.Stderr, "pdfjs: %d of %d linked files read\n", len(urls), len(linked))
	if err := writePDFLists("pdfjs", dir, docs); err != nil {
		return err
	}
	return addDocumentURLs(dir, urls)
}

// jsonTruthy reads a value as JavaScript's truthiness does, the way pdf.js's
// tools read its manifest: true, or a non-empty string ("true" in two
// entries).
type jsonTruthy bool

// UnmarshalJSON implements json.Unmarshaler.
func (t *jsonTruthy) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch x := v.(type) {
	case bool:
		*t = jsonTruthy(x)
	case string:
		*t = x != ""
	case float64:
		*t = x != 0
	default:
		*t = false
	}
	return nil
}

// addDocumentURLs rewrites documents.json with the URL and the optional mark
// of each document urls names by path.
func addDocumentURLs(dir string, urls map[string]string) error {
	p := filepath.Join(dir, "documents.json")
	docs, err := readDocumentList(p)
	if err != nil {
		return err
	}
	for i, d := range docs {
		if u, ok := urls[d.Path]; ok {
			docs[i].URL, docs[i].Optional = u, true
		}
	}
	return writeJSON(p, docs)
}

// selectGovdocsPDF lists every PDF of the Govdocs1 threads GOVDOCS_THREADS
// pins, read through HTTP range requests, each named
// "thread_<thread>_<stem>.pdf" (a file whose extension is not a lower-case
// ".pdf" keeps it before ".pdf").
func selectGovdocsPDF(ctx context.Context, pins map[string]string, dir string) error {
	zips := newZipCache(http.DefaultClient)
	var docs []selected
	for _, t := range strings.Split(pins["GOVDOCS_THREADS"], ",") {
		u := govdocsArchives + "/" + t + ".zip"
		files, err := zips.list(ctx, u)
		if err != nil {
			return err
		}
		for _, f := range files {
			if f.FileInfo().IsDir() || !anyCasePDF(f.Name) {
				continue
			}
			body, err := zips.read(ctx, u, f.Name)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(body)
			docs = append(docs, selected{
				id:     "thread_" + t + "_" + strings.TrimSuffix(path.Base(f.Name), ".pdf") + ".pdf",
				path:   t + ".zip/" + f.Name,
				sha256: hex.EncodeToString(digest[:]),
			})
		}
	}
	return writePDFLists("govdocs/pdf", dir, docs)
}
