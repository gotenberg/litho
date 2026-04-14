package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

type profileInfo struct {
	dir     string
	profile models.PDFAProfile
}

// allProfiles is the set of VeraPDF profile directories the remediation
// corpus walks. Order determines the text-report row order.
var allProfiles = []profileInfo{
	{"PDF_A-1b", models.PDFA1b},
	{"PDF_A-2b", models.PDFA2b},
	{"PDF_A-2u", models.PDFA2u},
	{"PDF_A-2a", models.PDFA2a},
	{"PDF_A-3b", models.PDFA3b},
	{"PDF_A-4", models.PDFA4},
}

// profileResult holds the per-profile aggregate numbers used by both text
// and JSON output.
type profileResult struct {
	Profile          string `json:"profile"`
	Files            int    `json:"files"`
	Failing          int    `json:"failing"`
	Remediated       int    `json:"remediated"`
	FullyFixed       int    `json:"fully_fixed"`
	ViolationsBefore int    `json:"violations_before"`
	ViolationsAfter  int    `json:"violations_after"`
}

// regressionEntry describes a file whose remediation made it worse (more
// post-remediation violations than pre-remediation).
type regressionEntry struct {
	File    string   `json:"file"`
	Profile string   `json:"profile"`
	Before  int      `json:"before"`
	After   int      `json:"after"`
	New     []string `json:"new,omitempty"`
}

// remediateReport is the aggregate report emitted in text or JSON form.
type remediateReport struct {
	Profiles         []profileResult   `json:"profiles"`
	Files            int               `json:"files"`
	Failing          int               `json:"failing"`
	Remediated       int               `json:"remediated"`
	FullyFixed       int               `json:"fully_fixed"`
	ViolationsBefore int               `json:"violations_before"`
	ViolationsAfter  int               `json:"violations_after"`
	FixedPercent     float64           `json:"fixed_percent"`
	Regressions      []regressionEntry `json:"regressions"`
	Elapsed          string            `json:"elapsed"`
}

func runRemediate(args []string) int {
	fs := flag.NewFlagSet("remediate", flag.ExitOnError)
	corpus := fs.String("corpus", "", "Path to corpus testdata (auto-detected if empty)")
	format := fs.String("format", "text", "Output format: text or json")
	profile := fs.String("profile", "", "Filter to a single profile directory (e.g. PDF_A-2b)")
	aggressive := fs.Bool("aggressive", false, "Enable content-modifying fixers (Tier 2)")
	detail := fs.Bool("detail", false, "Print residual-violation clause breakdown after the summary")
	maxRegressions := fs.Int("max-regressions", -1, "Maximum number of files allowed to regress (-1 = no limit)")
	minFixedPct := fs.Float64("min-fixed-pct", 0, "Minimum aggregate fix percentage (0-100); below this the command exits non-zero")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}

	corpusRoot := *corpus
	if corpusRoot == "" {
		corpusRoot = findCorpusPath()
	}

	profiles := allProfiles
	if *profile != "" {
		profiles = nil
		for _, pi := range allProfiles {
			if pi.dir == *profile {
				profiles = append(profiles, pi)
				break
			}
		}
		if len(profiles) == 0 {
			fmt.Fprintf(os.Stderr, "unknown profile directory %q. Valid: %s\n",
				*profile, strings.Join(profileDirs(), ", "))
			return 1
		}
	}

	showProgress := *format == "text"
	rpt := runCorpus(context.Background(), corpusRoot, profiles, *aggressive, showProgress)

	switch *format {
	case "json":
		printRemediateJSON(rpt)
	default:
		printRemediateText(rpt)
		if *detail {
			printResidualBreakdown(context.Background(), corpusRoot, profiles, *aggressive)
		}
	}

	return checkRemediateThresholds(rpt, *maxRegressions, *minFixedPct)
}

func profileDirs() []string {
	out := make([]string, 0, len(allProfiles))
	for _, pi := range allProfiles {
		out = append(out, pi.dir)
	}
	return out
}

// runCorpus walks each profile directory, validates the failing PDFs,
// remediates them, re-validates, and aggregates the results into a
// [remediateReport]. The function is deliberately side-effect-free on
// report assembly. Output formatting happens in [printRemediateText] / [printRemediateJSON].
func runCorpus(ctx context.Context, corpusRoot string, profiles []profileInfo, aggressive, showProgress bool) remediateReport {
	start := time.Now()
	var rpt remediateReport

	for _, pi := range profiles {
		profileDir := filepath.Join(corpusRoot, pi.dir)
		if _, err := os.Stat(profileDir); os.IsNotExist(err) {
			if showProgress {
				fmt.Fprintf(os.Stderr, "  skip %s (no directory)\n", pi.dir)
			}
			continue
		}

		failFiles, err := collectFailFiles(profileDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "walking %s: %v\n", profileDir, err)
			continue
		}

		pr := profileResult{Profile: string(pi.profile), Files: len(failFiles)}

		for _, path := range failFiles {
			rpt.Files++
			processed := processFile(ctx, path, pi.profile, aggressive, &rpt)
			if processed.skipped {
				continue
			}
			pr.Failing++
			rpt.Failing++

			pr.ViolationsBefore += processed.before
			pr.ViolationsAfter += processed.after

			if processed.remediated {
				pr.Remediated++
				rpt.Remediated++
			}
			if processed.fullyFixed {
				pr.FullyFixed++
				rpt.FullyFixed++
			}
			if processed.after > processed.before {
				rel, _ := filepath.Rel(corpusRoot, path)
				rpt.Regressions = append(rpt.Regressions, regressionEntry{
					File:    rel,
					Profile: string(pi.profile),
					Before:  processed.before,
					After:   processed.after,
					New:     processed.newMsgs,
				})
			}
		}

		rpt.Profiles = append(rpt.Profiles, pr)
	}

	for _, pr := range rpt.Profiles {
		rpt.ViolationsBefore += pr.ViolationsBefore
		rpt.ViolationsAfter += pr.ViolationsAfter
	}
	fixed := rpt.ViolationsBefore - rpt.ViolationsAfter
	if rpt.ViolationsBefore > 0 {
		rpt.FixedPercent = float64(fixed) / float64(rpt.ViolationsBefore) * 100
	}
	rpt.Elapsed = time.Since(start).Round(time.Millisecond).String()
	return rpt
}

// fileResult holds the outcome of remediating a single corpus file.
// skipped=true means the file could not be parsed, validated, or was
// already compliant; such files do not contribute to any aggregate.
type fileResult struct {
	skipped    bool
	before     int
	after      int
	remediated bool
	fullyFixed bool
	newMsgs    []string // violation messages present after remediation but absent before
}

func processFile(ctx context.Context, path string, profile models.PDFAProfile, aggressive bool, rpt *remediateReport) fileResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileResult{skipped: true}
	}
	r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fileResult{skipped: true}
	}
	violations, err := r.ValidatePDFA(ctx, profile)
	if err != nil {
		return fileResult{skipped: true}
	}
	if len(violations) == 0 {
		return fileResult{skipped: true}
	}

	result := fileResult{before: len(violations), after: len(violations)}

	w := pdf.NewWriter(r)
	var remOpts []pdf.RemediateOption
	if aggressive {
		remOpts = append(remOpts, pdf.WithAggressiveFixes())
	}
	report, err := w.RemediatePDFA(ctx, profile, remOpts...)
	if err != nil {
		return result
	}
	if len(report.Fixed) == 0 {
		return result
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		return result
	}
	r2, err := pdf.Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		return result
	}
	after, err := r2.ValidatePDFA(ctx, profile)
	if err != nil {
		return result
	}

	result.after = len(after)
	result.remediated = true
	result.fullyFixed = len(after) == 0
	if len(after) > len(violations) {
		for _, v := range after {
			if isNew(v, violations) {
				result.newMsgs = append(result.newMsgs, v.Error())
			}
		}
	}
	return result
}

func collectFailFiles(profileDir string) ([]string, error) {
	var out []string
	err := filepath.Walk(profileDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".pdf") && strings.Contains(info.Name(), "fail") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func printRemediateText(rpt remediateReport) {
	fmt.Println("VeraPDF Remediation Report")
	fmt.Println("=========================")
	fmt.Println()

	for _, pr := range rpt.Profiles {
		fmt.Printf("%-12s  files=%3d  fail=%3d  remediated=%3d  fully_fixed=%3d  violations: %d → %d\n",
			pr.Profile, pr.Files, pr.Failing, pr.Remediated, pr.FullyFixed,
			pr.ViolationsBefore, pr.ViolationsAfter)
	}
	for _, reg := range rpt.Regressions {
		fmt.Printf("  REGRESSION %s: %d → %d violations\n", reg.File, reg.Before, reg.After)
		for _, msg := range reg.New {
			fmt.Printf("    NEW: %s\n", msg)
		}
	}
	fmt.Println()
	fmt.Printf("Total:  files=%d  failing=%d  remediated=%d  fully_fixed=%d\n",
		rpt.Files, rpt.Failing, rpt.Remediated, rpt.FullyFixed)
	fmt.Printf("Violations:  before=%d  after=%d  fixed=%d (%.1f%%)\n",
		rpt.ViolationsBefore, rpt.ViolationsAfter,
		rpt.ViolationsBefore-rpt.ViolationsAfter, rpt.FixedPercent)
	fmt.Printf("Elapsed: %s\n", rpt.Elapsed)
}

func printRemediateJSON(rpt remediateReport) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rpt); err != nil {
		fmt.Fprintf(os.Stderr, "encoding json: %v\n", err)
	}
}

// checkRemediateThresholds enforces CI-style failure thresholds. Returns the exit
// code that the process should use.
func checkRemediateThresholds(rpt remediateReport, maxRegressions int, minFixedPct float64) int {
	exit := 0
	if maxRegressions >= 0 && len(rpt.Regressions) > maxRegressions {
		fmt.Fprintf(os.Stderr, "\nFAIL: %d regressions exceed threshold %d\n",
			len(rpt.Regressions), maxRegressions)
		exit = 1
	}
	if rpt.FixedPercent < minFixedPct {
		fmt.Fprintf(os.Stderr, "\nFAIL: fixed percentage %.1f%% below threshold %.1f%%\n",
			rpt.FixedPercent, minFixedPct)
		exit = 1
	}
	return exit
}

func printResidualBreakdown(ctx context.Context, corpusRoot string, profiles []profileInfo, aggressive bool) {
	fmt.Println("\n=== Residual Violation Breakdown ===")

	type clauseInfo struct {
		clause      string
		description string
		count       int
		profiles    map[string]bool
		examples    []string
	}
	clauseMap := make(map[string]*clauseInfo)

	for _, pi := range profiles {
		profileDir := filepath.Join(corpusRoot, pi.dir)
		if _, err := os.Stat(profileDir); os.IsNotExist(err) {
			continue
		}
		failFiles, _ := collectFailFiles(profileDir)

		for _, path := range failFiles {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				continue
			}
			violations, err := r.ValidatePDFA(ctx, pi.profile)
			if err != nil || len(violations) == 0 {
				continue
			}

			w := pdf.NewWriter(r)
			var remOpts []pdf.RemediateOption
			if aggressive {
				remOpts = append(remOpts, pdf.WithAggressiveFixes())
			}
			report, err := w.RemediatePDFA(ctx, pi.profile, remOpts...)
			if err != nil || len(report.Residual) == 0 {
				continue
			}

			for _, v := range report.Residual {
				clause, desc := parseClauseAndDescription(v.Error())
				ci, ok := clauseMap[clause]
				if !ok {
					ci = &clauseInfo{clause: clause, description: desc, profiles: make(map[string]bool)}
					clauseMap[clause] = ci
				}
				ci.count++
				ci.profiles[string(pi.profile)] = true
				if len(ci.examples) < 2 { //nolint:mnd // two examples is enough for quick triage
					rel, _ := filepath.Rel(corpusRoot, path)
					ci.examples = append(ci.examples, rel)
				}
			}
		}
	}

	entries := make([]*clauseInfo, 0, len(clauseMap))
	for _, ci := range clauseMap {
		entries = append(entries, ci)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})

	for _, ci := range entries {
		profs := make([]string, 0, len(ci.profiles))
		for p := range ci.profiles {
			profs = append(profs, p)
		}
		sort.Strings(profs)
		fmt.Printf("\n[%s] count=%d profiles=%v\n", ci.clause, ci.count, profs)
		fmt.Printf("  %s\n", ci.description)
		for _, ex := range ci.examples {
			fmt.Printf("  e.g. %s\n", ex)
		}
	}
}

// parseClauseAndDescription pulls "clause X.Y.Z" and the free-form
// description out of a compliance violation error string. Both return
// "unknown" when the expected prefixes are not present so the output
// always has a row to display.
func parseClauseAndDescription(errStr string) (string, string) {
	clause := "unknown"
	if _, after, ok := strings.Cut(errStr, "clause "); ok {
		if before, _, ok := strings.Cut(after, " "); ok {
			clause = before
		}
	}
	desc := "unknown"
	if _, after, ok := strings.Cut(errStr, ": "); ok {
		if before, _, ok := strings.Cut(after, " ["); ok {
			desc = before
		} else {
			desc = after
		}
	}
	return clause, desc
}

func isNew(v error, before []error) bool {
	vs := v.Error()
	for _, b := range before {
		if b.Error() == vs {
			return false
		}
	}
	return true
}
