package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

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
	profile := fs.String("profile", "", "Compliance profile (required, e.g., PDF/A-2b, PDF/UA-1)")
	aggressive := fs.Bool("aggressive", false, "Enable aggressive fixes that may alter visible content")
	format := fs.String("f", "text", "Output format: text or json")

	if err := fs.Parse(args); err != nil {
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
