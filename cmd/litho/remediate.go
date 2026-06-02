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

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	if fs.NArg() < 1 || *output == "" || *profile == "" {
		fmt.Fprintln(stderr, "Usage: litho remediate <file> --profile PROFILE [--aggressive] -o out.pdf")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Profiles: "+strings.Join(allProfileNames(), ", "))
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
	} else if variant, ok := facturxVariants[*profile]; ok {
		report, err = remediateFacturX(ctx, w, variant, *invoice, *level, remOpts, stderr)
		if err == errBadFacturXInput {
			return exitError
		}
	} else {
		fmt.Fprintf(stderr, "unknown profile %q. Valid: %s\n", *profile, strings.Join(allProfileNames(), ", "))
		return exitError
	}

	if err != nil {
		fmt.Fprintf(stderr, "remediation: %v\n", err)
		return exitError
	}

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := w.WriteTo(ctx, dst); err != nil {
		closeDst() //nolint:errcheck
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
		return jsonOut(stdout, stderr, result)
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

	return exitOK
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
