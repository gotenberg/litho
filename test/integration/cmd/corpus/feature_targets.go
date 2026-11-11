package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// outputProfile is one profile an output target conforms to: a PDF/A or a
// PDF/UA profile, and the veraPDF flavour that validates it.
type outputProfile struct {
	pdfa    models.PDFAProfile
	pdfua   models.PDFUAProfile
	flavour string
}

// name is the profile's name, as the models package spells it.
func (p outputProfile) name() string {
	if p.pdfa != "" {
		return string(p.pdfa)
	}
	return string(p.pdfua)
}

// outputTarget is one output flavour the targets cell converts each fixture
// to: a PDF version, or one or two profiles, and the version the header must
// then declare.
type outputTarget struct {
	// ID names the target in the scores and the metrics (pass_<ID>).
	ID string
	// Version is the version [pdf.WithConvertVersion] asks for, "" for the
	// one the profiles decide.
	Version string
	// Profiles are the profiles [pdf.WithConvertPDFA] and
	// [pdf.WithConvertPDFUA] ask for, PDF/A first.
	Profiles []outputProfile
	// Header is the version the file's header must declare: the one asked,
	// or the one the profile builds on (ISO 19005-1, clause 1 for PDF/A-1;
	// ISO 19005-2 and -3, 6.1.2 and ISO 14289-1, 6.1 for ISO 32000-1, which
	// Convert writes as 1.7; ISO 19005-4, 6.1.2 and ISO 14289-2, 6.1 for
	// PDF 2.0).
	Header string
}

// options returns the conversion options of the target.
func (t outputTarget) options() []pdf.ConvertOption {
	var opts []pdf.ConvertOption
	if t.Version != "" {
		opts = append(opts, pdf.WithConvertVersion(t.Version))
	}
	for _, p := range t.Profiles {
		if p.pdfa != "" {
			opts = append(opts, pdf.WithConvertPDFA(p.pdfa))
		} else {
			opts = append(opts, pdf.WithConvertPDFUA(p.pdfua))
		}
	}
	return opts
}

// versionOnly reports whether the target names a version and no profile.
func (t outputTarget) versionOnly() bool { return len(t.Profiles) == 0 }

func pdfaProfile(p models.PDFAProfile, flavour string) outputProfile {
	return outputProfile{pdfa: p, flavour: flavour}
}

func pdfuaProfile(p models.PDFUAProfile, flavour string) outputProfile {
	return outputProfile{pdfua: p, flavour: flavour}
}

// outputTargets are the flavours the targets cell converts to, in the order
// it reports them: every version [pdf.WithConvertVersion] takes but 1.7, the
// plain conversion's, every PDF/A profile [pdf.WithConvertPDFA] takes,
// both PDF/UA profiles, and the two pairs a single file can meet (as
// [pdf.WithPDFUA] pairs them).
//
//nolint:gochecknoglobals // static target table
var outputTargets = []outputTarget{
	{ID: "v1.4", Version: "1.4", Header: "1.4"},
	{ID: "v1.5", Version: "1.5", Header: "1.5"},
	{ID: "v1.6", Version: "1.6", Header: "1.6"},
	{ID: "v2.0", Version: "2.0", Header: "2.0"},
	{ID: "a1b", Profiles: []outputProfile{pdfaProfile(models.PDFA1b, "1b")}, Header: "1.4"},
	{ID: "a1a", Profiles: []outputProfile{pdfaProfile(models.PDFA1a, "1a")}, Header: "1.4"},
	{ID: "a2b", Profiles: []outputProfile{pdfaProfile(models.PDFA2b, "2b")}, Header: "1.7"},
	{ID: "a2u", Profiles: []outputProfile{pdfaProfile(models.PDFA2u, "2u")}, Header: "1.7"},
	{ID: "a2a", Profiles: []outputProfile{pdfaProfile(models.PDFA2a, "2a")}, Header: "1.7"},
	{ID: "a3b", Profiles: []outputProfile{pdfaProfile(models.PDFA3b, "3b")}, Header: "1.7"},
	{ID: "a3u", Profiles: []outputProfile{pdfaProfile(models.PDFA3u, "3u")}, Header: "1.7"},
	{ID: "a3a", Profiles: []outputProfile{pdfaProfile(models.PDFA3a, "3a")}, Header: "1.7"},
	{ID: "a4", Profiles: []outputProfile{pdfaProfile(models.PDFA4, "4")}, Header: "2.0"},
	{ID: "a4e", Profiles: []outputProfile{pdfaProfile(models.PDFA4e, "4e")}, Header: "2.0"},
	{ID: "ua1", Profiles: []outputProfile{pdfuaProfile(models.PDFUA1, "ua1")}, Header: "1.7"},
	{ID: "ua2", Profiles: []outputProfile{pdfuaProfile(models.PDFUA2, "ua2")}, Header: "2.0"},
	{ID: "a2a+ua1", Profiles: []outputProfile{pdfaProfile(models.PDFA2a, "2a"), pdfuaProfile(models.PDFUA1, "ua1")}, Header: "1.7"},
	{ID: "a4+ua2", Profiles: []outputProfile{pdfaProfile(models.PDFA4, "4"), pdfuaProfile(models.PDFUA2, "ua2")}, Header: "2.0"},
}

// Metrics of the targets cell. pass_<target> counts the fixtures each
// target passes on; the rest count (fixture, target) cells.
const (
	// metricTargetPassPrefix prefixes the count of fixtures a target passes
	// on.
	metricTargetPassPrefix = "pass_"
	// metricReportDisagrees counts the targets whose conversion report says
	// the file conforms while litho's validator finds a violation of a
	// profile, or the reverse.
	metricReportDisagrees = "report_disagrees"
	// metricVeraPDFDisagrees counts the sampled targets whose verdict veraPDF
	// and litho's validator give apart, at one of the target's profiles.
	metricVeraPDFDisagrees = "verapdf_disagrees"
	// metricVeraPDFUnavailable counts the sampled targets veraPDF did not
	// judge: no verapdf on PATH, another release than the pin, a run that
	// failed.
	metricVeraPDFUnavailable = "verapdf_unavailable"
	// metricRendererDisagrees counts the sampled targets one of whose first
	// pages poppler draws apart from litho.
	metricRendererDisagrees = "renderer_disagrees"
	// metricVersionStrictFailed counts the sampled version targets
	// pdfcpu's strict validation refuses.
	metricVersionStrictFailed = "version_strict_failed"
	// metricPdfcpuUnavailable counts the sampled version targets pdfcpu did
	// not judge.
	metricPdfcpuUnavailable = "pdfcpu_unavailable"
	// metricOracleTargets counts the targets the oracles ran on, the
	// sample's coverage.
	metricOracleTargets = "oracle_targets"
)

// targetsScoreTolerance is the drop a target's recorded score may take: the
// scores are 0 or 1, so any drop is a regression.
const targetsScoreTolerance = 0.5

// targetsSizeSlack is how far, in points, a page's width or height may lie
// from the plain conversion's: a target lays the same pages out, so only the
// rounding of the numbers the file writes may differ.
const targetsSizeSlack = 0.01

// defaultOracleSample is the share of the fixtures the oracles run on, one
// in this many (see [oracleSampled]): about 120 of the 980 sources of the
// converter corpora, enough for each oracle to see every format, at a cost
// the matrix bears.
const defaultOracleSample = 8

// oracleSampled reports whether the oracles run on a fixture: the first byte
// of its ID's SHA-256 lies below 256/oneIn, so the sample stays put when
// other fixtures come and go, as the render cell's does. oneIn 1 takes every
// fixture, 0 none.
func oracleSampled(id string, oneIn int) bool {
	if oneIn <= 0 {
		return false
	}
	sum := sha256.Sum256([]byte(id))
	return int(sum[0]) < 256/oneIn
}

// targetConverter converts a source at its settings to one output target,
// the zero target being the plain conversion. It is [convertTarget]
// outside tests; a test plants a defect through it.
type targetConverter func(ctx context.Context, src []byte, s htmlSettings, t outputTarget) ([]byte, *pdf.ConvertReport, error)

// convertTarget converts src at s with the target's options.
func convertTarget(ctx context.Context, src []byte, s htmlSettings, t outputTarget) ([]byte, *pdf.ConvertReport, error) {
	s.Print = append(append([]pdf.ConvertOption(nil), s.Print...), t.options()...)
	return convertWithReport(ctx, src, s)
}

// targetsFeature judges every output flavour of a conversion by how it
// looks. Each source of a converter corpus the diff cell scores is
// converted once plain, the base, and once per [outputTargets] entry, at
// the options of its options.json. A target scores 1 on a fixture when:
//
//   - it converts, and its header declares the version the target implies;
//   - litho's validator finds no violation of the target's profiles, and
//     the conversion report's Conformance.Residual is empty; a report that
//     disagrees with the validator also counts in report_disagrees;
//   - it looks like the base: the same page count and sizes, each page's
//     text equal, white space aside, the same count of links and of outline
//     items, and its first [renderMaxPages] pages drawn by litho at
//     [renderDPI] matching the base's under the render cell's rule.
//
// On a sample of the fixtures (see [oracleSampled]), three oracles judge
// the files too, and a target passes there only when each oracle that
// applies ran and agreed: veraPDF, batched per flavour, gives the
// verdict litho's validator gives at each profile (verapdf_disagrees,
// verapdf_unavailable); the pinned poppler draws the first pages as litho
// draws them (renderer_disagrees, poppler_unavailable); for a version
// target, pdfcpu's strict validation accepts the file
// (version_strict_failed, pdfcpu_unavailable). A missing tool is no pass.
//
// Everything runs in Prepare, where the oracles can batch; Run looks the
// result up.
type targetsFeature struct {
	convert targetConverter
	// vera validates a directory at a veraPDF flavour, [batchValidate]
	// outside tests; veraFound reports whether a verapdf is there to run.
	vera      func(ctx context.Context, dir, flavour string) (map[string]veraVerdict, error)
	veraFound func() error
	// poppler renders a batch, [renderWithPoppler] outside tests, and
	// popplerPin returns the version it must report, "" for any.
	poppler    func(ctx context.Context, dir string, jobs []popplerJob) (string, error)
	popplerPin func() string
	// pdfcpu validates files strictly, [validateWithPdfcpu] outside tests,
	// and pdfcpuPin returns the version it must report, "" for any.
	pdfcpu    func(ctx context.Context, dir string, files []string) (map[string]pdfcpuVerdict, string, error)
	pdfcpuPin func() string
	targets   []outputTarget

	mu       sync.RWMutex
	workers  int
	oneIn    int
	m        *Manifest
	fontDirs []string
	outputs  map[string][]byte
}

func newTargetsFeature() *targetsFeature {
	return &targetsFeature{
		convert:    convertTarget,
		vera:       batchValidate,
		veraFound:  func() error { _, err := exec.LookPath("verapdf"); return err },
		poppler:    renderWithPoppler,
		popplerPin: popplerPin,
		pdfcpu:     validateWithPdfcpu,
		pdfcpuPin:  pdfcpuPin,
		targets:    outputTargets,
		workers:    runtime.GOMAXPROCS(0),
		oneIn:      defaultOracleSample,
		outputs:    make(map[string][]byte),
	}
}

func (f *targetsFeature) Name() string         { return "targets" }
func (f *targetsFeature) GoldenSource() string { return "litho-self+verapdf+poppler+pdfcpu" }

// GoldensOf runs the cell on the corpora the diff cell runs on.
func (f *targetsFeature) GoldensOf() string { return "diff" }

// ScoreTolerance is the drop each recorded score may take.
func (f *targetsFeature) ScoreTolerance() float64 { return targetsScoreTolerance }

// SetRunOptions takes the run's worker count and oracle sample.
func (f *targetsFeature) SetRunOptions(workers, oracleSample int) {
	f.mu.Lock()
	f.workers, f.oneIn = max(1, workers), oracleSample
	f.mu.Unlock()
}

// SetManifest records the corpus, whose hostile documents the cell leaves
// out and whose references say whether its sources are Office documents.
func (f *targetsFeature) SetManifest(m *Manifest) {
	f.mu.Lock()
	f.m = m
	f.mu.Unlock()
}

// LoadGoldens checks the diff goldens are there, which says the corpus is
// provisioned, and finds the fixture fonts. The cell compares litho with
// itself and its oracles, so it reads no golden.
func (f *targetsFeature) LoadGoldens(dir string) error {
	if entries, err := os.ReadDir(dir); err != nil || len(entries) == 0 {
		return ErrNoGoldens
	}
	fonts, err := fixtureFontDirs(fixtureFontsRoot(), diffFontsTarget)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.fontDirs = fonts
	f.mu.Unlock()
	return nil
}

// HasGolden scores every fixture but a build corpus's hostile documents,
// which only the robustness cell reads.
func (f *targetsFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	m := f.m
	f.mu.RUnlock()
	return m == nil || !unreferencedFixtures(m)[fixtureID]
}

// office reports whether the corpus's sources are Office documents.
func (f *targetsFeature) office() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.m != nil && f.m.References != nil && f.m.References.Engine != engineChromium
}

// targetsOutput is the cell's per-fixture output.
type targetsOutput struct {
	// Error says why the base was not converted or read: every target
	// then scores 0.
	Error string `json:"error,omitempty"`
	// Pages is the base's page count.
	Pages   int            `json:"pages"`
	Sampled bool           `json:"sampled,omitempty"`
	Targets []targetResult `json:"targets"`
}

// targetResult is one target's checks on one fixture.
type targetResult struct {
	ID   string `json:"id"`
	Pass bool   `json:"pass"`
	// Reasons lists each check that failed.
	Reasons      []string `json:"reasons,omitempty"`
	ConvertError bool     `json:"convert_error,omitempty"`
	// ReportDisagrees marks a report whose Residual says otherwise than
	// litho's validator.
	ReportDisagrees bool `json:"report_disagrees,omitempty"`
	// Oracles are set on a sampled fixture.
	OracleChecked      bool `json:"oracle_checked,omitempty"`
	VeraPDFDisagrees   bool `json:"verapdf_disagrees,omitempty"`
	VeraPDFUnavailable bool `json:"verapdf_unavailable,omitempty"`
	RendererDisagrees  bool `json:"renderer_disagrees,omitempty"`
	PopplerUnavailable bool `json:"poppler_unavailable,omitempty"`
	StrictFailed       bool `json:"version_strict_failed,omitempty"`
	PdfcpuUnavailable  bool `json:"pdfcpu_unavailable,omitempty"`

	// litho holds, per veraPDF flavour of the target, litho's validator's
	// verdict and the rules it fails, for the veraPDF comparison.
	litho map[string]lithoVerdict
	// file and pages are the staged target file, relative to the staging
	// directory, and the pages litho drew of it there.
	file  string
	pages int
}

// lithoVerdict is litho's validation of a target at one profile.
type lithoVerdict struct {
	clean bool
	rules []string
}

// fail records a failed check.
func (r *targetResult) fail(format string, args ...any) {
	r.Pass = false
	r.Reasons = append(r.Reasons, fmt.Sprintf(format, args...))
}

// targetsDoc is what the cell reads of one conversion.
type targetsDoc struct {
	sizes   []diffPageSize
	texts   []string
	links   int
	outline int
	renders []image.Image
}

// readTargetsDoc reads a conversion: each page's size and text, up to
// maxDiffPages, its link and outline item counts, and its first
// renderMaxPages pages drawn at renderDPI.
func readTargetsDoc(ctx context.Context, data []byte) (*targetsDoc, error) {
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer r.Close()
	sizes, err := pageSizes(ctx, r)
	if err != nil {
		return nil, err
	}
	marks, err := r.Bookmarks(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the outline: %w", err)
	}
	doc := &targetsDoc{sizes: sizes, outline: len(flattenOutline(marks))}
	for p := 1; p <= len(sizes); p++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text, err := r.ExtractText(ctx, p, pdf.WithArtifacts())
		if errors.Is(err, pdf.ErrRequiresOCR) {
			text, err = "", nil
		}
		if err != nil {
			return nil, fmt.Errorf("extract text of page %d: %w", p, err)
		}
		doc.texts = append(doc.texts, strings.Join(strings.Fields(text), " "))
		annots, err := r.ExtractAnnotations(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("read annotations of page %d: %w", p, err)
		}
		for _, a := range annots {
			if a.Subtype == "Link" {
				doc.links++
			}
		}
		if p <= renderMaxPages {
			img, err := r.RenderPage(ctx, p, pdf.WithRenderDPI(renderDPI))
			if err != nil {
				return nil, fmt.Errorf("render page %d: %w", p, err)
			}
			doc.renders = append(doc.renders, img)
		}
	}
	return doc, nil
}

// looksAlike records in res each way doc differs from the base.
func looksAlike(res *targetResult, base, doc *targetsDoc) {
	if len(doc.sizes) != len(base.sizes) {
		res.fail("%d pages, the base has %d", len(doc.sizes), len(base.sizes))
	}
	for i := range min(len(doc.sizes), len(base.sizes)) {
		got, want := doc.sizes[i], base.sizes[i]
		if math.Abs(got.W-want.W) > targetsSizeSlack || math.Abs(got.H-want.H) > targetsSizeSlack {
			res.fail("page %d is %.2fx%.2f pt, the base's %.2fx%.2f", i+1, got.W, got.H, want.W, want.H)
			break
		}
	}
	for i := range min(len(doc.texts), len(base.texts)) {
		if doc.texts[i] != base.texts[i] {
			res.fail("page %d's text differs: %s", i+1, textDifference(base.texts[i], doc.texts[i]))
			break
		}
	}
	if doc.links != base.links {
		res.fail("%d links, the base has %d", doc.links, base.links)
	}
	if doc.outline != base.outline {
		res.fail("%d outline items, the base has %d", doc.outline, base.outline)
	}
	for i := range min(len(doc.renders), len(base.renders)) {
		if d := comparePage(doc.renders[i], base.renders[i]); !d.Match {
			detail := fmt.Sprintf("%d of %d pixels differ", d.DiffPixels, d.Pixels)
			if d.Error != "" {
				detail = d.Error
			}
			res.fail("page %d draws apart from the base: %s", i+1, detail)
		}
	}
}

// maxTextDifference bounds the excerpt of a text difference a reason
// quotes.
const maxTextDifference = 60

// textDifference quotes where two texts part: the words from the first
// that differs on, each side cut to maxTextDifference bytes.
func textDifference(want, got string) string {
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	if j := strings.LastIndexByte(want[:i], ' '); j >= 0 {
		i = j + 1
	}
	cut := func(s string) string {
		if len(s) > maxTextDifference {
			return s[:maxTextDifference] + "..."
		}
		return s
	}
	return fmt.Sprintf("base %q, target %q", cut(want[i:]), cut(got[i:]))
}

// targetsStaged is one fixture's progress through Prepare.
type targetsStaged struct {
	id  string
	out targetsOutput
}

// Prepare converts and checks every fixture, then runs the oracles on the
// sample's files in batches, and scores them. Run looks the result up.
func (f *targetsFeature) Prepare(ctx context.Context, fixtures []Fixture) error {
	start := time.Now()
	staging, err := os.MkdirTemp("", "litho-targets-")
	if err != nil {
		return fmt.Errorf("staging dir: %w", err)
	}
	defer os.RemoveAll(staging)

	f.mu.RLock()
	workers, oneIn, fonts := f.workers, f.oneIn, f.fontDirs
	f.mu.RUnlock()
	office := f.office()

	var todo []Fixture
	for _, fx := range fixtures {
		if fx.SkipReason == "" && f.HasGolden(fx.ID) {
			todo = append(todo, fx)
		}
	}
	results := make([]*targetsStaged, len(todo))
	pool(workers, len(todo), func(i int) {
		fctx, cancel := context.WithTimeout(ctx, prepareFixtureTimeout)
		defer cancel()
		sampled := oracleSampled(todo[i].ID, oneIn)
		results[i] = f.prepareOne(fctx, todo[i], office, fonts, sampled, staging, i)
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	converted := time.Since(start)
	sampled := 0
	for _, res := range results {
		if res.out.Sampled {
			sampled++
		}
	}
	if sampled > 0 {
		if err := f.runOracles(ctx, staging, results, workers); err != nil {
			return err
		}
	}

	f.mu.Lock()
	for _, res := range results {
		data, err := json.Marshal(res.out)
		if err != nil {
			data = []byte(`{"error":"marshal"}`)
		}
		f.outputs[res.id] = data
	}
	f.mu.Unlock()
	fmt.Fprintf(os.Stderr, "targets: %d fixtures, %d targets each, converted and checked in %s; %d sampled for the oracles, judged in %s\n",
		len(todo), len(f.targets), converted.Round(time.Millisecond), sampled, (time.Since(start) - converted).Round(time.Millisecond))
	return nil
}

// pool runs fn for 0..n-1 on at most workers goroutines.
func pool(workers, n int, fn func(i int)) {
	work := make(chan int)
	var wg sync.WaitGroup
	for range max(1, workers) {
		wg.Go(func() {
			for i := range work {
				fn(i)
			}
		})
	}
	for i := range n {
		work <- i
	}
	close(work)
	wg.Wait()
}

// prepareOne converts one fixture plain and to every target, and runs
// litho's checks. On a sampled fixture, it stages each target's file and
// litho's drawing of its first pages under staging, for the oracles.
func (f *targetsFeature) prepareOne(ctx context.Context, fx Fixture, office bool, fonts []string, sampled bool, staging string, n int) (res *targetsStaged) {
	res = &targetsStaged{id: fx.ID, out: targetsOutput{Sampled: sampled}}
	// A panic past the library's own recover is a failed conversion, not a
	// crashed run.
	defer func() {
		if r := recover(); r != nil {
			res.out = targetsOutput{Error: fmt.Sprintf("panic: %v", r)}
		}
	}()
	src, settings, err := sourceSettings(fx.PDFPath, office, fonts)
	if err != nil {
		res.out.Error = err.Error()
		return res
	}
	data, _, err := f.convert(ctx, src, settings, outputTarget{})
	if err != nil {
		res.out.Error = fmt.Sprintf("base conversion: %v", err)
		return res
	}
	base, err := readTargetsDoc(ctx, data)
	if err != nil {
		res.out.Error = fmt.Sprintf("base: %v", err)
		return res
	}
	res.out.Pages = len(base.sizes)
	for k, t := range f.targets {
		if err := ctx.Err(); err != nil {
			res.out.Error = err.Error()
			return res
		}
		r := f.checkTarget(ctx, src, settings, t, base)
		if sampled && !r.ConvertError {
			if err := stageTarget(&r, staging, fmt.Sprintf("f%d-t%d", n, k)); err != nil {
				r.fail("staging for the oracles: %v", err)
			}
		}
		r.data, r.doc = nil, nil
		res.out.Targets = append(res.out.Targets, r.targetResult)
	}
	return res
}

// checkedTarget is a target's result with the file and the reading the
// oracles need.
type checkedTarget struct {
	targetResult
	data []byte
	doc  *targetsDoc
}

// checkTarget converts src to t and runs litho's checks against base.
func (f *targetsFeature) checkTarget(ctx context.Context, src []byte, s htmlSettings, t outputTarget, base *targetsDoc) checkedTarget {
	res := checkedTarget{targetResult: targetResult{ID: t.ID, Pass: true}}
	data, report, err := f.convert(ctx, src, s, t)
	if err != nil {
		res.ConvertError = true
		res.fail("conversion: %v", err)
		return res
	}
	res.data = data
	if header := "%PDF-" + t.Header; !bytes.HasPrefix(data, []byte(header)) {
		res.fail("header %q, want %s", firstLine(data[:min(len(data), 16)]), header)
	}
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		res.fail("open: %v", err)
		return res
	}
	defer r.Close()
	if !t.versionOnly() {
		f.checkConformance(ctx, &res.targetResult, r, t, report)
	}
	doc, err := readTargetsDoc(ctx, data)
	if err != nil {
		res.fail("read: %v", err)
		return res
	}
	res.doc = doc
	looksAlike(&res.targetResult, base, doc)
	return res
}

// maxReasonRules bounds the rule IDs a reason lists before it counts the
// rest.
const maxReasonRules = 6

// checkConformance validates r at each of the target's profiles, holds the
// report's Residual to the verdict, and records litho's verdicts for the
// veraPDF comparison.
func (f *targetsFeature) checkConformance(ctx context.Context, res *targetResult, r *pdf.Reader, t outputTarget, report *pdf.ConvertReport) {
	res.litho = make(map[string]lithoVerdict, len(t.Profiles))
	validatorClean := true
	for _, p := range t.Profiles {
		var (
			violations []pdf.Violation
			err        error
		)
		if p.pdfa != "" {
			violations, err = r.ValidatePDFAReport(ctx, p.pdfa)
		} else {
			violations, err = r.ValidatePDFUAReport(ctx, p.pdfua)
		}
		if err != nil {
			res.fail("validating %s: %v", p.name(), err)
			validatorClean = false
			continue
		}
		rules := distinctRuleIDs(violations)
		res.litho[p.flavour] = lithoVerdict{clean: len(rules) == 0, rules: rules}
		if len(rules) > 0 {
			validatorClean = false
			res.fail("litho's validator finds %s violations: %s", p.name(), listRules(rules))
		}
	}
	switch {
	case report == nil || report.Conformance == nil:
		res.fail("the conversion report carries no conformance")
		res.ReportDisagrees = validatorClean
	case len(report.Conformance.Residual) > 0:
		res.fail("the conversion report lists %d residual violations", len(report.Conformance.Residual))
		res.ReportDisagrees = validatorClean
	default:
		res.ReportDisagrees = !validatorClean
		if res.ReportDisagrees {
			res.fail("the conversion report says the file conforms")
		}
	}
}

// listRules joins the first maxReasonRules rule IDs and counts the rest.
func listRules(rules []string) string {
	if len(rules) <= maxReasonRules {
		return strings.Join(rules, " ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(rules[:maxReasonRules], " "), len(rules)-maxReasonRules)
}

// stageTarget writes a target's file and litho's drawing of its first pages
// under staging, as name.pdf and name-litho-p<page>.png.
func stageTarget(r *checkedTarget, staging, name string) error {
	r.file = name + ".pdf"
	if err := os.WriteFile(filepath.Join(staging, r.file), r.data, 0o600); err != nil {
		return err
	}
	if r.doc == nil {
		return nil
	}
	for i, img := range r.doc.renders {
		if err := writeImage(filepath.Join(staging, lithoPNG(r.file, i+1)), img); err != nil {
			return err
		}
	}
	r.pages = len(r.doc.renders)
	return nil
}

// lithoPNG is the staged path of litho's drawing of page of the staged file.
func lithoPNG(file string, page int) string {
	return fmt.Sprintf("%s-litho-p%d.png", strings.TrimSuffix(file, ".pdf"), page)
}

// writeImage writes img to path as a PNG.
func writeImage(path string, img image.Image) error {
	out, err := os.Create(path) //nolint:gosec // a path under the cell's staging directory
	if err != nil {
		return err
	}
	if err := png.Encode(out, img); err != nil {
		_ = out.Close() //nolint:errcheck // the encoding error is the one to report
		return err
	}
	return out.Close()
}

// Run returns the output Prepare scored.
func (f *targetsFeature) Run(_ context.Context, _, fixtureID string) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if out, ok := f.outputs[fixtureID]; ok {
		return out, nil
	}
	return json.Marshal(targetsOutput{Error: "not prepared"})
}

// Compare turns one fixture's checks into a verdict: matched when every
// target passes, scored by the share that pass, with one score per target.
func (f *targetsFeature) Compare(fixtureID string, output []byte) Verdict {
	var out targetsOutput
	if err := json.Unmarshal(output, &out); err != nil {
		out = targetsOutput{Error: fmt.Sprintf("decode output: %v", err)}
	}
	m := map[string]int{
		metricConvertError: 0, metricReportDisagrees: 0, metricVeraPDFDisagrees: 0, metricVeraPDFUnavailable: 0,
		metricRendererDisagrees: 0, metricPopplerUnavailable: 0, metricVersionStrictFailed: 0,
		metricPdfcpuUnavailable: 0, metricOracleTargets: 0,
	}
	scores := make(map[string]float64, len(f.targets))
	for _, t := range f.targets {
		scores[t.ID] = 0
		m[metricTargetPassPrefix+t.ID] = 0
	}
	v := Verdict{FixtureID: fixtureID, Metrics: m, Scores: scores}
	if out.Error != "" {
		m[metricConvertError]++
		v.Reason = out.Error
		return v
	}
	passed := 0
	for _, r := range out.Targets {
		if _, known := scores[r.ID]; !known {
			continue
		}
		if r.Pass {
			passed++
			scores[r.ID] = 1
			m[metricTargetPassPrefix+r.ID] = 1
		} else if data, err := json.Marshal(struct {
			Target  string   `json:"target"`
			Reasons []string `json:"reasons"`
		}{r.ID, r.Reasons}); err == nil {
			v.Mismatches = append(v.Mismatches, data)
		}
		m[metricConvertError] += boolToInt(r.ConvertError)
		m[metricReportDisagrees] += boolToInt(r.ReportDisagrees)
		m[metricOracleTargets] += boolToInt(r.OracleChecked)
		m[metricVeraPDFDisagrees] += boolToInt(r.VeraPDFDisagrees)
		m[metricVeraPDFUnavailable] += boolToInt(r.VeraPDFUnavailable)
		m[metricRendererDisagrees] += boolToInt(r.RendererDisagrees)
		m[metricPopplerUnavailable] += boolToInt(r.PopplerUnavailable)
		m[metricVersionStrictFailed] += boolToInt(r.StrictFailed)
		m[metricPdfcpuUnavailable] += boolToInt(r.PdfcpuUnavailable)
	}
	v.Score = float64(passed) / float64(len(f.targets))
	v.Match = passed == len(f.targets)
	if !v.Match {
		v.Reason = fmt.Sprintf("%d of %d targets pass", passed, len(f.targets))
	}
	return v
}
