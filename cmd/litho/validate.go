package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
)

type violationInfo struct {
	Rule    string `json:"rule"`
	Clause  string `json:"clause"`
	Test    int    `json:"test"`
	Context string `json:"context"`
}

type validateResult struct {
	File       string          `json:"file"`
	Profile    string          `json:"profile"`
	Compliant  bool            `json:"compliant"`
	Violations []violationInfo `json:"violations,omitempty"`
}

var pdfaProfiles = map[string]models.PDFAProfile{
	"PDF/A-1a": models.PDFA1a,
	"PDF/A-1b": models.PDFA1b,
	"PDF/A-2a": models.PDFA2a,
	"PDF/A-2b": models.PDFA2b,
	"PDF/A-2u": models.PDFA2u,
	"PDF/A-3a": models.PDFA3a,
	"PDF/A-3b": models.PDFA3b,
	"PDF/A-3u": models.PDFA3u,
	"PDF/A-4":  models.PDFA4,
	"PDF/A-4e": models.PDFA4e,
	"PDF/A-4f": models.PDFA4f,
}

var pdfuaProfiles = map[string]models.PDFUAProfile{
	"PDF/UA-1": models.PDFUA1,
	"PDF/UA-2": models.PDFUA2,
}

func runValidate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	profile := fs.String("profile", "", "Compliance profile (e.g., PDF/A-2b, PDF/UA-1)")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 || *profile == "" {
		fmt.Fprintln(stderr, "Usage: litho validate --profile PROFILE [-f json] [-p password] <file> [file...]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Profiles: "+strings.Join(allProfileNames(), ", "))
		return exitError
	}

	var results []validateResult
	anyNonCompliant := false

	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		res := validateResult{File: path, Profile: *profile}

		if pdfaP, ok := pdfaProfiles[*profile]; ok {
			violations, err := r.ValidatePDFA(ctx, pdfaP)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			res.Compliant = len(violations) == 0
			res.Violations = formatViolations(violations)
		} else if pdfuaP, ok := pdfuaProfiles[*profile]; ok {
			violations, err := r.ValidatePDFUA(ctx, pdfuaP)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			res.Compliant = len(violations) == 0
			res.Violations = formatViolations(violations)
		} else {
			closer()
			fmt.Fprintf(stderr, "unknown profile %q. Valid: %s\n", *profile, strings.Join(allProfileNames(), ", "))
			return exitError
		}

		if !res.Compliant {
			anyNonCompliant = true
		}
		results = append(results, res)
		closer()
	}

	switch *format {
	case "json":
		if len(results) == 1 {
			if err := outputJSON(stdout, results[0]); err != nil {
				fmt.Fprintf(stderr, "encoding JSON: %v\n", err)
				return exitError
			}
		} else {
			if err := outputJSON(stdout, results); err != nil {
				fmt.Fprintf(stderr, "encoding JSON: %v\n", err)
				return exitError
			}
		}
	default:
		for _, res := range results {
			if len(results) > 1 {
				fmt.Fprintf(stdout, "--- %s ---\n", res.File)
			}
			if res.Compliant {
				fmt.Fprintf(stdout, "%s: compliant\n", res.Profile)
			} else {
				fmt.Fprintf(stdout, "%s: %d violation(s)\n", res.Profile, len(res.Violations))
				for _, v := range res.Violations {
					fmt.Fprintf(stdout, "  [%s] %s\n", v.Clause, v.Context)
				}
			}
		}
	}

	if anyNonCompliant {
		return exitValidationFail
	}
	return exitOK
}

func formatViolations(errs []error) []violationInfo {
	if len(errs) == 0 {
		return nil
	}
	var result []violationInfo
	for _, e := range errs {
		v := violationInfo{Context: e.Error()}
		// Parse structured fields if available.
		msg := e.Error()
		if _, after, ok := strings.Cut(msg, "clause "); ok {
			if clause, rest, ok := strings.Cut(after, " "); ok {
				v.Clause = clause
				v.Context = rest
			}
		}
		result = append(result, v)
	}
	return result
}

func allProfileNames() []string {
	names := make([]string, 0, len(pdfaProfiles)+len(pdfuaProfiles))
	for k := range pdfaProfiles {
		names = append(names, k)
	}
	for k := range pdfuaProfiles {
		names = append(names, k)
	}
	return names
}
