package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

type profileInfo struct {
	dir     string
	profile models.PDFAProfile
}

func main() {
	ctx := context.Background()
	corpusRoot := "test/integration/testdata/verapdf"

	profiles := []profileInfo{
		{"PDF_A-1b", models.PDFA1b},
		{"PDF_A-2b", models.PDFA2b},
		{"PDF_A-2u", models.PDFA2u},
		{"PDF_A-2a", models.PDFA2a},
		{"PDF_A-3b", models.PDFA3b},
		{"PDF_A-4", models.PDFA4},
	}

	aggressive := false
	for _, arg := range os.Args[1:] {
		if arg == "--aggressive" {
			aggressive = true
		}
	}

	start := time.Now()

	var totalFiles, totalFail, totalRemediated, totalFullyFixed int
	var totalViolationsBefore, totalViolationsAfter int

	for _, pi := range profiles {
		profileDir := filepath.Join(corpusRoot, pi.dir)
		if _, err := os.Stat(profileDir); os.IsNotExist(err) {
			continue
		}

		var failFiles []string
		err := filepath.Walk(profileDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && strings.HasSuffix(path, ".pdf") && strings.Contains(info.Name(), "fail") {
				failFiles = append(failFiles, path)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "walking %s: %v\n", profileDir, err)
			continue
		}

		profileRemediated := 0
		profileFullyFixed := 0
		profileViolBefore := 0
		profileViolAfter := 0

		for _, path := range failFiles {
			totalFiles++

			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			// Open and validate.
			r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				continue
			}

			violations, err := r.ValidatePDFA(ctx, pi.profile)
			if err != nil {
				continue
			}
			if len(violations) == 0 {
				continue // already compliant
			}
			totalFail++
			before := len(violations)
			profileViolBefore += before

			// Remediate.
			w := pdf.NewWriter(r)
			var remOpts []pdf.RemediateOption
			if aggressive {
				remOpts = append(remOpts, pdf.WithAggressiveFixes())
			}
			report, err := w.RemediatePDFA(ctx, pi.profile, remOpts...)
			if err != nil {
				continue
			}

			if len(report.Fixed) == 0 {
				profileViolAfter += before
				continue
			}

			// Write.
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				profileViolAfter += before
				continue
			}

			// Re-validate.
			r2, err := pdf.Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				profileViolAfter += before
				continue
			}

			after, err := r2.ValidatePDFA(ctx, pi.profile)
			if err != nil {
				profileViolAfter += before
				continue
			}

			profileViolAfter += len(after)
			profileRemediated++
			if len(after) == 0 {
				profileFullyFixed++
			}
			if len(after) > before {
				rel, _ := filepath.Rel(corpusRoot, path)
				fmt.Printf("  REGRESSION %s: %d → %d violations\n", rel, before, len(after))
				for _, v := range after {
					if isNew(v, violations) {
						fmt.Printf("    NEW: %v\n", v)
					}
				}
			}
		}

		totalRemediated += profileRemediated
		totalFullyFixed += profileFullyFixed
		totalViolationsBefore += profileViolBefore
		totalViolationsAfter += profileViolAfter

		fmt.Printf("%-12s  files=%3d  fail=%3d  remediated=%3d  fully_fixed=%3d  violations: %d → %d\n",
			pi.profile, len(failFiles), len(failFiles), profileRemediated, profileFullyFixed,
			profileViolBefore, profileViolAfter)
	}

	fmt.Println()
	fmt.Printf("Total:  files=%d  failing=%d  remediated=%d  fully_fixed=%d\n",
		totalFiles, totalFail, totalRemediated, totalFullyFixed)
	fmt.Printf("Violations:  before=%d  after=%d  fixed=%d (%.1f%%)\n",
		totalViolationsBefore, totalViolationsAfter,
		totalViolationsBefore-totalViolationsAfter,
		float64(totalViolationsBefore-totalViolationsAfter)/float64(max(totalViolationsBefore, 1))*100)
	fmt.Printf("Elapsed: %s\n", time.Since(start).Round(time.Millisecond))

	// Detailed breakdown requested via --detail flag.
	detail := false
	for _, arg := range os.Args[1:] {
		if arg == "--detail" {
			detail = true
		}
	}
	if detail {
		printResidualBreakdown(ctx, corpusRoot, profiles)
	}
}

func printResidualBreakdown(ctx context.Context, corpusRoot string, profiles []profileInfo) {
	fmt.Println("\n=== Residual Violation Breakdown ===")

	// Collect all residual violations across all profiles after remediation.
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

		var failFiles []string
		_ = filepath.Walk(profileDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && strings.HasSuffix(path, ".pdf") && strings.Contains(info.Name(), "fail") {
				failFiles = append(failFiles, path)
			}
			return nil
		})

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
			report, err := w.RemediatePDFA(ctx, pi.profile)
			if err != nil || len(report.Residual) == 0 {
				continue
			}

			for _, v := range report.Residual {
				// Extract clause from the error string.
				errStr := v.Error()
				clause := "unknown"
				if _, after, ok := strings.Cut(errStr, "clause "); ok {
					rest := after
					if before, _, ok := strings.Cut(rest, " "); ok {
						clause = before
					}
				}
				desc := "unknown"
				if _, after, ok := strings.Cut(errStr, ": "); ok {
					rest := after
					if before, _, ok := strings.Cut(rest, " ["); ok {
						desc = before
					} else {
						desc = rest
					}
				}

				key := clause
				ci, ok := clauseMap[key]
				if !ok {
					ci = &clauseInfo{clause: clause, description: desc, profiles: make(map[string]bool)}
					clauseMap[key] = ci
				}
				ci.count++
				ci.profiles[string(pi.profile)] = true
				if len(ci.examples) < 2 {
					rel, _ := filepath.Rel(corpusRoot, path)
					ci.examples = append(ci.examples, rel)
				}
			}
		}
	}

	// Sort by count descending.
	type sortEntry struct {
		key string
		ci  *clauseInfo
	}
	entries := make([]sortEntry, 0, len(clauseMap))
	for k, ci := range clauseMap {
		entries = append(entries, sortEntry{k, ci})
	}
	// Sort by count desc.
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].ci.count > entries[i].ci.count {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	for _, e := range entries {
		profs := make([]string, 0, len(e.ci.profiles))
		for p := range e.ci.profiles {
			profs = append(profs, p)
		}
		fmt.Printf("\n[%s] count=%d profiles=%v\n", e.ci.clause, e.ci.count, profs)
		fmt.Printf("  %s\n", e.ci.description)
		for _, ex := range e.ci.examples {
			fmt.Printf("  e.g. %s\n", ex)
		}
	}
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
