package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The generators of the provisioning image's goldens, each driving one of
// the image's oracles: poppler's pdftoppm, pdfcpu, Mustang, KoSIT and
// veraPDF.

// renderTimeout bounds one pdftoppm or pdfinfo call. Anti-DoS guard for test
// tooling: a crafted PDF can keep poppler busy indefinitely, while at 72 dpi
// a page takes well under a second.
const renderTimeout = 120 * time.Second

// renderPDFs writes the render goldens of the PDFs at the top of pdfs: the
// first maxPages pages (every page when 0) of each sampled PDF, drawn by
// poppler at [renderDPI] into <out>/<id>.<page>.png, and <out>/<id>.json, the
// [renderIndex] naming the pages drawn, the page count and every failure, so
// the render feature tells a page the oracle could not draw from one it never
// tried.
//
// A PDF is drawn when the first byte of the SHA-256 of its file name is
// below sample: 256 draws every PDF, 16 one in sixteen. Hashing the name
// keeps the sample stable when the corpus gains or loses other files.
//
// Every setting is passed explicitly, so a poppler default that moves cannot
// move the goldens:
//
//   - 72 dpi: one pixel per default user space unit (ISO 32000-1, 8.3.2.3),
//     small enough to keep a corpus's goldens to tens of megabytes, while a
//     misplaced path, a wrong colour or a missing glyph still covers pixels.
//   - -cropbox: the region a viewer displays (ISO 32000-1, 14.11.2), not the
//     media box pdftoppm draws by default.
//   - -aa yes -aaVector yes: anti-aliased text and vector graphics, as
//     viewers draw them.
//   - Annotations are drawn, as viewers draw them, unless hide: the index
//     records it (hide_annotations).
func renderPDFs(ctx context.Context, pdfs, out string, sample, maxPages int, hide bool) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	files, err := topPDFs(pdfs, false)
	if err != nil {
		return err
	}
	var picked []string
	for _, f := range files {
		if renderSampled(filepath.Base(f), sample) {
			picked = append(picked, f)
		}
	}
	if len(picked) == 0 {
		return errors.New("the sample selected no PDF")
	}
	indexes := make([]renderIndex, len(picked))
	err = forEach(ctx, len(picked), runtime.NumCPU(), func(i int) error {
		idx, err := renderOne(ctx, picked[i], out, maxPages, hide)
		indexes[i] = idx
		return err
	})
	if err != nil {
		return err
	}
	rendered, failed := 0, 0
	for _, idx := range indexes {
		if idx.Error != "" {
			failed++
		}
		for _, pg := range idx.Pages {
			if pg.Error != "" {
				failed++
			} else {
				rendered++
			}
		}
	}
	fmt.Printf("  render: %d PDFs sampled, %d pages rendered, %d failures\n", len(picked), rendered, failed)
	return nil
}

// renderSampled reports whether the PDF named name is in a sample of
// sample per 256.
func renderSampled(name string, sample int) bool {
	sum := sha256.Sum256([]byte(name))
	return int(sum[0]) < sample
}

// renderOne draws one PDF and writes its index. A failure is recorded in the
// index rather than ending the run.
func renderOne(ctx context.Context, pdf, out string, maxPages int, hide bool) (renderIndex, error) {
	id := strings.TrimSuffix(filepath.Base(pdf), ".pdf")
	idx := renderIndex{ID: id, DPI: renderDPI, Pages: []renderIndexPage{}}
	info, stderr, err := runBounded(ctx, "pdfinfo", pdf)
	if err != nil {
		if stderr == "" {
			stderr = "exit status"
		}
		idx.Error = "pdfinfo: " + stderr
		return idx, writeJSONFile(filepath.Join(out, id+".json"), idx)
	}
	for _, l := range strings.Split(info, "\n") {
		if rest, ok := strings.CutPrefix(l, "Pages:"); ok {
			if f := strings.Fields(rest); len(f) > 0 {
				idx.PageCount, _ = strconv.Atoi(f[0]) //nolint:errcheck // a count pdfinfo did not print is 0 pages
			}
			break
		}
	}
	last := idx.PageCount
	if maxPages > 0 && maxPages < last {
		last = maxPages
	}
	for page := 1; page <= last; page++ {
		base := filepath.Join(out, id+"."+strconv.Itoa(page))
		args := []string{"-r", strconv.Itoa(renderDPI), "-cropbox", "-aa", "yes", "-aaVector", "yes"}
		if hide {
			args = append(args, "-hide-annotations")
		}
		args = append(args, "-f", strconv.Itoa(page), "-l", strconv.Itoa(page), "-singlefile", "-png", pdf, base)
		stdout, stderr, err := runBounded(ctx, "pdftoppm", args...)
		if fi, statErr := os.Stat(base + ".png"); err == nil && statErr == nil && fi.Size() > 0 {
			idx.Pages = append(idx.Pages, renderIndexPage{Page: page, File: filepath.Base(base) + ".png"})
			continue
		}
		_ = os.Remove(base + ".png") //nolint:errcheck // a page that failed may have left no file
		msg := strings.TrimRight(stdout+stderr, "\n")
		if msg == "" {
			msg = "no output"
		}
		idx.Pages = append(idx.Pages, renderIndexPage{Page: page, Error: "pdftoppm: " + msg})
	}
	idx.HideAnnotations = hide
	return idx, writeJSONFile(filepath.Join(out, id+".json"), idx)
}

// runBounded runs a tool under [renderTimeout], returning its standard output
// and its standard error, trailing newlines trimmed.
func runBounded(ctx context.Context, name string, args ...string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(ctx, renderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the image's fixed tools
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err = cmd.Run()
	return strings.TrimRight(o.String(), "\n"), strings.TrimRight(e.String(), "\n"), err
}

// copyPopplerFonts copies the fonts poppler draws unembedded fonts with (URW
// base 35, DejaVu, Noto Sans CJK), the TrueType and OpenType files of its
// bundle, which the render feature hands to litho so both draw them alike.
// They come from the releases the goldens/render pins name, so the stage's
// record covers them; goldens that predate the copy get it on the next run.
func copyPopplerFonts(dst string) error {
	if nonEmptyDir(dst) {
		return nil
	}
	fmt.Println("==> copying poppler's fonts for the render feature...")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(imagePopplerFonts, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		ext := filepath.Ext(p)
		if !d.Type().IsRegular() || (ext != ".otf" && ext != ".ttf") {
			return nil
		}
		data, err := os.ReadFile(p) //nolint:gosec // the image's own fonts
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, filepath.Base(p)), data, 0o644) //nolint:gosec // goldens are world-readable
	})
}

// emptyBookmarks is the bookmarks golden of a PDF pdfcpu exports no outline
// of, which pairs it with litho's empty extraction rather than dropping it.
const emptyBookmarks = "{\"bookmarks\":[]}\n"

// bookmarkGoldens writes <out>/<id>.json, pdfcpu's export of the outline of
// each PDF at the top of pdfs. pdfcpu fails on a PDF with no outline, the
// majority, so its exit status says nothing: such a PDF gets [emptyBookmarks].
func bookmarkGoldens(ctx context.Context, pdfs, out string) error {
	files, err := topPDFs(pdfs, false)
	if err != nil {
		return err
	}
	found := 0
	for _, pdf := range files {
		dst := filepath.Join(out, strings.TrimSuffix(filepath.Base(pdf), ".pdf")+".json")
		if _, err := runQuiet(ctx, "", "pdfcpu", "bookmarks", "export", pdf, dst); err == nil {
			found++
			continue
		}
		if err := os.WriteFile(dst, []byte(emptyBookmarks), 0o644); err != nil { //nolint:gosec // goldens are world-readable
			return err
		}
	}
	fmt.Printf("  bookmarks: %d PDFs (%d with outline, %d empty)\n", len(files), found, len(files)-found)
	return nil
}

// mergeEmptyPartner is the pdfcpu test file, one empty page, every PDF of
// the merge-empty goldens is merged with.
const mergeEmptyPartner = "pkg_testdata__empty.pdf"

// maxMergePages is the page count above which a PDF gets no merge golden: the
// merge features skip such a PDF at run time, and merging a 1000-page
// document is 2000 pages of writing per fixture. Mirrors the features' cap.
const maxMergePages = 30

// mergeGoldens writes <out>/<id>.json, pdfcpu's export of the outline of
// each PDF at the top of pdfs merged, by pdfcpu, with itself, or with partner
// when set: merging with a known one-page PDF exercises the two-input merge,
// whose inputs bring different resources and outlines, where merging with
// itself lets an engine deduplicate by reference. A PDF over [maxMergePages]
// gets no golden; one pdfcpu cannot merge, or whose merge has no outline,
// gets [emptyBookmarks].
func mergeGoldens(ctx context.Context, pdfs, out, partner string) error {
	if partner != "" {
		if _, err := os.Stat(partner); err != nil {
			return fmt.Errorf("the merge partner is missing: %w", err)
		}
	}
	files, err := topPDFs(pdfs, false)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "merge-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	total, ok, empty, skipped := 0, 0, 0, 0
	for _, pdf := range files {
		if pdf == partner {
			continue
		}
		total++
		if pages := pdfcpuPageCount(ctx, pdf); pages > maxMergePages {
			skipped++
			continue
		}
		with := pdf
		if partner != "" {
			with = partner
		}
		merged := filepath.Join(tmp, "out.pdf")
		dst := filepath.Join(out, strings.TrimSuffix(filepath.Base(pdf), ".pdf")+".json")
		_, mergeErr := runQuiet(ctx, "", "pdfcpu", "merge", merged, pdf, with)
		if mergeErr == nil {
			if _, err := runQuiet(ctx, "", "pdfcpu", "bookmarks", "export", merged, dst); err == nil {
				ok++
				_ = os.Remove(merged) //nolint:errcheck // the next merge overwrites it
				continue
			}
		}
		_ = os.Remove(merged)                                                    //nolint:errcheck // the next merge overwrites it
		if err := os.WriteFile(dst, []byte(emptyBookmarks), 0o644); err != nil { //nolint:gosec // goldens are world-readable
			return err
		}
		empty++
	}
	fmt.Printf("  merges: %d PDFs (%d with outline, %d empty, %d too large)\n", total, ok, empty, skipped)
	return nil
}

// pdfcpuPageCount is the page count pdfcpu info prints, or 0 when it prints
// none.
func pdfcpuPageCount(ctx context.Context, pdf string) int {
	cmd := exec.CommandContext(ctx, "pdfcpu", "info", pdf) //nolint:gosec // the image's fixed tool
	out, _ := cmd.Output()                                 //nolint:errcheck // a PDF pdfcpu cannot read has no count
	for _, l := range strings.Split(string(out), "\n") {
		if !strings.Contains(l, "Page count:") {
			continue
		}
		if f := strings.Fields(l); len(f) > 2 {
			n, _ := strconv.Atoi(f[2]) //nolint:errcheck // an unreadable count is no count
			return n
		}
		return 0
	}
	return 0
}

// mustangGoldens validates every PDF at the top of pdfs with Mustang, which
// covers the Factur-X layer veraPDF does not see (the embedded file's name
// and /AFRelationship, the catalog /AF, the XMP fx: properties and their
// extension schema, the Cross-Industry Invoice XSD), and writes the verdicts
// to out as {"jobs": [...]}.
func mustangGoldens(ctx context.Context, pdfs, out string) error {
	files, err := topPDFs(pdfs, true)
	if err != nil {
		return err
	}
	jobs := make([]mustangJob, 0, len(files))
	for _, pdf := range files {
		report, err := runQuiet(ctx, "", "java", "-jar", imageMustang, "--action", "validate", "--source", pdf)
		jobs = append(jobs, mustangJob{File: filepath.Base(pdf), Valid: err == nil, Report: report})
	}
	fmt.Printf("  mustang: %d jobs written to %s\n", len(jobs), out)
	return writeJSONFile(out, mustangReport{Jobs: jobs})
}

// kositSkip matches, on one line of a KoSIT report, the markers of a
// document no scenario applies to: the phrases of KoSIT 1.5 and the element
// of 1.6.
var kositSkip = regexp.MustCompile(`(?i)<acceptedBy>\s*</acceptedBy>|no scenario matched|no applicable scenario|noScenarioMatched`)

// kositRule matches a rule ID of a KoSIT report.
var kositRule = regexp.MustCompile(`id="[A-Z0-9_\\-]+"`)

// kositGoldens validates the CII XML each PDF at the top of pdfs embeds,
// extracted by Mustang, with KoSIT and its XRechnung scenarios, which route a
// document to the XRechnung or the EN 16931 CII Schematron by its
// CustomizationID, and writes the verdicts to out as {"jobs": [...]}. KoSIT's
// exit status says the validator ran: whether a scenario applied is read from
// its report.
func kositGoldens(ctx context.Context, pdfs, out string) error {
	files, err := topPDFs(pdfs, true)
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "kosit-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	jobs := make([]kositJob, 0, len(files))
	for i, pdf := range files {
		job, err := kositOne(ctx, pdf, filepath.Join(work, strconv.Itoa(i)))
		if err != nil {
			return err
		}
		jobs = append(jobs, job)
	}
	fmt.Printf("  kosit: %d jobs written to %s\n", len(jobs), out)
	return writeJSONFile(out, kositReport{Jobs: jobs})
}

func kositOne(ctx context.Context, pdf, dir string) (kositJob, error) {
	job := kositJob{File: filepath.Base(pdf), Verdict: "skip"}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return job, err
	}
	// Mustang 2.20 and later need --out: without it they prompt on stdin.
	if _, err := runQuiet(ctx, dir, "java", "-jar", imageMustang, "--action", "extract", "--source", pdf, "--out", "factur-x.xml"); err != nil {
		job.Reason = "no embedded CII found"
		return job, nil //nolint:nilerr // Mustang's failure is the verdict: the PDF embeds no invoice
	}
	xml := ""
	for _, c := range []string{"factur-x.xml", "zugferd-invoice.xml", "ZUGFeRD-invoice.xml"} {
		if _, err := os.Stat(filepath.Join(dir, c)); err == nil {
			xml = filepath.Join(dir, c)
			break
		}
	}
	if xml == "" {
		job.Reason = "extracted XML not found"
		return job, nil
	}
	reports := filepath.Join(dir, "kosit")
	if err := os.MkdirAll(reports, 0o755); err != nil {
		return job, err
	}
	answer, runErr := runQuiet(ctx, dir, "java", "-jar", imageKoSIT, "--scenarios", imageKoSITConfig, "--output-directory", reports, xml)
	body := answer
	if found, err := kositReportFile(reports); err != nil {
		return job, err
	} else if found != "" {
		data, err := os.ReadFile(found) //nolint:gosec // the report KoSIT just wrote
		if err != nil {
			return job, err
		}
		body = strings.TrimRight(string(data), "\n")
	}
	job.Report = body
	for _, l := range strings.Split(body, "\n") {
		if kositSkip.MatchString(l) {
			job.Reason = "no scenario applicable"
			return job, nil
		}
	}
	seen := map[string]bool{}
	rules := []string{}
	for _, m := range kositRule.FindAllString(body, -1) {
		id := strings.TrimSuffix(strings.TrimPrefix(m, `id="`), `"`)
		if !seen[id] {
			seen[id] = true
			rules = append(rules, id)
		}
	}
	sort.Strings(rules)
	job.Rules = rules
	job.Verdict = "fail"
	if runErr == nil {
		job.Verdict = "pass"
	}
	return job, nil
}

// kositReportFile returns the first XML file KoSIT wrote under dir, at most one
// directory down, or "".
func kositReportFile(dir string) (string, error) {
	var found []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p) //nolint:errcheck // p is under dir
		if d.IsDir() && strings.Count(rel, string(filepath.Separator)) >= 1 {
			return fs.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".xml") {
			found = append(found, p)
		}
		return nil
	})
	if err != nil || len(found) == 0 {
		return "", err
	}
	return found[0], nil
}

// veraPDFFlavours maps a profile directory to veraPDF's --flavour.
var veraPDFFlavours = map[string]string{ //nolint:gochecknoglobals // static table
	"PDF_A-1a": "1a", "PDF_A-1b": "1b",
	"PDF_A-2a": "2a", "PDF_A-2b": "2b", "PDF_A-2u": "2u",
	"PDF_A-3a": "3a", "PDF_A-3b": "3b", "PDF_A-3u": "3u",
	"PDF_A-4": "4", "PDF_A-4e": "4e", "PDF_A-4f": "4f",
	"PDF_UA-1": "ua1", "PDF_UA-2": "ua2",
	"WTPDF-1.0-Accessibility": "wt1a", "WTPDF-1.0-Reuse": "wt1r",
}

// veraPDFGoldens writes <out>/<profile>.json, veraPDF's report of the PDFs of
// pdfs against one profile. Without claims, every PDF is validated against
// every profile; with claims, the claims.csv of [writeClaims], each PDF only
// against the profile it claims, so the cost scales with the corpus, not
// with the corpus times the profiles. veraPDF's exit status is not a
// failure: it reports the documents that do not conform.
func veraPDFGoldens(ctx context.Context, pdfs, out string, profiles []string, claims string) error {
	if claims == "" {
		for _, profile := range profiles {
			if err := veraPDFReport(ctx, profile, pdfs, out); err != nil {
				return err
			}
		}
		return nil
	}
	rows, err := loadClaimsCSV(claims)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "verapdf-claims-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	buckets := map[string]bool{}
	for base, profile := range rows {
		src := filepath.Join(pdfs, base+".pdf")
		if _, err := os.Stat(src); err != nil {
			fmt.Fprintf(os.Stderr, "  WARN: claimed PDF missing on disk: %s.pdf\n", base)
			continue
		}
		bucket := filepath.Join(stage, profile)
		if err := os.MkdirAll(bucket, 0o755); err != nil {
			return err
		}
		if err := linkOrCopy(src, filepath.Join(bucket, base+".pdf")); err != nil {
			return fmt.Errorf("stage %s: %w", src, err)
		}
		buckets[profile] = true
	}
	names := make([]string, 0, len(buckets))
	for b := range buckets {
		names = append(names, b)
	}
	sort.Strings(names)
	for _, profile := range names {
		if err := veraPDFReport(ctx, profile, filepath.Join(stage, profile), out); err != nil {
			return err
		}
	}
	return nil
}

// linkOrCopy hard-links src at dst, or copies it when the two sit on other
// file systems: the PDFs are on the mounted testdata.
func linkOrCopy(src, dst string) error {
	if os.Link(src, dst) == nil {
		return nil
	}
	data, err := os.ReadFile(src) //nolint:gosec // a corpus PDF
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600) //nolint:gosec // dst is a staging path under a temporary directory
}

// veraPDFReport validates the PDFs of dir against profile into
// <out>/<profile>.json.
func veraPDFReport(ctx context.Context, profile, dir, out string) error {
	flavour, ok := veraPDFFlavours[profile]
	if !ok {
		fmt.Fprintf(os.Stderr, "  WARN: unknown profile %s, skipping\n", profile)
		return nil
	}
	fmt.Printf("  %s (flavour=%s)...\n", profile, flavour)
	f, err := os.Create(filepath.Join(out, profile+".json")) //nolint:gosec // a golden this stage writes
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "verapdf", "-f", flavour, "-r", "--format", "json", "--maxfailuresdisplayed", "-1", dir) //nolint:gosec // the image's fixed tool
	cmd.Stdout = f
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			f.Close()
			return fmt.Errorf("verapdf %s: %w", profile, err)
		}
		fmt.Printf("  WARN: verapdf had errors for %s (exit %d)\n", profile, exit.ExitCode())
	}
	return f.Close()
}

// writeJSONFile writes v as indented JSON, HTML characters unescaped.
func writeJSONFile(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644) //nolint:gosec // goldens are world-readable
}

// forEach calls fn for 0..n-1 on at most workers goroutines, stopping at the
// first error or when ctx is done.
func forEach(ctx context.Context, n, workers int, fn func(i int) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	work := make(chan int)
	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
	)
	for range max(workers, 1) {
		wg.Go(func() {
			for i := range work {
				if err := fn(i); err != nil {
					once.Do(func() { first = err; cancel() })
				}
			}
		})
	}
feed:
	for i := range n {
		select {
		case work <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(work)
	wg.Wait()
	if first != nil {
		return first
	}
	return ctx.Err()
}
