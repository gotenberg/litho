package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

type violationInfo struct {
	Specification string `json:"specification,omitempty"`
	Clause        string `json:"clause"`
	Test          int    `json:"test"`
	Description   string `json:"description,omitempty"`
	Context       string `json:"context,omitempty"`
	Severity      string `json:"severity"`
}

type validateResult struct {
	File       string          `json:"file"`
	Profile    string          `json:"profile"`
	Compliant  bool            `json:"compliant"`
	Violations []violationInfo `json:"violations,omitempty"`
	// Advisories list Schematron rules the bundled engine could not
	// evaluate (Factur-X / ZUGFeRD only). They do not affect Compliant.
	Advisories []string `json:"advisories,omitempty"`
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

var wtpdfProfiles = map[string]models.WTPDFProfile{
	"WTPDF-1.0-Accessibility": models.WTPDF1Accessibility,
	"WTPDF-1.0-Reuse":         models.WTPDF1Reuse,
}

// facturXProfileName is the profile string selecting Factur-X / ZUGFeRD
// 2.1+ validation, which takes no version argument.
const facturXProfileName = "Factur-X"

var zugferdProfiles = map[string]models.ZUGFeRDVersion{
	"ZUGFeRD-1.0": models.ZUGFeRD1,
	"ZUGFeRD-2.0": models.ZUGFeRD2,
	"ZUGFeRD-2.1": models.ZUGFeRD21,
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
		fmt.Fprintln(stderr, "Profiles: "+strings.Join(validateProfileNames(), ", "))
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
			violations, err := r.ValidatePDFAReport(ctx, pdfaP)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			res.Compliant = len(violations) == 0
			res.Violations = structuredViolations(violations)
		} else if pdfuaP, ok := pdfuaProfiles[*profile]; ok {
			violations, err := r.ValidatePDFUAReport(ctx, pdfuaP)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			res.Compliant = len(violations) == 0
			res.Violations = structuredViolations(violations)
		} else if wtpdfP, ok := wtpdfProfiles[*profile]; ok {
			violations, err := r.ValidateWTPDFReport(ctx, wtpdfP)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			res.Compliant = len(violations) == 0
			res.Violations = structuredViolations(violations)
		} else if *profile == facturXProfileName {
			rep, err := r.ValidateFacturXReport(ctx)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			res.Compliant = len(rep.Fatals()) == 0
			res.Violations = structuredViolations(rep.Structured())
			res.Advisories = advisoryMessages(rep.Advisories)
		} else if ver, ok := zugferdProfiles[*profile]; ok {
			rep, err := r.ValidateZUGFeRDReport(ctx, ver)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			res.Compliant = len(rep.Fatals()) == 0
			res.Violations = structuredViolations(rep.Structured())
			res.Advisories = advisoryMessages(rep.Advisories)
		} else {
			closer()
			fmt.Fprintf(stderr, "unknown profile %q. Valid: %s\n", *profile, strings.Join(validateProfileNames(), ", "))
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
			switch {
			case res.Compliant && len(res.Violations) == 0:
				fmt.Fprintf(stdout, "%s: compliant\n", res.Profile)
			case res.Compliant:
				fmt.Fprintf(stdout, "%s: compliant, %d warning(s)\n", res.Profile, len(res.Violations))
			default:
				fmt.Fprintf(stdout, "%s: %d violation(s)\n", res.Profile, len(res.Violations))
			}
			for _, v := range res.Violations {
				fmt.Fprintf(stdout, "  [%s] clause %s test %d: %s", v.Severity, v.Clause, v.Test, v.Description)
				if v.Context != "" {
					fmt.Fprintf(stdout, " [%s]", v.Context)
				}
				fmt.Fprintln(stdout)
			}
			for _, a := range res.Advisories {
				fmt.Fprintf(stdout, "  advisory: %s\n", a)
			}
		}
	}

	if anyNonCompliant {
		return exitValidationFail
	}
	return exitOK
}

// structuredViolations maps the structured [pdf.Violation] values from
// the report API onto the CLI output shape, carrying the spec
// coordinates and severity directly instead of parsing them out of an
// error string.
func structuredViolations(vs []pdf.Violation) []violationInfo {
	if len(vs) == 0 {
		return nil
	}
	result := make([]violationInfo, 0, len(vs))
	for _, v := range vs {
		result = append(result, violationInfo{
			Specification: v.Specification,
			Clause:        v.Clause,
			Test:          v.TestNumber,
			Description:   v.Description,
			Context:       v.Context,
			Severity:      v.Severity.String(),
		})
	}
	return result
}

// advisoryMessages renders Schematron advisories as their error strings.
func advisoryMessages(errs []error) []string {
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	return msgs
}

// validateProfileNames lists every profile `litho validate` accepts.
// remediate accepts a different set, so it has its own list: advertising
// a profile a command then refuses sends the caller in circles.
func validateProfileNames() []string {
	names := make([]string, 0, len(pdfaProfiles)+len(pdfuaProfiles)+len(wtpdfProfiles)+1+len(zugferdProfiles))
	for k := range pdfaProfiles {
		names = append(names, k)
	}
	for k := range pdfuaProfiles {
		names = append(names, k)
	}
	for k := range wtpdfProfiles {
		names = append(names, k)
	}
	names = append(names, facturXProfileName)
	for k := range zugferdProfiles {
		names = append(names, k)
	}
	return names
}

// remediateProfileNames lists every profile `litho remediate` accepts.
// It is deliberately separate from [validateProfileNames]: advertising a
// profile a command then refuses sends the caller in circles.
func remediateProfileNames() []string {
	names := make([]string, 0, len(pdfaProfiles)+len(pdfuaProfiles)+len(wtpdfProfiles)+len(facturxVariants))
	for k := range pdfaProfiles {
		names = append(names, k)
	}
	for k := range pdfuaProfiles {
		names = append(names, k)
	}
	for k := range wtpdfProfiles {
		names = append(names, k)
	}
	for k := range facturxVariants {
		names = append(names, k)
	}
	return names
}
