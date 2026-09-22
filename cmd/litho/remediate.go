package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// errBadFacturXInput signals that Factur-X remediation inputs were
// invalid. The helper has already written the cause to stderr.
var errBadFacturXInput = errors.New("invalid factur-x input")

// facturxVariant captures the embedded-file naming that distinguishes a
// ZUGFeRD generation from Factur-X. RemediateFacturX produces the
// archival base; the file name and AFRelationship select the flavour.
// An empty value uses the Factur-X / ZUGFeRD 2.1+ defaults.
type facturxVariant struct {
	fileName     string
	relationship string
}

var facturxVariants = map[string]facturxVariant{
	facturXProfileName: {},
	"ZUGFeRD-2.1":      {},
	"ZUGFeRD-2.0":      {fileName: "zugferd-invoice.xml", relationship: "Alternative"},
	"ZUGFeRD-1.0":      {fileName: "ZUGFeRD-invoice.xml", relationship: "Alternative"},
}

type remediateResult struct {
	Profile  string   `json:"profile"`
	Fixed    []string `json:"fixed"`
	Residual []string `json:"residual,omitempty"`
}

func runRemediate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("remediate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	profile := fs.String("profile", "", "Compliance profile (required, e.g., PDF/A-2b, PDF/UA-1, Factur-X)")
	aggressive := fs.Bool("aggressive", false, "Enable aggressive fixes that may alter visible content")
	format := fs.String("f", "text", "Output format: text or json")
	invoice := fs.String("invoice", "", "Cross-Industry Invoice XML to embed (Factur-X / ZUGFeRD profiles)")
	level := fs.String("level", "", "Factur-X conformance level: MINIMUM, BASIC WL, BASIC, EN 16931, EXTENDED, XRECHNUNG")
	compact := registerCompactFlag(fs)
	strict := fs.Bool("strict", false,
		"Exit 2 when violations remain that could not be fixed")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || *profile == "" {
		fmt.Fprintln(stderr, "Usage: litho remediate <file> --profile PROFILE [--aggressive] [--compact] [--strict] -o out.pdf")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Profiles: "+strings.Join(remediateProfileNames(), ", "))
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	w := pdf.NewWriter(r)

	var remOpts []pdf.RemediateOption
	if *aggressive {
		remOpts = append(remOpts, pdf.WithAggressiveFixes())
	}

	var report *pdf.RemediationReport

	if pdfaP, ok := pdfaProfiles[*profile]; ok {
		report, err = w.RemediatePDFA(ctx, pdfaP, remOpts...)
	} else if pdfuaP, ok := pdfuaProfiles[*profile]; ok {
		report, err = w.RemediatePDFUA(ctx, pdfuaP, remOpts...)
	} else if wtpdfP, ok := wtpdfProfiles[*profile]; ok {
		report, err = w.RemediateWTPDF(ctx, wtpdfP, remOpts...)
	} else if variant, ok := facturxVariants[*profile]; ok {
		report, err = remediateFacturX(ctx, w, variant, *invoice, *level, remOpts, stderr)
		if err == errBadFacturXInput {
			return exitError
		}
	} else {
		fmt.Fprintf(stderr, "unknown profile %q. Valid: %s\n", *profile, strings.Join(remediateProfileNames(), ", "))
		return exitError
	}

	if err != nil {
		fmt.Fprintf(stderr, "remediation: %v\n", err)
		return exitError
	}

	// Remediation forces a full rewrite for most documents but not all, so the
	// flag is accepted the way pipe accepts it: it applies when a rewrite
	// happens. PDF/A-1 is the exception that must be refused outright, since
	// packing would hand back a file that no longer conforms to the profile
	// this command was asked to produce.
	compactOK := *compact && !compactForbiddenByProfile(*profile)
	warnCompactProfileRefused(stderr, *compact, *profile)

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := w.WriteTo(ctx, dst, compactWriteOptions(compactOK)...); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	result := remediateResult{
		Profile: *profile,
		Fixed:   report.Fixed,
	}
	for _, e := range report.Residual {
		result.Residual = append(result.Residual, e.Error())
	}

	switch *format {
	case "json":
		// Not a direct return: the strict verdict below applies to both
		// formats, and returning here once skipped it for JSON callers.
		if code := jsonOut(stdout, stderr, result); code != exitOK {
			return code
		}
	default:
		fmt.Fprintf(stdout, "Profile:  %s\n", result.Profile)
		fmt.Fprintf(stdout, "Fixed:    %d\n", len(result.Fixed))
		for _, f := range result.Fixed {
			fmt.Fprintf(stdout, "  %s\n", f)
		}
		if len(result.Residual) > 0 {
			fmt.Fprintf(stdout, "Residual: %d\n", len(result.Residual))
			for _, r := range result.Residual {
				fmt.Fprintf(stdout, "  %s\n", r)
			}
		}
	}

	// Remediation writes the profile's identification metadata whatever else
	// remains (ISO 19005-1, 6.7.11 and ISO 14289-1, clause 5 make those
	// entries requirements in their own right), so a document with fatal
	// residuals still ASSERTS a conformance it does not have. Exiting 0 tells
	// a pipeline the file is ready, and only the report says otherwise.
	//
	// The status mirrors validate rather than inventing one: exitValidationFail
	// means the document did not conform, and exitError stays reserved for the
	// command itself failing. Off by default, so an existing caller reading the
	// report keeps the exit code it already gets.
	if *strict && len(fatalResiduals(report)) > 0 {
		return exitValidationFail
	}
	return exitOK
}

// fatalResiduals returns the residual violations that fail conformance.
//
// Severity is worth filtering on for one profile family only. The warning
// grade covers four Factur-X and CII rules, a collapsed Schematron bucket and
// three lenient-profile tolerances, while every PDF/A and PDF/UA rule is fatal
// under the binary conformance model. So this narrows the set only on the
// invoice profiles, which is exactly where a soft tolerance must not fail a
// build, and is an identity everywhere else.
func fatalResiduals(report *pdf.RemediationReport) []pdf.Violation {
	var out []pdf.Violation
	for _, v := range report.ResidualViolations() {
		if v.Severity == pdf.SeverityFatal {
			out = append(out, v)
		}
	}
	return out
}

// remediateFacturX embeds the supplied invoice XML and remediates the
// PDF/A-3 base. The variant tunes the embedded file name and
// AFRelationship to select Factur-X or a legacy ZUGFeRD generation.
// Input errors are reported to stderr and signalled by
// [errBadFacturXInput]; any other error is the remediation failure.
func remediateFacturX(ctx context.Context, w *pdf.Writer, variant facturxVariant, invoicePath, level string, opts []pdf.RemediateOption, stderr io.Writer) (*pdf.RemediationReport, error) {
	if invoicePath == "" || level == "" {
		fmt.Fprintln(stderr, `Usage: litho remediate <file> --profile Factur-X --invoice cii.xml --level "EN 16931" -o out.pdf`)
		return nil, errBadFacturXInput
	}

	lvl := models.FacturXLevel(level)
	if !lvl.IsValid() {
		fmt.Fprintf(stderr, "invalid Factur-X level %q\n", level)
		return nil, errBadFacturXInput
	}

	invoiceXML, err := os.ReadFile(invoicePath)
	if err != nil {
		fmt.Fprintf(stderr, "reading invoice XML: %v\n", err)
		return nil, errBadFacturXInput
	}

	cfg := pdf.FacturXConfig{InvoiceXML: invoiceXML, Level: lvl}
	if variant.fileName != "" {
		cfg.FileName = variant.fileName
	}
	if variant.relationship != "" {
		cfg.Relationship = variant.relationship
	}

	return w.RemediateFacturX(ctx, cfg, opts...)
}
