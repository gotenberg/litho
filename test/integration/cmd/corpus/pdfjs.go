package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

type manifestEntry struct {
	ID       string `json:"id"`
	File     string `json:"file"`
	MD5      string `json:"md5"`
	Type     string `json:"type"`
	LastPage int    `json:"lastPage"`
	Link     any    `json:"link"`
}

type referenceFixture struct {
	ID       string          `json:"id"`
	File     string          `json:"file"`
	NumPages int             `json:"numPages"`
	Metadata refMetadata     `json:"metadata"`
	Outline  json.RawMessage `json:"outline"`
	Pages    []refPage       `json:"pages"`
}

type refMetadata struct {
	Info refInfo `json:"info"`
}

type refInfo struct {
	PDFFormatVersion    string `json:"PDFFormatVersion"`
	Language            string `json:"Language"`
	IsLinearized        bool   `json:"IsLinearized"`
	IsAcroFormPresent   bool   `json:"IsAcroFormPresent"`
	IsXFAPresent        bool   `json:"IsXFAPresent"`
	IsCollectionPresent bool   `json:"IsCollectionPresent"`
	IsSignaturesPresent bool   `json:"IsSignaturesPresent"`
	Author              string `json:"Author"`
	Title               string `json:"Title"`
	Creator             string `json:"Creator"`
	Producer            string `json:"Producer"`
	CreationDate        string `json:"CreationDate"`
	ModDate             string `json:"ModDate"`
	Keywords            string `json:"Keywords"`
	EncryptFilterName   any    `json:"EncryptFilterName"`
}

type refPage struct {
	PageNumber int       `json:"pageNumber"`
	Text       string    `json:"text"`
	ItemCount  int       `json:"itemCount"`
	Items      []refItem `json:"items"`
	Error      string    `json:"error,omitempty"`
}

type refItem struct {
	Str      string  `json:"str"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	FontName string  `json:"fontName"`
}

var pdfjsKnownSkips = map[string]string{
	"bug1393476":                       "infinite loop in xref parsing",
	"bomb_giant_contents_image_stream": "JBIG2 decompression bomb: CGO call blocks ~20s, cannot be cancelled by context",
}

func shouldSkipPDFJS(id string) (string, bool) {
	if reason, ok := pdfjsKnownSkips[id]; ok {
		return reason, true
	}
	return "", false
}

type pdfjsSummary struct {
	Total          int `json:"total"`
	Survived       int `json:"survived"`
	Panics         int `json:"panics"`
	WithReferences int `json:"with_references"`
	SurvivalOnly   int `json:"survival_only"`
	Errors         int `json:"errors"`
	Skipped        int `json:"skipped"`
	Timeouts       int `json:"timeouts"`

	// Dimension-level match counts (out of WithReferences).
	PageCountMatch    int `json:"page_count_match"`
	PageCountMismatch int `json:"page_count_mismatch"`
	MetadataMatches   int `json:"metadata_matches"`
	TextCharsMatch    int `json:"text_chars_match"`
	TextCharsMismatch int `json:"text_chars_mismatch"`
	TextWordsMatch    int `json:"text_words_match"`
	TextWordsMismatch int `json:"text_words_mismatch"`
	ItemCountMatch    int `json:"item_count_match"`
	ItemCountMismatch int `json:"item_count_mismatch"`
}

type pdfjsTextMismatch struct {
	File     string `json:"file"`
	Page     int    `json:"page"`
	Position int    `json:"position"`
	Expected string `json:"expected"`
	Got      string `json:"got"`
}

type pdfjsMetadataMismatch struct {
	File  string `json:"file"`
	Field string `json:"field"`
	Litho string `json:"litho"`
	PDFJS string `json:"pdfjs"`
}

type pdfjsErrorEntry struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

type pdfjsReport struct {
	Summary             pdfjsSummary             `json:"summary"`
	Elapsed             string                   `json:"elapsed"`
	PageCountMismatches []pdfjsPageCountMismatch `json:"page_count_mismatches,omitempty"`
	MetadataMismatches  []pdfjsMetadataMismatch  `json:"metadata_mismatches,omitempty"`
	TextMismatches      []pdfjsTextMismatch      `json:"text_mismatches,omitempty"`
	ItemCountMismatches []pdfjsItemCountMismatch `json:"item_count_mismatches,omitempty"`
	Errors              []pdfjsErrorEntry        `json:"errors,omitempty"`
}

// pdfjsPageCountMismatch records a page count disagreement.
type pdfjsPageCountMismatch struct {
	File  string `json:"file"`
	Litho int    `json:"litho"`
	PDFJS int    `json:"pdfjs"`
}

// pdfjsItemCountMismatch records an item count disagreement for a page.
type pdfjsItemCountMismatch struct {
	File  string `json:"file"`
	Page  int    `json:"page"`
	Litho int    `json:"litho"`
	PDFJS int    `json:"pdfjs"`
}

// pdfjsResult holds the outcome of processing a single manifest entry.
type pdfjsResult struct {
	ID                  string
	File                string
	HasRef              bool
	Skipped             bool
	SkipReason          string
	Error               string
	Timeout             bool
	Panicked            bool
	PageCountMatch      bool
	MetadataMatch       bool
	TextCharsMatch      bool
	TextWordsMatch      bool
	ItemCountMatch      bool
	PageCountMismatch   *pdfjsPageCountMismatch
	MetadataMismatches  []pdfjsMetadataMismatch
	TextMismatches      []pdfjsTextMismatch
	ItemCountMismatches []pdfjsItemCountMismatch
}

func runPDFJS(args []string) {
	fs := flag.NewFlagSet("pdfjs", flag.ExitOnError)
	init := fs.Bool("init", false, "Provision the corpus via Docker before running")
	format := fs.String("format", "text", "Output format: text or json")
	maxTextErrors := fs.Int("max-text-errors", -1, "Maximum text mismatch percentage (-1 = no limit)")
	corpus := fs.String("corpus", "", "Path to pdf.js corpus testdata (auto-detected if empty)")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		os.Exit(1)
	}

	corpusPath := *corpus
	if corpusPath == "" {
		corpusPath = findPDFJSCorpusPath()
	}

	if *init {
		if err := provisionPDFJS(corpusPath); err != nil {
			fmt.Fprintf(os.Stderr, "provisioning failed: %v\n", err)
			os.Exit(1)
		}
	}

	manifestPath := filepath.Join(corpusPath, "test_manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "PDF.js corpus not found.")
		fmt.Fprintln(os.Stderr, "Run with --init to provision the corpus, or check --corpus path.")
		os.Exit(1)
	}

	showProgress := *format != "json"
	start := time.Now()
	rpt := comparePDFJS(corpusPath, showProgress)
	rpt.Elapsed = time.Since(start).Round(time.Millisecond).String()

	switch *format {
	case "json":
		printPDFJSJSON(rpt)
	default:
		printPDFJSText(rpt)
	}

	if *maxTextErrors >= 0 && rpt.Summary.WithReferences > 0 {
		pct := float64(rpt.Summary.TextWordsMismatch) / float64(rpt.Summary.WithReferences) * 100
		if pct > float64(*maxTextErrors) {
			fmt.Fprintf(os.Stderr, "FAIL: text mismatch %.1f%% > max %d%%\n", pct, *maxTextErrors)
			os.Exit(1)
		}
	}
}

// findPDFJSCorpusPath locates the pdf.js testdata directory.
func findPDFJSCorpusPath() string {
	candidates := []string{
		"test/integration/testdata/pdf.js",
		"testdata/pdf.js",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return "test/integration/testdata/pdf.js"
}

// provisionPDFJS provisions the PDF.js corpus via Docker.
func provisionPDFJS(corpusPath string) error {
	return provisionCorpus(corpusPath, "pdf.js", "litho-pdfjs-corpus")
}

func comparePDFJS(corpusPath string, showProgress bool) pdfjsReport {
	manifestPath := filepath.Join(corpusPath, "test_manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading manifest: %v\n", err)
		return pdfjsReport{}
	}

	var entries []manifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		fmt.Fprintf(os.Stderr, "parsing manifest: %v\n", err)
		return pdfjsReport{}
	}

	entries = dedupManifest(entries)
	refsDir := filepath.Join(corpusPath, "references")

	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers < 1 {
		numWorkers = 4
	}

	type workItem struct {
		entry  manifestEntry
		hasRef bool
	}

	work := make(chan workItem, len(entries))
	results := make(chan pdfjsResult, len(entries))

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Go(func() {
			for item := range work {
				results <- processPDFJSEntry(corpusPath, refsDir, item.entry, item.hasRef)
			}
		})
	}

	for _, entry := range entries {
		refPath := filepath.Join(refsDir, entry.ID+".json")
		_, refErr := os.Stat(refPath)
		work <- workItem{entry: entry, hasRef: refErr == nil}
	}
	close(work)

	go func() {
		wg.Wait()
		close(results)
	}()

	var rpt pdfjsReport
	processed := 0
	for res := range results {
		processed++
		if showProgress && processed%50 == 0 {
			fmt.Fprintf(os.Stderr, "\r  processed %d/%d", processed, len(entries))
		}

		rpt.Summary.Total++
		if res.Panicked {
			rpt.Summary.Panics++
			continue
		}
		if res.Skipped {
			rpt.Summary.Skipped++
			continue
		}
		if res.Timeout {
			rpt.Summary.Timeouts++
			continue
		}
		if res.Error != "" {
			rpt.Summary.Errors++
			rpt.Errors = append(rpt.Errors, pdfjsErrorEntry{File: res.File, Error: res.Error})
			continue
		}

		if res.HasRef {
			rpt.Summary.WithReferences++

			// Page count.
			if res.PageCountMatch {
				rpt.Summary.PageCountMatch++
			} else {
				rpt.Summary.PageCountMismatch++
				if res.PageCountMismatch != nil {
					rpt.PageCountMismatches = append(rpt.PageCountMismatches, *res.PageCountMismatch)
				}
			}

			// Metadata.
			if res.MetadataMatch {
				rpt.Summary.MetadataMatches++
			} else {
				rpt.MetadataMismatches = append(rpt.MetadataMismatches, res.MetadataMismatches...)
			}

			// Text content (no spaces).
			if res.TextCharsMatch {
				rpt.Summary.TextCharsMatch++
			} else {
				rpt.Summary.TextCharsMismatch++
			}

			// Text (with word boundaries).
			if res.TextWordsMatch {
				rpt.Summary.TextWordsMatch++
			} else {
				rpt.Summary.TextWordsMismatch++
				rpt.TextMismatches = append(rpt.TextMismatches, res.TextMismatches...)
			}

			// Item count.
			if res.ItemCountMatch {
				rpt.Summary.ItemCountMatch++
			} else {
				rpt.Summary.ItemCountMismatch++
				rpt.ItemCountMismatches = append(rpt.ItemCountMismatches, res.ItemCountMismatches...)
			}
		} else {
			rpt.Summary.SurvivalOnly++
		}
	}
	if showProgress {
		fmt.Fprintf(os.Stderr, "\r  processed %d/%d\n", processed, len(entries))
	}

	rpt.Summary.Survived = rpt.Summary.Total - rpt.Summary.Panics

	sort.Slice(rpt.PageCountMismatches, func(i, j int) bool {
		return rpt.PageCountMismatches[i].File < rpt.PageCountMismatches[j].File
	})
	sort.Slice(rpt.MetadataMismatches, func(i, j int) bool {
		return rpt.MetadataMismatches[i].File < rpt.MetadataMismatches[j].File
	})
	sort.Slice(rpt.TextMismatches, func(i, j int) bool {
		if rpt.TextMismatches[i].File != rpt.TextMismatches[j].File {
			return rpt.TextMismatches[i].File < rpt.TextMismatches[j].File
		}
		return rpt.TextMismatches[i].Page < rpt.TextMismatches[j].Page
	})
	sort.Slice(rpt.ItemCountMismatches, func(i, j int) bool {
		if rpt.ItemCountMismatches[i].File != rpt.ItemCountMismatches[j].File {
			return rpt.ItemCountMismatches[i].File < rpt.ItemCountMismatches[j].File
		}
		return rpt.ItemCountMismatches[i].Page < rpt.ItemCountMismatches[j].Page
	})
	sort.Slice(rpt.Errors, func(i, j int) bool {
		return rpt.Errors[i].File < rpt.Errors[j].File
	})

	return rpt
}

// processPDFJSEntry handles a single manifest entry.
func processPDFJSEntry(corpusPath, refsDir string, entry manifestEntry, hasRef bool) (res pdfjsResult) {
	res = pdfjsResult{ID: entry.ID, File: entry.File, HasRef: hasRef}

	defer func() {
		if r := recover(); r != nil {
			res.Panicked = true
		}
	}()

	if reason, skip := shouldSkipPDFJS(entry.ID); skip {
		res.Skipped = true
		res.SkipReason = reason
		return res
	}

	pdfPath := filepath.Join(corpusPath, entry.File)
	f, err := os.Open(pdfPath)
	if err != nil {
		res.Error = fmt.Sprintf("open: %v", err)
		return res
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		res.Error = fmt.Sprintf("stat: %v", err)
		return res
	}

	ctx, cancel := context.WithTimeout(context.Background(), perFileTimeout)
	defer cancel()

	reader, err := pdf.Open(ctx, f, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		res.Error = fmt.Sprintf("open: %v", err)
		return res
	}

	if hasRef {
		refPath := filepath.Join(refsDir, entry.ID+".json")
		refData, err := os.ReadFile(refPath)
		if err != nil {
			res.Error = fmt.Sprintf("reading reference: %v", err)
			return res
		}
		var pdfjsRef referenceFixture
		if err := json.Unmarshal(refData, &pdfjsRef); err != nil {
			res.Error = fmt.Sprintf("parsing reference: %v", err)
			return res
		}

		lithoRef := lithoReference(ctx, reader, entry.ID, entry.LastPage)
		if lithoRef == nil {
			res.Error = "litho reference generation failed"
			return res
		}

		if ctx.Err() != nil {
			res.Timeout = true
			return res
		}

		cmp := compareFixtures(entry.File, &pdfjsRef, lithoRef)
		res.PageCountMatch = cmp.PageCountMatch
		res.PageCountMismatch = cmp.PageCountMismatch
		res.MetadataMatch = cmp.MetadataMatch
		res.MetadataMismatches = cmp.MetadataMismatches
		res.TextCharsMatch = cmp.TextCharsMatch
		res.TextWordsMatch = cmp.TextWordsMatch
		res.TextMismatches = cmp.TextMismatches
		res.ItemCountMatch = cmp.ItemCountMatch
		res.ItemCountMismatches = cmp.ItemCountMismatches
	} else {
		// Survival test: open, count pages, extract text per page.
		numPages, err := reader.PageCount(ctx)
		if err != nil {
			res.Error = fmt.Sprintf("PageCount: %v", err)
			return res
		}
		lastPage := numPages
		if entry.LastPage > 0 && entry.LastPage < lastPage {
			lastPage = entry.LastPage
		}
		for i := 1; i <= lastPage; i++ {
			if ctx.Err() != nil {
				res.Timeout = true
				return res
			}
			if _, err := reader.ExtractText(ctx, i); err != nil {
				break
			}
		}
	}

	return res
}

// lithoReference builds a referenceFixture from a litho Reader.
func lithoReference(ctx context.Context, r *pdf.Reader, id string, lastPage int) *referenceFixture {
	fixture := &referenceFixture{ID: id}

	numPages, err := r.PageCount(ctx)
	if err != nil {
		return nil
	}
	fixture.NumPages = numPages

	fixture.Metadata.Info.PDFFormatVersion = r.Version()
	fixture.Metadata.Info.IsLinearized = r.IsLinearized()

	if meta, err := r.Metadata(ctx); err == nil {
		fixture.Metadata.Info.Author = meta["Author"]
		fixture.Metadata.Info.Title = meta["Title"]
		fixture.Metadata.Info.Creator = meta["Creator"]
		fixture.Metadata.Info.Producer = meta["Producer"]
		fixture.Metadata.Info.CreationDate = meta["CreationDate"]
		fixture.Metadata.Info.ModDate = meta["ModDate"]
		fixture.Metadata.Info.Keywords = meta["Keywords"]
		fixture.Metadata.Info.Language = meta["Language"]
	}

	fixture.Metadata.Info.IsAcroFormPresent = r.HasAcroForm(ctx)

	if isPortfolio, err := r.IsPortfolio(ctx); err == nil {
		fixture.Metadata.Info.IsCollectionPresent = isPortfolio
	}

	effectiveLastPage := numPages
	if lastPage > 0 && lastPage < effectiveLastPage {
		effectiveLastPage = lastPage
	}

	for i := 1; i <= effectiveLastPage; i++ {
		if ctx.Err() != nil {
			return fixture
		}

		page := refPage{PageNumber: i}

		// PDF.js's textContent rejects glyphs whose transform falls
		// outside the viewBox (CropBox) but includes invisible and
		// artifact text. Match that policy so comparisons reflect the
		// same reality: strip clipped, keep the rest.
		text, err := r.ExtractText(ctx, i,
			pdf.WithInvisibleText(),
			pdf.WithArtifacts(),
		)
		if err != nil {
			page.Error = err.Error()
			fixture.Pages = append(fixture.Pages, page)
			continue
		}
		page.Text = text

		elems, err := r.ExtractTextElements(ctx, i)
		if err != nil {
			page.Error = err.Error()
			fixture.Pages = append(fixture.Pages, page)
			continue
		}

		page.Items = groupGlyphsIntoRuns(elems)
		page.ItemCount = len(page.Items)
		fixture.Pages = append(fixture.Pages, page)
	}

	return fixture
}

// groupGlyphsIntoRuns maps coalesced [models.TextElement] values to
// [refItem]s. [ExtractTextElements] already merges glyphs that came
// from the same text-showing operator; a further merge by Y coordinate
// collapses adjacent same-baseline runs into one item. PDF.js splits on
// operator boundaries, so same-Y adjacent runs stay separate there; the
// collapse here is intentional because litho and PDF.js diverge on how
// they segment operator boundaries and matching same-Y items is the
// best available proxy for PDF.js's visual-line grouping.
func groupGlyphsIntoRuns(elems []models.TextElement) []refItem {
	if len(elems) == 0 {
		return nil
	}

	var runs []refItem
	current := refItem{
		Str:    elems[0].Text,
		X:      elems[0].X,
		Y:      elems[0].Y,
		Width:  elems[0].Width,
		Height: elems[0].FontSize,
	}

	for i := 1; i < len(elems); i++ {
		e := elems[i]
		if math.Abs(e.Y-current.Y) < 0.5 {
			current.Str += e.Text
			current.Width = (e.X + e.Width) - current.X
		} else {
			runs = append(runs, current)
			current = refItem{
				Str:    e.Text,
				X:      e.X,
				Y:      e.Y,
				Width:  e.Width,
				Height: e.FontSize,
			}
		}
	}
	runs = append(runs, current)

	return runs
}

// compareFixtures compares litho's output against the PDF.js reference
// across all comparison dimensions.
func compareFixtures(file string, pdfjs *referenceFixture, litho *referenceFixture) pdfjsResult {
	res := pdfjsResult{
		PageCountMatch: true,
		MetadataMatch:  true,
		TextCharsMatch: true,
		TextWordsMatch: true,
		ItemCountMatch: true,
	}

	// Page count.
	if litho.NumPages != pdfjs.NumPages {
		res.PageCountMatch = false
		res.PageCountMismatch = &pdfjsPageCountMismatch{
			File:  file,
			Litho: litho.NumPages,
			PDFJS: pdfjs.NumPages,
		}
	}

	// Metadata.
	res.MetadataMismatches = compareMetadata(file, pdfjs.Metadata.Info, litho.Metadata.Info)
	if len(res.MetadataMismatches) > 0 {
		res.MetadataMatch = false
	}

	// Per-page: text content and item count.
	for _, pdfjsPage := range pdfjs.Pages {
		if pdfjsPage.Error != "" {
			continue
		}

		var lithoPage *refPage
		for j := range litho.Pages {
			if litho.Pages[j].PageNumber == pdfjsPage.PageNumber {
				lithoPage = &litho.Pages[j]
				break
			}
		}

		if lithoPage == nil {
			res.TextCharsMatch = false
			res.TextWordsMatch = false
			res.ItemCountMatch = false
			res.TextMismatches = append(res.TextMismatches, pdfjsTextMismatch{
				File:     file,
				Page:     pdfjsPage.PageNumber,
				Position: 0,
				Expected: truncate(pdfjsPage.Text, 80),
				Got:      "(no output from litho)",
			})
			continue
		}

		if lithoPage.Error != "" {
			if strings.TrimSpace(pdfjsPage.Text) != "" {
				res.TextCharsMatch = false
				res.TextWordsMatch = false
				res.TextMismatches = append(res.TextMismatches, pdfjsTextMismatch{
					File:     file,
					Page:     pdfjsPage.PageNumber,
					Position: 0,
					Expected: truncate(pdfjsPage.Text, 80),
					Got:      fmt.Sprintf("(error: %s)", lithoPage.Error),
				})
			}
			continue
		}

		// Text content comparison (ignoring word boundaries).
		if !comparePageTextContent(pdfjsPage.Text, lithoPage.Text) {
			res.TextCharsMatch = false
		}

		// Text comparison (with word boundaries).
		if mm := comparePageText(file, pdfjsPage.PageNumber, pdfjsPage.Text, lithoPage.Text); mm != nil {
			res.TextWordsMatch = false
			res.TextMismatches = append(res.TextMismatches, *mm)
		}

		// Item count comparison.
		if pdfjsPage.ItemCount > 0 && lithoPage.ItemCount != pdfjsPage.ItemCount {
			res.ItemCountMatch = false
			res.ItemCountMismatches = append(res.ItemCountMismatches, pdfjsItemCountMismatch{
				File:  file,
				Page:  pdfjsPage.PageNumber,
				Litho: lithoPage.ItemCount,
				PDFJS: pdfjsPage.ItemCount,
			})
		}
	}

	return res
}

// compareMetadata compares all metadata fields and returns mismatches.
func compareMetadata(file string, pdfjs, litho refInfo) []pdfjsMetadataMismatch {
	var mm []pdfjsMetadataMismatch

	addStr := func(field, lithoVal, pdfjsVal string) {
		if pdfjsVal != "" && lithoVal != pdfjsVal {
			mm = append(mm, pdfjsMetadataMismatch{File: file, Field: field, Litho: lithoVal, PDFJS: pdfjsVal})
		}
	}
	addBool := func(field string, lithoVal, pdfjsVal bool) {
		if lithoVal != pdfjsVal {
			mm = append(mm, pdfjsMetadataMismatch{
				File: file, Field: field,
				Litho: fmt.Sprintf("%v", lithoVal),
				PDFJS: fmt.Sprintf("%v", pdfjsVal),
			})
		}
	}

	addStr("PDFFormatVersion", litho.PDFFormatVersion, pdfjs.PDFFormatVersion)
	addStr("Title", litho.Title, pdfjs.Title)
	addStr("Author", litho.Author, pdfjs.Author)
	addStr("Creator", litho.Creator, pdfjs.Creator)
	addStr("Producer", litho.Producer, pdfjs.Producer)
	addStr("CreationDate", normalizePDFDate(litho.CreationDate), normalizePDFDate(pdfjs.CreationDate))
	addStr("ModDate", normalizePDFDate(litho.ModDate), normalizePDFDate(pdfjs.ModDate))
	addStr("Keywords", litho.Keywords, pdfjs.Keywords)
	addStr("Language", litho.Language, pdfjs.Language)
	addBool("IsLinearized", litho.IsLinearized, pdfjs.IsLinearized)
	addBool("IsAcroFormPresent", litho.IsAcroFormPresent, pdfjs.IsAcroFormPresent)
	addBool("IsCollectionPresent", litho.IsCollectionPresent, pdfjs.IsCollectionPresent)

	return mm
}

// comparePageTextContent compares text content ignoring whitespace and
// Unicode normalization differences. Returns true if the non-whitespace
// characters are identical after NFKC normalization.
func comparePageTextContent(pdfjsText, lithoText string) bool {
	return normalizeContent(pdfjsText) == normalizeContent(lithoText)
}

func normalizeContent(s string) string {
	return norm.NFKC.String(stripAllSpaces(s))
}

func stripAllSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r <= ' ': // ASCII control + space
			continue
		case r == 0xA0: // NBSP
			continue
		case r >= 0x2000 && r <= 0x200F: // various Unicode spaces and zero-width chars
			continue
		case r == 0x00AD: // soft hyphen
			continue
		case r == 0xFEFF: // BOM / zero-width no-break space
			continue
		case r == 0x202A || r == 0x202B || r == 0x202C: // bidi controls
			continue
		case r == 0x2028 || r == 0x2029: // line/paragraph separator
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// comparePageText normalizes and compares text for a single PDF page.
func comparePageText(file string, pageNum int, pdfjsText, lithoText string) *pdfjsTextMismatch {
	pdfjsNorm := normalizeWhitespace(pdfjsText)
	lithoNorm := normalizeWhitespace(lithoText)

	if lithoNorm == pdfjsNorm {
		return nil
	}

	pos, expected, got := textDiffSummary(pdfjsNorm, lithoNorm, 80)
	return &pdfjsTextMismatch{
		File:     file,
		Page:     pageNum,
		Position: pos,
		Expected: expected,
		Got:      got,
	}
}

// normalizePDFDate parses a PDF date string (either raw "D:YYYYMMDDHHmmSS+HH'MM'"
// or ISO 8601) into UTC and returns it in a canonical form for comparison.
// Returns the original string unchanged if parsing fails.
func normalizePDFDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if t, ok := parsePDFDate(s); ok {
		return t.UTC().Format(time.RFC3339)
	}
	return s
}

// parsePDFDate attempts to parse a date string in either PDF format
// ("D:YYYYMMDDHHmmSS+HH'MM'") or ISO 8601.
func parsePDFDate(s string) (time.Time, bool) {
	// Try ISO 8601 first.
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}

	// PDF date format: D:YYYYMMDDHHmmSS+HH'MM' (all parts after YYYY are optional).
	raw := strings.TrimPrefix(s, "D:")
	raw = strings.ReplaceAll(raw, "'", "")

	// Normalise the redundant-but-valid "Z0000" suffix that some producers emit.
	// After quote removal, "Z00'00'" becomes "Z0000". Go's Z0700 layout treats Z
	// and a numeric offset as mutually exclusive, so "Z0000" fails to parse.
	// Strip the trailing zero-offset digits when preceded by a literal Z.
	if idx := strings.Index(raw, "Z"); idx != -1 && idx == len(raw)-5 {
		raw = raw[:idx+1]
	}

	// Try progressively shorter formats.
	layouts := []string{
		"20060102150405-0700",
		"20060102150405Z0700",
		"20060102150405Z",
		"20060102150405",
		"200601021504",
		"2006010215",
		"20060102",
		"200601",
		"2006",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// textDiffSummary finds the first differing position and returns context
// snippets, measured in rune offsets so multi-byte UTF-8 characters
// (Arabic, CJK, Devanagari, and other non-ASCII scripts that dominate
// the mismatch set) are never sliced mid-codepoint. Byte-level slicing
// produced invalid UTF-8 at the snippet edges which serialised as
// U+FFFD in JSON output and misled later analysis into thinking the
// actual extraction contained replacement characters.
func textDiffSummary(expected, got string, maxLen int) (pos int, expSnippet, gotSnippet string) {
	expRunes := []rune(expected)
	gotRunes := []rune(got)
	minLen := min(len(expRunes), len(gotRunes))
	pos = minLen
	for i := range minLen {
		if expRunes[i] != gotRunes[i] {
			pos = i
			break
		}
	}

	start := max(pos-40, 0)
	endExp := min(pos+maxLen, len(expRunes))
	endGot := min(pos+maxLen, len(gotRunes))

	expSnippet = "..." + string(expRunes[start:endExp]) + "..."
	gotSnippet = "..." + string(gotRunes[start:endGot]) + "..."

	if len(got) == 0 {
		gotSnippet = "(empty)"
	}

	return pos, expSnippet, gotSnippet
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func dedupManifest(entries []manifestEntry) []manifestEntry {
	seen := make(map[string]bool)
	var unique []manifestEntry
	for _, e := range entries {
		if seen[e.File] {
			continue
		}
		seen[e.File] = true
		unique = append(unique, e)
	}
	return unique
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

func printPDFJSText(rpt pdfjsReport) {
	fmt.Println("PDF.js Corpus Comparison")
	fmt.Println("========================")

	s := rpt.Summary
	fmt.Printf("Total: %d  With refs: %d  Survival only: %d\n", s.Total, s.WithReferences, s.SurvivalOnly)
	fmt.Printf("Survival: %d/%d (%.1f%%)  Panics: %d\n", s.Survived, s.Total, pct(s.Survived, s.Total), s.Panics)
	fmt.Printf("Errors: %d  Skipped: %d  Timeouts: %d  Elapsed: %s\n\n", s.Errors, s.Skipped, s.Timeouts, rpt.Elapsed)

	// Details.
	if len(rpt.PageCountMismatches) > 0 {
		fmt.Printf("Page Count Mismatches (%d):\n", len(rpt.PageCountMismatches))
		for _, m := range rpt.PageCountMismatches {
			fmt.Printf("  %s: litho=%d pdfjs=%d\n", m.File, m.Litho, m.PDFJS)
		}
		fmt.Println()
	}

	if len(rpt.MetadataMismatches) > 0 {
		fmt.Printf("Metadata Mismatches (%d):\n", len(rpt.MetadataMismatches))
		for _, m := range rpt.MetadataMismatches {
			fmt.Printf("  %s: %s litho=%q pdfjs=%q\n", m.File, m.Field, m.Litho, m.PDFJS)
		}
		fmt.Println()
	}

	if len(rpt.TextMismatches) > 0 {
		fmt.Printf("Text Mismatches (%d):\n", len(rpt.TextMismatches))
		for _, m := range rpt.TextMismatches {
			fmt.Printf("  %s (page %d): first difference at position %d\n", m.File, m.Page, m.Position)
			fmt.Printf("    expected: %s\n", m.Expected)
			fmt.Printf("    got:      %s\n\n", m.Got)
		}
	}

	if len(rpt.ItemCountMismatches) > 0 {
		fmt.Printf("Item Count Mismatches (%d):\n", len(rpt.ItemCountMismatches))
		for _, m := range rpt.ItemCountMismatches {
			fmt.Printf("  %s (page %d): litho=%d pdfjs=%d\n", m.File, m.Page, m.Litho, m.PDFJS)
		}
		fmt.Println()
	}

	// Summary at the bottom for easy reading.
	w := s.WithReferences
	fmt.Println("Results:")
	fmt.Printf("  Page count:  %d/%d (%.1f%%)\n", s.PageCountMatch, w, pct(s.PageCountMatch, w))
	fmt.Printf("  Metadata:    %d/%d (%.1f%%)\n", s.MetadataMatches, w, pct(s.MetadataMatches, w))
	fmt.Printf("  Text (chars): %d/%d (%.1f%%)\n", s.TextCharsMatch, w, pct(s.TextCharsMatch, w))
	fmt.Printf("  Text (words): %d/%d (%.1f%%)\n", s.TextWordsMatch, w, pct(s.TextWordsMatch, w))
	fmt.Printf("  Item count:  %d/%d (%.1f%%)\n", s.ItemCountMatch, w, pct(s.ItemCountMatch, w))
}

func printPDFJSJSON(rpt pdfjsReport) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rpt); err != nil {
		fmt.Fprintf(os.Stderr, "encoding JSON: %v\n", err)
	}
}
