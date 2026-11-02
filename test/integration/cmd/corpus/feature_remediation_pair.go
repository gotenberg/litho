package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// remediationPairFeature remediates each PDF of a corpus to a PDF/A and a
// PDF/UA profile together ([pdf.WithPDFUA]), then validates the output with
// litho and veraPDF against both profiles. A PDF is scored once, at the pair
// its home profile belongs to (see [lithoPairOf]): a PDF/A home profile with
// the PDF/UA part of its PDF version, PDF/UA-1 with PDF/A-2b and PDF/UA-2 with
// PDF/A-4. An output is fully fixed when neither litho nor veraPDF finds a
// violation of either profile, so the cell catches a remediation that breaks
// one claim while it repairs the other, such as a pdfuaid:part left without
// the extension schema PDF/A-1 to PDF/A-3 require.
//
// It implements [BatchPreparer] and [PerPDFFeature]: Prepare remediates every
// PDF in parallel into one staging directory per pair, then runs veraPDF once
// per pair and flavour.
//
// # Corpus evidence
//
//   - litho: 106 of the 257 PDFs come out fully fixed, and litho and veraPDF
//     1.30.3 disagree on one, sharedicc_2b. The cell takes 15 to 20 s.
//   - litho: against the remediation cells at the same profiles, the pair
//     leaves no PDF/A rule RemediatePDFA fixes alone, and one PDF/UA rule
//     RemediatePDFUA fixes alone: extpredef_2b keeps ISO 14289-1, 7.1 t9. Its
//     extension schema redeclares dc:title as Text, which its Lang Alt value
//     does not match, so the PDF/A engine strips the title (ISO 19005-2,
//     6.6.2.3.1 t2) that the PDF/UA engine restores, and the last round is a
//     PDF/A one.
type remediationPairFeature struct {
	mu sync.RWMutex
	// verdicts, populated by Prepare and consumed by Run.
	verdicts map[string]*remediationPairOutput
}

func newRemediationPairFeature() *remediationPairFeature {
	return &remediationPairFeature{verdicts: make(map[string]*remediationPairOutput)}
}

func (*remediationPairFeature) Name() string         { return "remediation-pair" }
func (*remediationPairFeature) GoldenSource() string { return "verapdf-batched" }

// OncePerPDF scores each PDF once, at its home profile's pair, whatever
// profiles the manifest fans it out to.
func (*remediationPairFeature) OncePerPDF() {}

// LoadGoldens reads nothing: veraPDF judges the outputs live. dir, the
// compliance goldens the manifest names, only has to exist, so a corpus not
// provisioned yet shows "[no golden]".
func (*remediationPairFeature) LoadGoldens(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		return ErrNoGoldens
	}
	return nil
}

// HasGolden reports whether the PDF named fixtureID has a home profile, and
// so a pair to remediate to.
func (*remediationPairFeature) HasGolden(fixtureID string) bool {
	_, _, ok := lithoPairOf(fixtureID)
	return ok
}

// lithoPairs maps a home profile directory to the PDF/A and PDF/UA profile
// directories of the pair a litho fixture of that home profile is remediated
// to. PDF/UA-1 pairs with PDF/A-1 to PDF/A-3, and PDF/UA-2 with PDF/A-4 (see
// [pdf.ErrIncompatibleProfiles]); a PDF/UA fixture pairs with the PDF/A part
// most documents target.
//
//nolint:gochecknoglobals // static pairing table
var lithoPairs = map[string][2]string{
	"PDF_A-1b": {"PDF_A-1b", "PDF_UA-1"},
	"PDF_A-2a": {"PDF_A-2a", "PDF_UA-1"},
	"PDF_A-2b": {"PDF_A-2b", "PDF_UA-1"},
	"PDF_A-2u": {"PDF_A-2u", "PDF_UA-1"},
	"PDF_A-3b": {"PDF_A-3b", "PDF_UA-1"},
	"PDF_A-4":  {"PDF_A-4", "PDF_UA-2"},
	"PDF_A-4e": {"PDF_A-4e", "PDF_UA-2"},
	"PDF_A-4f": {"PDF_A-4f", "PDF_UA-2"},
	"PDF_UA-1": {"PDF_A-2b", "PDF_UA-1"},
	"PDF_UA-2": {"PDF_A-4", "PDF_UA-2"},
}

// lithoPairOf returns the PDF/A and PDF/UA profile directories the litho
// fixture named base is remediated to, from its home profile.
func lithoPairOf(base string) (pdfaDir, pdfuaDir string, ok bool) {
	home, ok := lithoHomeProfile(base)
	if !ok {
		return "", "", false
	}
	pair, ok := lithoPairs[home]
	return pair[0], pair[1], ok
}

// remediationPairOutput is the per-PDF blob Compare decodes: the distinct
// rules litho and veraPDF find in the output at each profile of the pair.
type remediationPairOutput struct {
	PDFA     string `json:"pdfa"`
	PDFUA    string `json:"pdfua"`
	LithoPre int    `json:"litho_pre"`
	// RemediationError is the error the remediation returned, which leaves
	// the PDF unremediated: it counts as no fix.
	RemediationError string   `json:"remediation_error,omitempty"`
	LithoPDFA        []string `json:"litho_pdfa_rules,omitempty"`
	LithoPDFUA       []string `json:"litho_pdfua_rules,omitempty"`
	VeraPDFA         []string `json:"verapdf_pdfa_rules,omitempty"`
	VeraPDFUA        []string `json:"verapdf_pdfua_rules,omitempty"`
	ParseFailed      bool     `json:"parse_failed,omitempty"`
	Error            string   `json:"error,omitempty"`
}

// Prepare remediates every PDF to its pair and runs veraPDF on the outputs,
// once per pair and flavour.
func (f *remediationPairFeature) Prepare(ctx context.Context, fixtures []Fixture) error {
	if _, err := exec.LookPath("verapdf"); err != nil {
		return errors.New("verapdf binary not found on PATH (install VeraPDF or run inside the provisioning container)")
	}
	byPair := make(map[[2]string][]Fixture)
	for _, fx := range fixtures {
		if fx.SkipReason != "" {
			continue
		}
		pdfaDir, pdfuaDir, ok := lithoPairOf(fx.ID)
		if !ok {
			continue
		}
		key := [2]string{pdfaDir, pdfuaDir}
		byPair[key] = append(byPair[key], fx)
	}
	keys := make([][2]string, 0, len(byPair))
	for k := range byPair {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+keys[i][1] < keys[j][0]+keys[j][1] })
	for _, k := range keys {
		if err := f.preparePair(ctx, k[0], k[1], byPair[k]); err != nil {
			return fmt.Errorf("pair %s and %s: %w", k[0], k[1], err)
		}
	}
	return nil
}

// preparePair remediates every fixture to the pair of profile directories
// pdfaDir and pdfuaDir, then asks veraPDF for its verdict at each.
func (f *remediationPairFeature) preparePair(ctx context.Context, pdfaDir, pdfuaDir string, fixtures []Fixture) error {
	pdfaProfile, okA := dirToPDFAProfile[pdfaDir]
	pdfuaProfile, okUA := dirToPDFUAProfile[pdfuaDir]
	pdfaFlavour, okFA := profileDirToVeraPDFFlavour(pdfaDir)
	pdfuaFlavour, okFUA := profileDirToVeraPDFFlavour(pdfuaDir)
	if !okA || !okUA || !okFA || !okFUA {
		return fmt.Errorf("no profile or veraPDF flavour for %s and %s", pdfaDir, pdfuaDir)
	}

	stagingDir, err := os.MkdirTemp("", "litho-remediation-pair-")
	if err != nil {
		return fmt.Errorf("staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	type staged struct {
		id   string
		path string
		out  remediationPairOutput
	}
	results := make([]staged, len(fixtures))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, fx := range fixtures {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			res := staged{id: fx.ID, out: remediationPairOutput{PDFA: pdfaDir, PDFUA: pdfuaDir}}
			// A panic is recorded, not lost: a lost worker would leave a
			// zero-valued entry that reads as a clean run.
			defer func() {
				if r := recover(); r != nil {
					res.out.Error = fmt.Sprintf("panic: %v", r)
				}
				results[i] = res
			}()
			fctx, cancel := context.WithTimeout(ctx, prepareFixtureTimeout)
			defer cancel()
			path, err := remediatePairOnce(fctx, fx.PDFPath, pdfaProfile, pdfuaProfile, stagingDir, &res.out)
			if err != nil {
				res.out.Error = err.Error()
			}
			res.path = path
		}()
	}
	wg.Wait()

	veraA, err := batchValidate(ctx, stagingDir, pdfaFlavour)
	if err != nil {
		return fmt.Errorf("verapdf batch at %s: %w", pdfaFlavour, err)
	}
	veraUA, err := batchValidate(ctx, stagingDir, pdfuaFlavour)
	if err != nil {
		return fmt.Errorf("verapdf batch at %s: %w", pdfuaFlavour, err)
	}

	for _, r := range results {
		out := r.out
		if out.Error == "" && r.path != "" {
			base := strings.TrimSuffix(filepath.Base(r.path), filepath.Ext(r.path))
			for _, side := range []struct {
				batch   map[string]veraVerdict
				flavour string
				rules   *[]string
			}{{veraA, pdfaFlavour, &out.VeraPDFA}, {veraUA, pdfuaFlavour, &out.VeraPDFUA}} {
				v, ok := side.batch[base]
				if !ok {
					// The batch run occasionally drops a job; a single-file
					// retry recovers the real verdict.
					var rerr error
					if v, rerr = singleValidate(ctx, r.path, side.flavour); rerr != nil {
						out.Error = fmt.Sprintf("verapdf at %s: %v", side.flavour, rerr)
						break
					}
				}
				if v.parseFailed {
					out.ParseFailed = true
					continue
				}
				*side.rules = v.rules
			}
		}
		f.mu.Lock()
		f.verdicts[r.id] = &out
		f.mu.Unlock()
	}
	return nil
}

// remediatePairOnce remediates the PDF at pdfPath to pdfa and pdfua together,
// records in out what litho finds before and after at each profile, and writes
// the output to stagingDir under the PDF's basename, whose path it returns. A
// remediation that fails leaves the PDF as it was, which out records with the
// litho findings of the source.
func remediatePairOnce(ctx context.Context, pdfPath string, pdfa models.PDFAProfile, pdfua models.PDFUAProfile, stagingDir string, out *remediationPairOutput) (string, error) {
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("pdf.Open: %w", err)
	}
	defer r.Close()
	preA, preUA, err := validatePair(ctx, r, pdfa, pdfua)
	if err != nil {
		return "", fmt.Errorf("validate pre: %w", err)
	}
	out.LithoPre = len(preA) + len(preUA)

	result := data
	w := pdf.NewWriter(r)
	// Removes the remediation output when the write below never runs.
	defer w.Close()
	if _, err := w.RemediatePDFA(ctx, pdfa, pdf.WithPDFUA(pdfua)); err != nil {
		out.RemediationError = err.Error()
	} else {
		var buf bytes.Buffer
		if err := w.WriteTo(ctx, &buf); err != nil {
			out.RemediationError = fmt.Sprintf("write: %v", err)
		} else {
			result = buf.Bytes()
		}
	}

	r2, err := pdf.Open(ctx, bytes.NewReader(result), int64(len(result)))
	if err != nil {
		out.ParseFailed = true
		return "", nil
	}
	defer r2.Close()
	out.LithoPDFA, out.LithoPDFUA, err = validatePair(ctx, r2, pdfa, pdfua)
	if err != nil {
		return "", fmt.Errorf("validate post: %w", err)
	}
	path := filepath.Join(stagingDir, strings.TrimSuffix(filepath.Base(pdfPath), filepath.Ext(pdfPath))+".pdf")
	if err := os.WriteFile(path, result, 0o600); err != nil { //nolint:gosec // G703: path is the staging dir this run made joined with a fixture's base name
		return "", fmt.Errorf("write staging: %w", err)
	}
	return path, nil
}

// validatePair returns the distinct rules litho finds in the document r reads
// at pdfa and at pdfua.
func validatePair(ctx context.Context, r *pdf.Reader, pdfa models.PDFAProfile, pdfua models.PDFUAProfile) (pdfaRules, pdfuaRules []string, err error) {
	a, err := r.ValidatePDFAReport(ctx, pdfa)
	if err != nil {
		return nil, nil, err
	}
	ua, err := r.ValidatePDFUAReport(ctx, pdfua)
	if err != nil {
		return nil, nil, err
	}
	return distinctRuleIDs(a), distinctRuleIDs(ua), nil
}

func (f *remediationPairFeature) Run(_ context.Context, _, fixtureID string) ([]byte, error) {
	f.mu.RLock()
	v := f.verdicts[fixtureID]
	f.mu.RUnlock()
	if v == nil {
		return json.Marshal(remediationPairOutput{Error: "no precomputed verdict"})
	}
	return json.Marshal(*v)
}

// Compare scores an output fully fixed when neither litho nor veraPDF finds a
// violation of either profile. It reports the metrics of the remediation
// cells, each count summed over the two profiles: litho_post and verapdf_post
// the distinct rules found after, disagreement how far litho's count is from
// veraPDF's at each profile, and parse_failed an output veraPDF could not
// read.
func (*remediationPairFeature) Compare(fixtureID string, output []byte) Verdict {
	var got remediationPairOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	if got.Error != "" {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: got.Error}
	}
	lithoPost := len(got.LithoPDFA) + len(got.LithoPDFUA)
	veraPost := len(got.VeraPDFA) + len(got.VeraPDFUA)
	fullyFixed := got.RemediationError == "" && !got.ParseFailed && lithoPost == 0 && veraPost == 0
	disagreement := abs(len(got.LithoPDFA)-len(got.VeraPDFA)) + abs(len(got.LithoPDFUA)-len(got.VeraPDFUA))

	// Every metric is emitted, zero included, so the baseline keeps a bar on
	// each (see remediationFeature.Compare).
	metrics := map[string]int{
		"litho_pre":    got.LithoPre,
		"litho_post":   lithoPost,
		"verapdf_post": veraPost,
		"disagreement": disagreement,
		"parse_failed": boolToInt(got.ParseFailed),
		"fully_fixed":  boolToInt(fullyFixed),
	}
	raw, _ := json.Marshal(struct {
		File string `json:"file"`
		remediationPairOutput
	}{File: fixtureID, remediationPairOutput: got})
	score := 0.0
	if fullyFixed {
		score = 1
	}
	return Verdict{
		FixtureID:  fixtureID,
		Match:      fullyFixed,
		Score:      score,
		Metrics:    metrics,
		Mismatches: []json.RawMessage{raw},
	}
}

// abs returns the absolute value of n.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
