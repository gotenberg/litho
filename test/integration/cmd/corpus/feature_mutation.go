package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
)

// The failure counts the mutate-* features emit. Each is a cost metric (see
// costMetrics in baseline.go): a rise is the regression.
const (
	// metricWriteFailed counts PDFs whose mutation or write returned an error
	// the Writer does not document as a refusal.
	metricWriteFailed = "write_failed"
	// metricReopenFailed counts outputs litho cannot open again, or whose
	// page count it cannot read.
	metricReopenFailed = "reopen_failed"
	// metricQPDFFailed counts outputs qpdf --check reports a warning or error
	// class for that the input does not carry.
	metricQPDFFailed = "qpdf_failed"
	// metricPagesWrong counts outputs whose page count is not the one the
	// mutation promises.
	metricPagesWrong = "pages_wrong"
	// metricTextWrong counts outputs a page of which does not carry the text
	// the mutation promises.
	metricTextWrong = "text_wrong"
	// metricRulesetChanged counts (PDF, profile) cells whose litho rule set
	// moved further than the mutation's [ruleAllowance] lets it.
	metricRulesetChanged = "ruleset_changed"
	// metricPromiseBroken counts outputs that break the mutation's own
	// promise: a flattened form that still has fields, an encrypted output
	// that opens without its password or whose P entry breaks the reserved
	// bits.
	metricPromiseBroken = "promise_broken"
	// metricSignatureInvalid counts signed outputs whose new signature litho's
	// own verifier does not accept.
	metricSignatureInvalid = "signature_invalid"
)

// mutationFailure is one failed check. Metric names the counter it feeds.
type mutationFailure struct {
	Metric string `json:"metric"`
	Detail string `json:"detail"`
}

// mutationFeature runs one [pdf.Writer] mutation on every PDF of a corpus and
// checks the output against what the mutation promises. The gate's other
// cells validate and remediate, so without these a fixture could not protect
// a stamp, a page edit, auto-tagging, flattening, a signature, encryption or a
// redaction: a change that broke one would leave every cell where it was.
//
// Each feature, mutate-<name>, queues one mutation from [mutationSpecs] and
// writes the output. A documented refusal (a signature on an encrypted
// source, a redaction that cannot remove content exactly) skips the PDF, and
// any other error counts as write_failed. The output is then checked:
//
//   - it reopens in litho and reports a page count (reopen_failed);
//   - qpdf --check finds no warning or error class the input does not have
//     (qpdf_failed), when qpdf is on PATH. Without it the check is skipped
//     and the feature says so once on stderr;
//   - its page count is the one the mutation promises (pages_wrong);
//   - each page carries the text the mutation promises, read with
//     [pdf.WithHiddenText] so text an edit hides still counts (text_wrong);
//   - the mutation's own promise holds (promise_broken, signature_invalid):
//     see each spec;
//   - at the fixture's profile, litho's failed-rule set on the output differs
//     from the one on the input only as far as the mutation's [ruleAllowance]
//     says it must (ruleset_changed). This is litho against litho: the
//     compliance cell already compares litho with veraPDF.
//
// A litho fixture runs once per profile, but everything above except the rule
// set is the same at every profile. Those checks count on the PDF's first
// fixture only, its anchor (see [mutationFeature.Prepare]), so their metrics
// count PDFs and ruleset_changed counts (PDF, profile) cells. A PDF that
// produced no readable output counts on its anchor and skips its other
// fixtures, which have no output to validate. Every scored fixture emits every
// metric of its feature, zero included, so the baseline keeps a bar on each.
//
// The mutation runs once per PDF and its output is cached, so the per-profile
// fixtures only validate, two validations each, which is most of the cost.
//
// Run and Compare are safe for concurrent calls once Prepare returns.
//
// # Corpus evidence
//
//   - litho: the seven features take 18 s together with 4 workers on an idle
//     machine.
type mutationFeature struct {
	spec   mutationSpec
	signer *corpusSigner
	// qpdf is the qpdf binary, "" when it is not on PATH.
	qpdf string

	mu      sync.Mutex
	entries map[string]*mutationEntry
	// anchors maps a PDF path to the fixture that counts its PDF-level checks.
	anchors map[string]string
}

// mutationEntry memoises one PDF's mutation. mu serialises the fixtures of
// that PDF while the first computes it.
type mutationEntry struct {
	mu     sync.Mutex
	result *mutationResult
}

// mutationResult is one PDF's mutation and its PDF-level checks.
type mutationResult struct {
	// skip is non-empty when the input does not open or the Writer refused by
	// design: every fixture of the PDF skips.
	skip string
	// failures are the PDF-level failed checks, counted on the anchor.
	failures []mutationFailure
	// input is the source bytes; output the written bytes, nil when the write
	// failed; readable is false when litho cannot open output.
	input    []byte
	output   []byte
	readable bool
	plan     mutationPlan
}

// newMutationFeature builds the mutate-* feature for spec.
func newMutationFeature(spec mutationSpec) *mutationFeature {
	f := &mutationFeature{
		spec:    spec,
		entries: make(map[string]*mutationEntry),
		anchors: make(map[string]string),
	}
	if p, err := exec.LookPath("qpdf"); err == nil {
		f.qpdf = p
	}
	return f
}

// mutationFeatureFactories returns one factory per [mutationSpecs] entry, for
// the feature registry.
func mutationFeatureFactories() []FeatureFactory {
	factories := make([]FeatureFactory, 0, len(mutationSpecs))
	for _, spec := range mutationSpecs {
		factories = append(factories, func() Feature { return newMutationFeature(spec) })
	}
	return factories
}

// sharedSigner is the one [corpusSigner] of a run, made on first use.
//
//nolint:gochecknoglobals // process-wide lazily built signing key
var sharedSigner = sync.OnceValues(newCorpusSigner)

// Name returns "mutate-" and the spec's name.
func (f *mutationFeature) Name() string { return "mutate-" + f.spec.name }

// GoldenSource names the oracle: the input PDF itself, read by litho.
func (f *mutationFeature) GoldenSource() string { return "litho-self" }

// LoadGoldens is a no-op: the oracle is the input read at run time. The
// manifest still declares a golden directory so the runner schedules the
// feature.
func (f *mutationFeature) LoadGoldens(_ string) error { return nil }

// HasGolden returns true for every fixture: any PDF can be mutated.
func (f *mutationFeature) HasGolden(_ string) bool { return true }

// qpdfNotice makes the missing-qpdf notice print once per run rather than
// once per feature.
//
//nolint:gochecknoglobals // process-wide print-once guard
var qpdfNotice sync.Once

// Prepare records each PDF's anchor, the first of its fixtures in list order,
// which the runner keeps in path then profile order. Only the anchor counts
// the checks that do not depend on the profile. It also builds the signer a
// signing spec needs. It computes nothing else: the mutations run lazily in
// Run, under the per-fixture timeout and the worker pool.
func (f *mutationFeature) Prepare(_ context.Context, fixtures []Fixture) error {
	if f.spec.needsSigner {
		signer, err := sharedSigner()
		if err != nil {
			return fmt.Errorf("building the corpus signer: %w", err)
		}
		f.signer = signer
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, fx := range fixtures {
		if fx.SkipReason != "" {
			continue
		}
		if _, ok := f.anchors[fx.PDFPath]; !ok {
			f.anchors[fx.PDFPath] = fx.ID
		}
	}
	if f.qpdf == "" {
		qpdfNotice.Do(func() {
			fmt.Fprintln(os.Stderr, "mutate-*: qpdf is not on PATH, skipping the qpdf --check of every output")
		})
	}
	return nil
}

// isAnchor reports whether fixtureID counts its PDF's profile-independent
// checks. Without Prepare every fixture does.
func (f *mutationFeature) isAnchor(pdfPath, fixtureID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	anchor, ok := f.anchors[pdfPath]
	return !ok || anchor == fixtureID
}

// mutationOutput is the per-fixture blob Compare decodes.
type mutationOutput struct {
	Skip     string            `json:"skip,omitempty"`
	Profile  string            `json:"profile,omitempty"`
	Anchor   bool              `json:"anchor"`
	Failures []mutationFailure `json:"failures,omitempty"`
}

// Run mutates the fixture's PDF, once per PDF, and checks the output at the
// fixture's profile.
func (f *mutationFeature) Run(ctx context.Context, pdfPath, fixtureID string) ([]byte, error) {
	res := f.result(ctx, pdfPath)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("mutating %s: %w", pdfPath, err)
	}

	out := mutationOutput{Skip: res.skip, Anchor: f.isAnchor(pdfPath, fixtureID)}
	if out.Skip == "" {
		if out.Anchor {
			out.Failures = append(out.Failures, res.failures...)
		}
		if !res.readable && !out.Anchor {
			out.Skip = "no readable output, counted on " + f.anchorOf(pdfPath)
		}
	}
	if out.Skip == "" && res.readable {
		if head, _, ok := strings.Cut(fixtureID, "/"); ok && isProfileDir(head) {
			out.Profile = head
			if failure, ok := f.checkRules(ctx, res, head); ok {
				out.Failures = append(out.Failures, failure)
			}
		}
	}
	return json.Marshal(out)
}

// anchorOf returns the anchor fixture of pdfPath.
func (f *mutationFeature) anchorOf(pdfPath string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.anchors[pdfPath]
}

// result returns pdfPath's mutation, computing it on the first call. A result
// computed under a context that expired is returned but not kept, so the next
// fixture of the PDF tries again rather than inheriting a timeout.
func (f *mutationFeature) result(ctx context.Context, pdfPath string) *mutationResult {
	f.mu.Lock()
	e, ok := f.entries[pdfPath]
	if !ok {
		e = &mutationEntry{}
		f.entries[pdfPath] = e
	}
	f.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.result != nil {
		return e.result
	}
	res := f.mutate(ctx, pdfPath)
	if ctx.Err() == nil {
		e.result = res
	}
	return res
}

// mutate runs the spec's mutation on pdfPath and the PDF-level checks on its
// output.
func (f *mutationFeature) mutate(ctx context.Context, pdfPath string) *mutationResult {
	data, err := os.ReadFile(pdfPath) //nolint:gosec // pdfPath comes from the corpus testdata walk
	if err != nil {
		return &mutationResult{skip: fmt.Sprintf("read: %v", err)}
	}
	src, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return &mutationResult{skip: fmt.Sprintf("input does not open: %v", err)}
	}
	defer src.Close()

	in, err := readMutationInput(ctx, src, data, f.spec)
	if err != nil {
		return &mutationResult{skip: fmt.Sprintf("reading input: %v", err)}
	}

	res := &mutationResult{input: data}
	res.output, res.plan, res.skip, err = f.write(ctx, in, src)
	if err != nil {
		res.failures = append(res.failures, mutationFailure{Metric: metricWriteFailed, Detail: err.Error()})
		return res
	}
	if res.skip != "" {
		return res
	}

	out, err := openMutationOutput(ctx, res.output, res.plan.password)
	if err != nil {
		res.failures = append(res.failures, mutationFailure{Metric: metricReopenFailed, Detail: err.Error()})
		return res
	}
	defer out.Close()
	pages, err := out.PageCount(ctx)
	if err != nil {
		res.failures = append(res.failures, mutationFailure{Metric: metricReopenFailed, Detail: fmt.Sprintf("page count: %v", err)})
		return res
	}
	res.readable = true

	if pages != res.plan.pages {
		res.failures = append(res.failures, mutationFailure{
			Metric: metricPagesWrong,
			Detail: fmt.Sprintf("%d pages, want %d", pages, res.plan.pages),
		})
	}
	if failure, ok := f.checkQPDF(ctx, data, res.output, res.plan.password); ok {
		res.failures = append(res.failures, failure)
	}
	doc := &mutationOutputDoc{reader: out, data: res.output, pages: pages}
	res.failures = append(res.failures, f.spec.verify(ctx, f, in, doc, res.plan)...)
	return res
}

// write queues the spec's mutation on a Writer over src and writes it. A
// documented refusal comes back as skip; any other error as err.
func (f *mutationFeature) write(ctx context.Context, in *mutationInput, src *pdf.Reader) (output []byte, plan mutationPlan, skip string, err error) {
	w := pdf.NewWriter(src)
	defer w.Close()

	plan, err = f.spec.apply(ctx, f, in, w)
	if plan.release != nil {
		defer plan.release()
	}
	if err == nil {
		var buf bytes.Buffer
		if err = w.WriteTo(ctx, &buf); err == nil {
			return buf.Bytes(), plan, "", nil
		}
	}
	for _, refusal := range f.spec.refusals {
		if errors.Is(err, refusal) {
			return nil, plan, fmt.Sprintf("refused by design: %v", err), nil
		}
	}
	return nil, plan, "", err
}

// openMutationOutput opens output, with password when the mutation encrypted
// it.
func openMutationOutput(ctx context.Context, output []byte, password string) (*pdf.Reader, error) {
	var opts []pdf.OpenOption
	if password != "" {
		opts = append(opts, pdf.WithPassword(password))
	}
	r, err := pdf.Open(ctx, bytes.NewReader(output), int64(len(output)), opts...)
	if err != nil {
		return nil, fmt.Errorf("reopening the output: %w", err)
	}
	return r, nil
}

// checkRules validates the input and the output at profileDir and checks the
// difference against the spec's allowance. ok is false when nothing failed or
// the check could not run on the input, which is not the mutation's doing.
func (f *mutationFeature) checkRules(ctx context.Context, res *mutationResult, profileDir string) (mutationFailure, bool) {
	src, err := pdf.Open(ctx, bytes.NewReader(res.input), int64(len(res.input)))
	if err != nil {
		return mutationFailure{}, false
	}
	defer src.Close()
	inViolations, known, err := validateReader(ctx, src, profileDir)
	if !known || err != nil {
		return mutationFailure{}, false
	}
	inRules := distinctRuleIDs(inViolations)

	out, err := openMutationOutput(ctx, res.output, res.plan.password)
	if err != nil {
		return mutationFailure{Metric: metricRulesetChanged, Detail: err.Error()}, true
	}
	defer out.Close()
	outViolations, _, err := validateReader(ctx, out, profileDir)
	if err != nil {
		return mutationFailure{Metric: metricRulesetChanged, Detail: fmt.Sprintf("validating the output: %v", err)}, true
	}

	outRules := distinctRuleIDs(outViolations)
	allowance := f.spec.allowance(ruleContext{profile: profileDir, plan: res.plan, in: inRules, out: outRules})
	if msg := allowance.check(inRules, outRules); msg != "" {
		return mutationFailure{Metric: metricRulesetChanged, Detail: msg}, true
	}
	return mutationFailure{}, false
}

// Compare turns the blob Run wrote into a verdict. The fixture matches when no
// check failed, and every metric of the spec is emitted, zero included.
func (f *mutationFeature) Compare(fixtureID string, output []byte) Verdict {
	var got mutationOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	if got.Skip != "" {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: got.Skip}
	}

	metrics := make(map[string]int, len(f.spec.metrics()))
	for _, m := range f.spec.metrics() {
		metrics[m] = 0
	}
	reasons := make([]string, 0, len(got.Failures))
	for _, failure := range got.Failures {
		// A PDF with three wrong pages is one PDF with wrong text: each metric
		// counts the fixture once.
		metrics[failure.Metric] = 1
		reasons = append(reasons, failure.Metric+": "+truncate(failure.Detail, maxFailureDetail))
	}
	sort.Strings(reasons)

	v := Verdict{
		FixtureID: fixtureID,
		Match:     len(got.Failures) == 0,
		Metrics:   metrics,
		Reason:    strings.Join(reasons, "; "),
	}
	if v.Match {
		v.Score = 1
		return v
	}
	raw, err := json.Marshal(struct {
		File     string            `json:"file"`
		Profile  string            `json:"profile,omitempty"`
		Failures []mutationFailure `json:"failures"`
	}{fixtureID, got.Profile, got.Failures})
	if err == nil {
		v.Mismatches = []json.RawMessage{raw}
	}
	return v
}

// maxFailureDetail caps one failure's detail in the text scoreboard, where a
// rule diff or a text snippet would otherwise run to hundreds of characters.
// The JSON mismatch record carries it whole.
const maxFailureDetail = 160

// checkQPDF runs qpdf --check on the input and the output and reports the
// output's warning and error classes the input does not have. ok is false
// when qpdf is absent, could not run, or found nothing new.
func (f *mutationFeature) checkQPDF(ctx context.Context, input, output []byte, password string) (mutationFailure, bool) {
	if f.qpdf == "" {
		return mutationFailure{}, false
	}
	inKinds, err := qpdfCheck(ctx, f.qpdf, input, "")
	if err != nil {
		return mutationFailure{}, false
	}
	outKinds, err := qpdfCheck(ctx, f.qpdf, output, password)
	if err != nil {
		return mutationFailure{}, false
	}
	var fresh []string
	for k := range outKinds {
		if !inKinds[k] {
			fresh = append(fresh, k)
		}
	}
	if len(fresh) == 0 {
		return mutationFailure{}, false
	}
	sort.Strings(fresh)
	return mutationFailure{Metric: metricQPDFFailed, Detail: strings.Join(fresh, " | ")}, true
}

// qpdfDigits matches the numbers in a qpdf message: object numbers, offsets
// and counts, which a rewrite renumbers without changing what is wrong.
var qpdfDigits = regexp.MustCompile(`\d+`)

// qpdfCheck runs qpdf --check on data and returns the classes of warning and
// error it reports, each message with its file name and every number
// replaced. qpdf exits 0 when it finds nothing, 3 on warnings and 2 on errors;
// a non-zero exit with no message line still counts, as its exit status.
func qpdfCheck(ctx context.Context, qpdf string, data []byte, password string) (map[string]bool, error) {
	tmp, err := os.CreateTemp("", "litho-mutate-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("qpdf staging file: %w", err)
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("writing the qpdf staging file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("closing the qpdf staging file: %w", err)
	}

	args := []string{"--check"}
	if password != "" {
		args = append(args, "--password="+password)
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, qpdf, args...) //nolint:gosec // qpdf from LookPath, path is our staging file
	combined, runErr := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return nil, fmt.Errorf("running qpdf: %w", runErr)
	}
	return qpdfKinds(string(combined), path, exitErr), nil
}

// qpdfKinds extracts the message classes from qpdf --check output: its
// "WARNING:" lines and the "qpdf:" line of a fatal error. The line qpdf adds
// after any warning, "operation succeeded with warnings", names no class.
func qpdfKinds(output, path string, exitErr *exec.ExitError) map[string]bool {
	kinds := make(map[string]bool)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "WARNING:") && !strings.HasPrefix(line, "ERROR:") && !strings.HasPrefix(line, "qpdf:") {
			continue
		}
		if strings.Contains(line, "operation succeeded with warnings") {
			continue
		}
		line = strings.ReplaceAll(line, path, "FILE")
		kinds[qpdfDigits.ReplaceAllString(line, "N")] = true
	}
	if exitErr != nil && len(kinds) == 0 {
		kinds[fmt.Sprintf("exit status %d", exitErr.ExitCode())] = true
	}
	return kinds
}
