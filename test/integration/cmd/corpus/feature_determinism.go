package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
)

// metricNondeterministic counts the (PDF, operation) cells whose two runs
// wrote different bytes or a different report. It is a cost metric (see
// costMetrics in baseline.go): a rise is the regression.
const metricNondeterministic = "nondeterministic"

// determinismOp is one operation the determinism feature runs twice.
type determinismOp struct {
	name string
	run  func(ctx context.Context, data []byte, home string) ([]byte, error)
}

// determinismFeature guards CONTRIBUTING "Deterministic output" over a whole
// corpus: each operation runs twice on the same PDF, and the two runs must
// write the same bytes and, for a remediation, the same report. Nothing is
// pinned: remediation derives a missing /ID from the content, so the default
// options are the ones the cell holds to it.
//
// The operations are the remediation at the PDF's home profile, the one its
// file name ends with (see [lithoHomeProfile]), in the default and the
// aggressive mode, and the mutate-* Writer mutations that draw no randomness.
// mutate-sign and mutate-encrypt are left out, since a signature and an
// encryption carry the random nonces, keys, salts and IVs their cryptography
// requires.
//
// A PDF has one fixture per profile of the manifest, and the operations are
// dealt out over them in fixture order (see [determinismFeature.Prepare]), so
// each fixture runs one operation, twice, well inside the per-fixture
// timeout. The fixture's profile directory plays no part: the verdict names
// the operation. Fixtures beyond the operations skip. Each scored fixture
// emits the metric, zero included, so the baseline keeps a bar on it.
//
// Run and Compare are safe for concurrent calls once Prepare returns.
//
// # Corpus evidence
//
//   - litho: the 966 cells of the 138 fixtures take 2 s with 4 workers, and
//     every operation writes the same bytes twice.
//   - litho: planted, a serializer that writes dictionary keys unsorted counts
//     787 of the 966 cells nondeterministic.
//   - litho filestruct_1b: planted, the random trailer /ID remediation drew
//     before it derived one counts its two remediation cells, the only ones
//     it moves.
type determinismFeature struct {
	ops []determinismOp

	mu sync.Mutex
	// assigned maps a fixture ID to the index of the operation it runs.
	assigned map[string]int
}

// newDeterminismFeature builds the determinism feature over the remediations
// and the randomness-free mutations.
func newDeterminismFeature() *determinismFeature {
	ops := []determinismOp{
		{name: "remediation", run: func(ctx context.Context, data []byte, home string) ([]byte, error) {
			return remediateForDeterminism(ctx, data, home, false)
		}},
		{name: "remediation-aggressive", run: func(ctx context.Context, data []byte, home string) ([]byte, error) {
			return remediateForDeterminism(ctx, data, home, true)
		}},
	}
	for _, spec := range mutationSpecs {
		if spec.needsSigner || spec.name == "encrypt" {
			continue
		}
		mf := newMutationFeature(spec)
		ops = append(ops, determinismOp{name: "mutate-" + spec.name, run: func(ctx context.Context, data []byte, _ string) ([]byte, error) {
			return mutateForDeterminism(ctx, mf, data)
		}})
	}
	return &determinismFeature{ops: ops, assigned: make(map[string]int)}
}

// Name returns "determinism".
func (f *determinismFeature) Name() string { return "determinism" }

// GoldenSource names the oracle: the first of the two runs.
func (f *determinismFeature) GoldenSource() string { return "litho-self" }

// LoadGoldens is a no-op: the oracle is the first run. The manifest still
// declares a golden directory so the runner schedules the feature.
func (f *determinismFeature) LoadGoldens(_ string) error { return nil }

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
	// Same reports whether the two runs wrote the same bytes; First and
	// Second are their SHA-256 digests.
	Same   bool   `json:"same"`
	First  string `json:"first,omitempty"`
	Second string `json:"second,omitempty"`
}

// Run runs the fixture's operation twice and compares the two results.
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

	base := strings.TrimSuffix(filepath.Base(pdfPath), ".pdf")
	home, ok := lithoHomeProfile(base)
	if !ok {
		return json.Marshal(determinismOutput{Op: op.name, Skip: "no home profile in the file name"})
	}
	data, err := os.ReadFile(pdfPath) //nolint:gosec // pdfPath comes from the corpus testdata walk
	if err != nil {
		return json.Marshal(determinismOutput{Op: op.name, Skip: fmt.Sprintf("read: %v", err)})
	}

	first := determinismRun(ctx, op, data, home)
	second := determinismRun(ctx, op, data, home)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("running %s twice on %s: %w", op.name, pdfPath, err)
	}
	out := determinismOutput{Op: op.name, Same: bytes.Equal(first, second)}
	if !out.Same {
		out.First = fmt.Sprintf("%x", sha256.Sum256(first))
		out.Second = fmt.Sprintf("%x", sha256.Sum256(second))
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
	if got.Same {
		v.Score = 1
		return v
	}
	v.Metrics[metricNondeterministic] = 1
	v.Reason = fmt.Sprintf("%s: two runs wrote %s and %s", got.Op, truncate(got.First, 12), truncate(got.Second, 12))
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
