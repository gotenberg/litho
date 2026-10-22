package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
)

// metricNondeterministic counts the (PDF, operation) cells whose runs wrote
// different bytes or a different report. It is a cost metric (see
// costMetrics in baseline.go): a rise is the regression.
const metricNondeterministic = "nondeterministic"

// metricRenderPagesChecked counts the pages the render pass drew in each of
// its runs. It is coverage, gated as good-when-higher: a drop is a page the
// cell stopped looking at.
const metricRenderPagesChecked = "render_pages_checked"

// metricRenderPagesNondeterministic counts the pages whose pixels differ
// between the render pass's runs. It is a cost metric (see costMetrics in
// baseline.go), the per-page reading of the cells nondeterministic counts.
const metricRenderPagesNondeterministic = "render_pages_nondeterministic"

// determinismRenderOp names the render pass among the operations.
const determinismRenderOp = "render"

// determinismRenderRuns is how many times the render pass draws a page, each
// time in a fresh Reader. Two runs keep the pass on the pdfjs and govdocs
// samples cheap; a coin flip a page draws goes unseen once in two per cell,
// and a gate run after run gives it another flip each time.
const determinismRenderRuns = 2

// determinismRenderOnlyDir is the golden directory name that asks for the
// render pass alone: a corpus other than litho names no home profile in its
// file names, and its goldens/render directory holds the fonts the pass
// draws unembedded fonts with.
const determinismRenderOnlyDir = "render"

// determinismRuns is how many times the determinism feature runs an
// operation. Three runs instead of two cost half as much time again and turn
// a coin-flip nondeterminism, the kind a Go map's iteration order breeds, from
// a one-in-two to a one-in-four chance of going unseen in a cell.
const determinismRuns = 3

// determinismBookmarkRuns is how many times the determinism feature runs a
// bookmark generation. Generation reads a document's text once and decides
// one thing from it, its body font size, so a tie it broke at random is one
// coin flip a run, on the one cell a fixture assigns it: eight runs let such
// a tie through once in 128.
const determinismBookmarkRuns = 8

// determinismOp is one operation the determinism feature runs several times.
type determinismOp struct {
	name string
	// runs is how many times the cell runs the operation, at least two.
	runs int
	run  func(ctx context.Context, data []byte, home string) ([]byte, error)
	// render marks the render pass, which needs no home profile and compares
	// its runs page by page (see [determinismFeature.runRender]).
	render bool
}

// determinismFeature guards CONTRIBUTING "Deterministic output" over a whole
// corpus: each operation runs several times on the same PDF (see
// [determinismRuns]), and every run must write the bytes the first wrote and,
// for a remediation, the same report. Nothing is
// pinned: remediation derives a missing /ID from the content, so the default
// options are the ones the cell holds to it.
//
// The operations are the remediation at the PDF's home profile, the one its
// file name ends with (see [lithoHomeProfile]), in the default and the
// aggressive mode, the mutate-* Writer mutations that draw no randomness, and
// two bookmark generations: [pdf.Writer.GenerateBookmarks] with the CLI's
// options in replace mode, and a [pdf.Split] of every page that reconstructs
// the outline. mutate-sign and mutate-encrypt are left out, since a signature
// and an encryption carry the random nonces, keys, salts and IVs their
// cryptography requires. The bookmark generations come last, so they take
// fixture slots that skipped before them and move no other assignment.
//
// A PDF has one fixture per profile of the manifest, and the operations are
// dealt out over them in fixture order (see [determinismFeature.Prepare]), so
// each fixture runs one operation, twice, well inside the per-fixture
// timeout. The fixture's profile directory plays no part: the verdict names
// the operation. Fixtures beyond the operations skip. Each scored fixture
// emits the metric, zero included, so the baseline keeps a bar on it.
//
// The render pass comes last on litho: it draws the first [renderMaxPages]
// pages at [renderDPI] with [pdf.Reader.RenderPage], [determinismRenderRuns]
// times, each run in a fresh Reader with a fresh font index over the render
// goldens' fonts, and compares the pixels page by page. It runs alone on the
// corpora whose manifest maps the feature to goldens/render (see
// [determinismRenderOnlyDir]), pdfjs and govdocs, whose PDFs name no home
// profile, one cell per PDF. A render cell emits [metricRenderPagesChecked]
// and [metricRenderPagesNondeterministic] beside the metric every cell
// emits. A page that fails to render the same way in both runs compares
// equal, as an operation's error does.
//
// Run and Compare are safe for concurrent calls once Prepare returns.
//
// # Corpus evidence
//
//   - litho: the 1557 scored cells of the 173 fixtures, at three runs and
//     eight for a bookmark generation, take 4 s with 4 workers, and every
//     operation writes the same bytes each time.
//   - litho sizetie_2b: planted, the tie in the body font size broken in map
//     order again, as before 5688d5c6, counts its generate-bookmarks or its
//     split-reconstruct cell nondeterministic, or both, in 10 of 10 runs.
//   - litho: planted, a serializer that writes dictionary keys unsorted counts
//     787 of the 966 cells nondeterministic.
//   - litho (217 files): flatten-transparency writes the same bytes three
//     times over on every file, the 20 it flattens included.
//   - litho filestruct_1b: planted, the random trailer /ID remediation drew
//     before it derived one counts its two remediation cells, the only ones
//     it moves.
//   - pdfjs issue14256: before 1e31d290 an inline image whose dictionary
//     wrote a key both abbreviated and in full drew either value, in Go map
//     order. With that fix reverted, the render pass counts the file
//     nondeterministic in 10 of 10 cells. The file is outside the pdfjs
//     render sample, so the pass runs over the whole corpus.
//   - litho, pdfjs: the render pass draws 341 pages of 225 files and 1520
//     pages of 1191 files twice each, every one the same pixels; 10 pdfjs
//     files draw no page. It takes 5 s and 64 s with 4 workers on a loaded
//     machine (load 8 to 12), the slowest pdfjs cell issue11518 at 15 s.
//   - govdocs: the pass draws 2941 pages of 1091 files the same twice, but
//     takes 145 s, and thread_400_400730 overruns the per-fixture timeout
//     on a loaded machine, so the corpus does not run it.
type determinismFeature struct {
	ops []determinismOp
	// render draws one page; tests plant a nondeterministic one.
	render pageRenderer

	mu sync.Mutex
	// assigned maps a fixture ID to the index of the operation it runs.
	assigned map[string]int
	// fontsDir holds the fonts the render goldens were drawn with, "" when
	// the testdata holds none.
	fontsDir string
}

// newDeterminismFeature builds the determinism feature over the remediations
// and the randomness-free mutations.
func newDeterminismFeature() *determinismFeature {
	ops := []determinismOp{
		{name: "remediation", runs: determinismRuns, run: func(ctx context.Context, data []byte, home string) ([]byte, error) {
			return remediateForDeterminism(ctx, data, home, false)
		}},
		{name: "remediation-aggressive", runs: determinismRuns, run: func(ctx context.Context, data []byte, home string) ([]byte, error) {
			return remediateForDeterminism(ctx, data, home, true)
		}},
	}
	for _, spec := range mutationSpecs {
		if spec.needsSigner || spec.name == "encrypt" {
			continue
		}
		mf := newMutationFeature(spec)
		ops = append(ops, determinismOp{name: "mutate-" + spec.name, runs: determinismRuns, run: func(ctx context.Context, data []byte, _ string) ([]byte, error) {
			return mutateForDeterminism(ctx, mf, data)
		}})
	}
	f := &determinismFeature{render: lithoRenderPage, assigned: make(map[string]int)}
	ops = append(ops,
		determinismOp{name: "generate-bookmarks", runs: determinismBookmarkRuns, run: func(ctx context.Context, data []byte, _ string) ([]byte, error) {
			return generateBookmarksForDeterminism(ctx, data)
		}},
		determinismOp{name: "split-reconstruct", runs: determinismBookmarkRuns, run: func(ctx context.Context, data []byte, _ string) ([]byte, error) {
			return splitReconstructForDeterminism(ctx, data)
		}},
		determinismOp{name: "flatten-transparency", runs: determinismRuns, run: func(ctx context.Context, data []byte, _ string) ([]byte, error) {
			return flattenForDeterminism(ctx, data)
		}},
		f.renderOp(),
	)
	f.ops = ops
	return f
}

// renderOp returns the render pass as an operation (see
// [determinismFeature.renderForDeterminism] for what a run writes).
func (f *determinismFeature) renderOp() determinismOp {
	return determinismOp{name: determinismRenderOp, runs: determinismRenderRuns, render: true, run: func(ctx context.Context, data []byte, _ string) ([]byte, error) {
		return f.renderForDeterminism(ctx, data)
	}}
}

// Name returns "determinism".
func (f *determinismFeature) Name() string { return "determinism" }

// GoldenSource names the oracle: the first of the two runs.
func (f *determinismFeature) GoldenSource() string { return "litho-self" }

// LoadGoldens reads no golden: the oracle is the first run. The manifest
// still declares a golden directory so the runner schedules the feature. A
// directory named [determinismRenderOnlyDir] keeps the render pass alone. The
// render pass draws with the fonts of the corpus's goldens/render/fonts, the
// fonts the render cell draws with.
func (f *determinismFeature) LoadGoldens(dir string) error {
	renderOnly := filepath.Base(dir) == determinismRenderOnlyDir
	renderDir := filepath.Join(filepath.Dir(dir), determinismRenderOnlyDir)
	if renderOnly {
		renderDir = dir
	}
	fontsDir := filepath.Join(renderDir, renderFontsDir)
	if info, err := os.Stat(fontsDir); err != nil || !info.IsDir() {
		fontsDir = ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fontsDir = fontsDir
	if renderOnly {
		f.ops = []determinismOp{f.renderOp()}
	}
	return nil
}

// HasGolden returns true for every fixture.
func (f *determinismFeature) HasGolden(_ string) bool { return true }

// Prepare deals the operations out over each PDF's fixtures, in the order the
// runner lists them, path then profile.
func (f *determinismFeature) Prepare(_ context.Context, fixtures []Fixture) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := make(map[string]int)
	for _, fx := range fixtures {
		if fx.SkipReason != "" {
			continue
		}
		f.assigned[fx.ID] = next[fx.PDFPath]
		next[fx.PDFPath]++
	}
	return nil
}

// determinismOutput is the per-fixture blob Compare decodes.
type determinismOutput struct {
	Op   string `json:"op,omitempty"`
	Skip string `json:"skip,omitempty"`
	// Same reports whether every run wrote the bytes the first wrote; First
	// and Second are the SHA-256 digests of the first run and of the first
	// run that differed from it.
	Same   bool   `json:"same"`
	First  string `json:"first,omitempty"`
	Second string `json:"second,omitempty"`
	// Render marks a render cell. Pages counts the pages it drew in each run
	// and Differed the pages whose pixels differ between two runs.
	Render   bool `json:"render,omitempty"`
	Pages    int  `json:"pages,omitempty"`
	Differed int  `json:"differed,omitempty"`
}

// Run runs the fixture's operation op.runs times and compares each result
// with the first.
func (f *determinismFeature) Run(ctx context.Context, pdfPath, fixtureID string) ([]byte, error) {
	f.mu.Lock()
	idx, ok := f.assigned[fixtureID]
	f.mu.Unlock()
	if !ok {
		// Without Prepare, every fixture runs the first operation.
		idx = 0
	}
	if idx >= len(f.ops) {
		return json.Marshal(determinismOutput{Skip: "every operation runs on another fixture of this PDF"})
	}
	op := f.ops[idx]

	var home string
	if !op.render {
		base := strings.TrimSuffix(filepath.Base(pdfPath), ".pdf")
		if home, ok = lithoHomeProfile(base); !ok {
			return json.Marshal(determinismOutput{Op: op.name, Skip: "no home profile in the file name"})
		}
	}
	data, err := os.ReadFile(pdfPath) //nolint:gosec // pdfPath comes from the corpus testdata walk
	if err != nil {
		return json.Marshal(determinismOutput{Op: op.name, Skip: fmt.Sprintf("read: %v", err)})
	}

	first := determinismRun(ctx, op, data, home)
	if op.render {
		return runRender(ctx, op, data, first, pdfPath)
	}
	out := determinismOutput{Op: op.name, Same: true}
	for range max(op.runs, 2) - 1 {
		next := determinismRun(ctx, op, data, home)
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("running %s on %s: %w", op.name, pdfPath, err)
		}
		if !bytes.Equal(first, next) {
			out.Same = false
			out.First = fmt.Sprintf("%x", sha256.Sum256(first))
			out.Second = fmt.Sprintf("%x", sha256.Sum256(next))
			break
		}
	}
	return json.Marshal(out)
}

// determinismRun runs op once and returns what it wrote, or its error, so two
// runs that fail the same way compare equal and two that fail differently do
// not.
func determinismRun(ctx context.Context, op determinismOp, data []byte, home string) []byte {
	out, err := op.run(ctx, data, home)
	if err != nil {
		return []byte("error: " + err.Error())
	}
	return out
}

// Compare turns the blob Run wrote into a verdict. The fixture matches when
// the two runs agree.
func (f *determinismFeature) Compare(fixtureID string, output []byte) Verdict {
	var got determinismOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	if got.Skip != "" {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: got.Skip}
	}
	v := Verdict{
		FixtureID: fixtureID,
		Match:     got.Same,
		Metrics:   map[string]int{metricNondeterministic: 0},
	}
	if got.Render {
		v.Metrics[metricRenderPagesChecked] = got.Pages
		v.Metrics[metricRenderPagesNondeterministic] = got.Differed
	}
	if got.Same {
		v.Score = 1
		return v
	}
	v.Metrics[metricNondeterministic] = 1
	v.Reason = fmt.Sprintf("%s: the first run wrote %s and a later one %s", got.Op, truncate(got.First, 12), truncate(got.Second, 12))
	if got.Render {
		v.Reason = fmt.Sprintf("%s: %d of %d pages drew different pixels in a later run", got.Op, got.Differed, got.Pages)
	}
	if raw, err := json.Marshal(struct {
		File   string `json:"file"`
		Op     string `json:"op"`
		First  string `json:"first"`
		Second string `json:"second"`
	}{fixtureID, got.Op, got.First, got.Second}); err == nil {
		v.Mismatches = []json.RawMessage{raw}
	}
	return v
}

// lithoHomeProfiles maps the suffix of a litho fixture's file name to its home
// profile directory, the profile the litho corpus README's Fixtures table
// lists it under.
//
//nolint:gochecknoglobals // static suffix table
var lithoHomeProfiles = map[string]string{
	"1b":  "PDF_A-1b",
	"2a":  "PDF_A-2a",
	"2b":  "PDF_A-2b",
	"2u":  "PDF_A-2u",
	"3b":  "PDF_A-3b",
	"4":   "PDF_A-4",
	"4e":  "PDF_A-4e",
	"4f":  "PDF_A-4f",
	"ua1": "PDF_UA-1",
	"ua2": "PDF_UA-2",
	// forbidden_mix mixes forbidden constructs and is listed at PDF/A-2b.
	"mix": "PDF_A-2b",
}

// lithoHomeProfile returns the home profile directory of the litho fixture
// named base, from the suffix after its last underscore.
func lithoHomeProfile(base string) (string, bool) {
	i := strings.LastIndexByte(base, '_')
	if i < 0 {
		return "", false
	}
	home, ok := lithoHomeProfiles[base[i+1:]]
	return home, ok
}

// remediateForDeterminism remediates data at the profile directory home and
// returns the bytes written followed by the report.
func remediateForDeterminism(ctx context.Context, data []byte, home string, aggressive bool) ([]byte, error) {
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening: %w", err)
	}
	defer r.Close()
	w := pdf.NewWriter(r)
	defer w.Close()

	var opts []pdf.RemediateOption
	if aggressive {
		opts = append(opts, pdf.WithAggressiveFixes())
	}
	var report *pdf.RemediationReport
	if p, ok := dirToPDFAProfile[home]; ok {
		report, err = w.RemediatePDFA(ctx, p, opts...)
	} else if p, ok := dirToPDFUAProfile[home]; ok {
		report, err = w.RemediatePDFUA(ctx, p, opts...)
	} else {
		return nil, fmt.Errorf("no remediation for profile %s", home)
	}
	if err != nil {
		return nil, fmt.Errorf("remediating at %s: %w", home, err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		return nil, fmt.Errorf("writing: %w", err)
	}
	for _, e := range report.Residual {
		out.WriteString("\nresidual: " + e.Error())
	}
	for _, fix := range report.Fixed {
		out.WriteString("\nfixed: " + fix)
	}
	for _, fix := range report.RolledBack {
		out.WriteString("\nrolled back: " + fix)
	}
	for _, refusal := range report.FontRefusals {
		out.WriteString("\nrefused font: " + refusal.String())
	}
	return out.Bytes(), nil
}

// mutateForDeterminism queues mf's mutation on data and returns the bytes
// written, or the refusal the Writer documents for it.
func mutateForDeterminism(ctx context.Context, mf *mutationFeature, data []byte) ([]byte, error) {
	src, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening: %w", err)
	}
	defer src.Close()
	in, err := readMutationInput(ctx, src, data, mf.spec)
	if err != nil {
		return nil, fmt.Errorf("reading the input: %w", err)
	}
	output, _, skip, err := mf.write(ctx, in, src)
	if err != nil {
		return nil, err
	}
	if skip != "" {
		return []byte(skip), nil
	}
	return output, nil
}

// determinismBookmarkOptions are the options `litho bookmarks` and
// `litho set-bookmarks --generate` generate with (defaultBookmarkGenOptions
// in cmd/litho), so the cell holds the CLI's generation to its bytes.
func determinismBookmarkOptions() pdf.BookmarkGenerationOptions {
	return pdf.BookmarkGenerationOptions{
		MaxDepth:              3,
		MinFontSizeRatio:      1.15,
		SkipHeaderFooter:      true,
		MaxTitleLength:        200,
		PreferStructureTree:   true,
		DetectNumbering:       true,
		HeadingScoreThreshold: 0.5,
	}
}

// generateBookmarksForDeterminism replaces data's outline with the one
// [pdf.Writer.GenerateBookmarks] detects, as `litho set-bookmarks --generate`
// does, and returns the bytes written.
func generateBookmarksForDeterminism(ctx context.Context, data []byte) ([]byte, error) {
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening: %w", err)
	}
	defer r.Close()
	w := pdf.NewWriter(r)
	defer w.Close()
	w.GenerateBookmarks(determinismBookmarkOptions(), pdf.BookmarkReplace)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		return nil, fmt.Errorf("writing: %w", err)
	}
	return out.Bytes(), nil
}

// flattenForDeterminism flattens the transparency of data with
// [pdf.Writer.FlattenTransparency] at its default resolution and returns the
// bytes written. A document with nothing to flatten is written whole all the
// same, as the flatten-transparency command writes it.
func flattenForDeterminism(ctx context.Context, data []byte) ([]byte, error) {
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening: %w", err)
	}
	defer r.Close()
	w := pdf.NewWriter(r)
	defer w.Close()
	if _, err := w.FlattenTransparency(ctx); err != nil {
		return nil, fmt.Errorf("flattening: %w", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out, pdf.WithFullRewrite()); err != nil {
		return nil, fmt.Errorf("writing: %w", err)
	}
	return out.Bytes(), nil
}

// splitReconstructForDeterminism splits every page of data, in order, into a
// document whose outline [pdf.Split] reconstructs from the detected headings,
// and returns the bytes written.
func splitReconstructForDeterminism(ctx context.Context, data []byte) ([]byte, error) {
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening: %w", err)
	}
	defer r.Close()
	n, err := r.PageCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("counting pages: %w", err)
	}
	pages := make([]int, n)
	for i := range pages {
		pages[i] = i + 1
	}
	var out bytes.Buffer
	if err := pdf.Split(ctx, r, pages, &out, pdf.WithBookmarks(pdf.BookmarkOptions{
		Handling:   pdf.BookmarkReconstruct,
		Generation: determinismBookmarkOptions(),
	})); err != nil {
		return nil, fmt.Errorf("splitting: %w", err)
	}
	return out.Bytes(), nil
}

// runRender runs the render pass op.runs times on data, first being the first
// run's output, and compares the runs line by line, a line a page. A PDF that
// draws no page, the same way in every run, skips.
func runRender(ctx context.Context, op determinismOp, data, first []byte, pdfPath string) ([]byte, error) {
	out := determinismOutput{Op: op.name, Same: true, Render: true}
	firstLines := bytes.Split(first, []byte("\n"))
	out.Pages = len(firstLines) - 1
	differed := make(map[int]bool)
	for range max(op.runs, 2) - 1 {
		next := determinismRun(ctx, op, data, "")
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("running %s on %s: %w", op.name, pdfPath, err)
		}
		if bytes.Equal(first, next) {
			continue
		}
		if out.Same {
			out.First = fmt.Sprintf("%x", sha256.Sum256(first))
			out.Second = fmt.Sprintf("%x", sha256.Sum256(next))
		}
		out.Same = false
		nextLines := bytes.Split(next, []byte("\n"))
		for i := 1; i < len(firstLines); i++ {
			if i >= len(nextLines) || !bytes.Equal(firstLines[i], nextLines[i]) {
				differed[i] = true
			}
		}
	}
	if out.Same && out.Pages == 0 {
		return json.Marshal(determinismOutput{Op: op.name, Skip: "draws no page: " + string(first)})
	}
	out.Differed = len(differed)
	if !out.Same && out.Differed == 0 {
		// The runs disagree on the page count, or on the error that kept the
		// document shut, so no page line differs, yet the cell does not hold.
		out.Differed = max(out.Pages, 1)
	}
	return json.Marshal(out)
}

// renderForDeterminism opens data in a fresh Reader and draws each of its
// first [renderMaxPages] pages at [renderDPI]. It writes a first line naming
// the page count, then one line a page, the SHA-256 digest of its pixels or
// the error that stopped it, so two runs compare page by page. A document
// that does not open writes the error as its one line. The fonts are indexed
// afresh, so the runs share no font index an option value keeps.
func (f *determinismFeature) renderForDeterminism(ctx context.Context, data []byte) ([]byte, error) {
	f.mu.Lock()
	fontsDir := f.fontsDir
	f.mu.Unlock()
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return []byte("open: " + err.Error()), nil
	}
	defer r.Close()
	n, err := r.PageCount(ctx)
	if err != nil {
		return []byte("page count: " + err.Error()), nil
	}
	var opts []pdf.RenderOption
	if fontsDir != "" {
		opts = append(opts, pdf.WithRenderFontFiles(os.DirFS(fontsDir)))
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "pages: %d", n)
	for page := 1; page <= min(n, renderMaxPages); page++ {
		img, err := f.render(ctx, r, page, renderDPI, opts...)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("rendering page %d: %w", page, ctxErr)
			}
			fmt.Fprintf(&out, "\nerror: %v", err)
			continue
		}
		fmt.Fprintf(&out, "\n%x", pixelDigest(img))
	}
	return out.Bytes(), nil
}

// pixelDigest returns the SHA-256 digest of img's bounds and of the colour of
// every pixel, so two images that draw the same page hash the same.
func pixelDigest(img image.Image) []byte {
	h := sha256.New()
	b := img.Bounds()
	writeInts(h, b.Min.X, b.Min.Y, b.Max.X, b.Max.Y)
	if rgba, ok := img.(*image.RGBA); ok {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			i := rgba.PixOffset(b.Min.X, y)
			h.Write(rgba.Pix[i : i+4*b.Dx()])
		}
		return h.Sum(nil)
	}
	row := make([]byte, 0, 8*b.Dx())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row = row[:0]
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			row = binary.BigEndian.AppendUint16(row, uint16(r))  //nolint:gosec // RGBA returns 16-bit values in a uint32
			row = binary.BigEndian.AppendUint16(row, uint16(g))  //nolint:gosec // RGBA returns 16-bit values in a uint32
			row = binary.BigEndian.AppendUint16(row, uint16(bl)) //nolint:gosec // RGBA returns 16-bit values in a uint32
			row = binary.BigEndian.AppendUint16(row, uint16(a))  //nolint:gosec // RGBA returns 16-bit values in a uint32
		}
		h.Write(row)
	}
	return h.Sum(nil)
}

// writeInts writes each of vs to h as a big-endian 64-bit integer.
func writeInts(h hash.Hash, vs ...int) {
	for _, v := range vs {
		h.Write(binary.BigEndian.AppendUint64(nil, uint64(int64(v)))) //nolint:gosec // a two's-complement reinterpretation, injective, which is all a digest needs
	}
}
