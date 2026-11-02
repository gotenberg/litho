package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

type violationInfo struct {
	// Profile names the profile of a pair the violation belongs to, "" for
	// one profile.
	Profile       string `json:"profile,omitempty"`
	Specification string `json:"specification,omitempty"`
	Clause        string `json:"clause"`
	Test          int    `json:"test"`
	Description   string `json:"description,omitempty"`
	Context       string `json:"context,omitempty"`
	Severity      string `json:"severity"`
}

// validateResult is the verdict on one file. For a pair, Profile is a JSON
// array, Compliant holds when the file conforms to both profiles, and
// Violations lists those of each profile in the order of Profile, each naming
// its profile.
type validateResult struct {
	File       string          `json:"file"`
	Profile    profileNames    `json:"profile"`
	Compliant  bool            `json:"compliant"`
	Violations []violationInfo `json:"violations,omitempty"`
	// Advisories list Schematron rules the bundled engine could not
	// evaluate (Factur-X / ZUGFeRD only). They do not affect Compliant.
	Advisories []string `json:"advisories,omitempty"`
	// verdicts holds each profile's own verdict, in the order of Profile, for
	// the text report.
	verdicts []profileVerdict
}

// profileVerdict is the verdict of one profile on one file.
type profileVerdict struct {
	profile    string
	compliant  bool
	violations []violationInfo
	advisories []string
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
	var profiles profileFlags
	fs.Var(&profiles, "profile", "Compliance profile (e.g., PDF/A-2b, PDF/UA-1; repeatable, to pair a PDF/A, Factur-X "+
		"or ZUGFeRD profile with the PDF/UA profile of its PDF version, in either order, which the file must conform to both of)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	files := fs.Args()
	if len(files) == 0 || len(profiles) == 0 {
		fmt.Fprintln(stderr, "Usage: litho validate --profile PROFILE [--profile PDF/UA-N] [-f json] [-p password] <file> [file...]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Profiles: "+strings.Join(validateProfileNames(), ", "))
		fmt.Fprintln(stderr, "Repeat --profile for a pair: "+validPairs())
		return exitError
	}
	// The pairs are those remediate takes, so that validate can judge what a
	// paired remediation claims.
	req, err := parseProfiles(profiles)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
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

		res := validateResult{File: path, Profile: profileNames(req.names), Compliant: true}
		for _, profile := range req.names {
			verdict, known, err := validateProfile(ctx, r, profile)
			if !known {
				closer()
				fmt.Fprintf(stderr, "unknown profile %q. Valid: %s\n", profile, strings.Join(validateProfileNames(), ", "))
				return exitError
			}
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			if req.isPair() {
				for i := range verdict.violations {
					verdict.violations[i].Profile = profile
				}
			}
			res.Compliant = res.Compliant && verdict.compliant
			res.Violations = append(res.Violations, verdict.violations...)
			res.Advisories = append(res.Advisories, verdict.advisories...)
			res.verdicts = append(res.verdicts, verdict)
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
			// A pair prints one verdict per profile, each as one profile
			// prints its own.
			for _, verdict := range res.verdicts {
				writeVerdict(stdout, verdict)
			}
		}
	}

	if anyNonCompliant {
		return exitValidationFail
	}
	return exitOK
}

// validateProfile judges the document r reads against profile. known is
// false, and nothing is judged, for a profile [validateProfileNames] does not
// list.
//
// A Factur-X or ZUGFeRD report holds warnings too, soft findings that do not
// fail conformance on their own, so its verdict counts the fatal violations
// alone. Every PDF/A, PDF/UA and WTPDF violation is fatal.
func validateProfile(ctx context.Context, r *pdf.Reader, profile string) (verdict profileVerdict, known bool, err error) {
	verdict.profile = profile
	var violations []pdf.Violation
	switch {
	case pdfaProfiles[profile] != "":
		violations, err = r.ValidatePDFAReport(ctx, pdfaProfiles[profile])
	case pdfuaProfiles[profile] != "":
		violations, err = r.ValidatePDFUAReport(ctx, pdfuaProfiles[profile])
	case wtpdfProfiles[profile] != "":
		violations, err = r.ValidateWTPDFReport(ctx, wtpdfProfiles[profile])
	case profile == facturXProfileName || zugferdProfiles[profile] != "":
		var rep pdf.FacturXReport
		if profile == facturXProfileName {
			rep, err = r.ValidateFacturXReport(ctx)
		} else {
			rep, err = r.ValidateZUGFeRDReport(ctx, zugferdProfiles[profile])
		}
		if err != nil {
			return verdict, true, err
		}
		verdict.compliant = len(rep.Fatals()) == 0
		verdict.violations = structuredViolations(rep.Structured())
		verdict.advisories = advisoryMessages(rep.Advisories)
		return verdict, true, nil
	default:
		return verdict, false, nil
	}
	if err != nil {
		return verdict, true, err
	}
	verdict.compliant = len(violations) == 0
	verdict.violations = structuredViolations(violations)
	return verdict, true, nil
}

// writeVerdict prints the text report of one profile's verdict.
func writeVerdict(w io.Writer, verdict profileVerdict) {
	switch {
	case verdict.compliant && len(verdict.violations) == 0:
		fmt.Fprintf(w, "%s: compliant\n", verdict.profile)
	case verdict.compliant:
		fmt.Fprintf(w, "%s: compliant, %d warning(s)\n", verdict.profile, len(verdict.violations))
	default:
		fmt.Fprintf(w, "%s: %d violation(s)\n", verdict.profile, len(verdict.violations))
	}
	for _, v := range verdict.violations {
		fmt.Fprintf(w, "  [%s] clause %s test %d: %s", v.Severity, v.Clause, v.Test, v.Description)
		if v.Context != "" {
			fmt.Fprintf(w, " [%s]", v.Context)
		}
		fmt.Fprintln(w)
	}
	for _, a := range verdict.advisories {
		fmt.Fprintf(w, "  advisory: %s\n", a)
	}
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
	names = append(names, sortedKeys(pdfaProfiles)...)
	names = append(names, sortedKeys(pdfuaProfiles)...)
	names = append(names, sortedKeys(wtpdfProfiles)...)
	names = append(names, facturXProfileName)
	names = append(names, sortedKeys(zugferdProfiles)...)
	return names
}

// remediateProfileNames lists every profile `litho remediate` accepts.
// It is deliberately separate from [validateProfileNames]: advertising a
// profile a command then refuses sends the caller in circles.
func remediateProfileNames() []string {
	names := make([]string, 0, len(pdfaProfiles)+len(pdfuaProfiles)+len(wtpdfProfiles)+len(facturxVariants))
	names = append(names, sortedKeys(pdfaProfiles)...)
	names = append(names, sortedKeys(pdfuaProfiles)...)
	names = append(names, sortedKeys(wtpdfProfiles)...)
	names = append(names, sortedKeys(facturxVariants)...)
	return names
}

// sortedKeys returns a profile table's names in ascending order. The usage
// and error messages print them, and ranging the map printed a different
// order on every run (CONTRIBUTING "Deterministic output").
func sortedKeys[V any](table map[string]V) []string {
	return slices.Sorted(maps.Keys(table))
}
