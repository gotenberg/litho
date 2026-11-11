package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// convertTargetUsage is the part of the litho convert synopsis that names
// the output: its version, its profiles and how a residual exits.
const convertTargetUsage = "[--pdf-version 1.4|1.5|1.6|1.7|2.0] [--profile PDF/A-Nx]... [--profile PDF/UA-N] [--strict] [--temp-dir DIR] [--lang TAG]"

// convertTargetFlags are the flags of litho convert that name the file it
// writes: its version, the profiles it conforms to, where the intermediate
// files go, the default language, and whether a residual fails the command.
type convertTargetFlags struct {
	profiles profileFlags
	version  *string
	tempDir  *string
	lang     *string
	strict   *bool
	// req is the profile list [convertTargetFlags.options] parsed.
	req profileRequest
}

// convertConformanceJSON is the conformance block of the litho convert JSON
// report: the profiles asked for, what the remediation fixed, and what the
// profiles still find, each entry of a pair prefixed with its profile as
// litho remediate prefixes it.
type convertConformanceJSON struct {
	Profile  profileNames `json:"profile"`
	Fixed    []string     `json:"fixed"`
	Residual []string     `json:"residual"`
}

// convertTargetResult is the litho convert JSON report with the
// conformance block, present when a profile was asked for.
type convertTargetResult struct {
	convertResult
	Conformance *convertConformanceJSON `json:"conformance,omitempty"`
}

// registerConvertTargetFlags registers the output flags of litho convert on
// fs.
func registerConvertTargetFlags(fs *flag.FlagSet) *convertTargetFlags {
	f := &convertTargetFlags{}
	fs.Var(&f.profiles, "profile", "Profile the output conforms to: a PDF/A profile (PDF/A-1a to PDF/A-4e) or a PDF/UA one (PDF/UA-1, PDF/UA-2); "+
		"repeatable, to pair a PDF/A profile with the PDF/UA profile of its PDF version")
	f.version = fs.String("pdf-version", "", "PDF version of the output: 1.4, 1.5, 1.6, 1.7 or 2.0 (default 1.7, or the version the profile builds on)")
	f.strict = fs.Bool("strict", false, "Exit 2 when the output does not conform to a --profile; the file is written either way")
	f.tempDir = fs.String("temp-dir", "", "Directory of the intermediate files of a --profile conversion (default the system's)")
	f.lang = fs.String("lang", "", "Language of a document that names none, as a BCP 47 tag such as en-US")
	return f
}

// errConvertProfile reports a --profile litho convert cannot write.
var errConvertProfile = errors.New("--profile")

// options returns the [pdf.ConvertOption] values the flags ask for, or an
// error naming a --profile list litho convert cannot write: one that does
// not pair, or a Factur-X, ZUGFeRD or WTPDF profile. The library judges the
// version against the profiles, and refuses PDF/A-4f.
func (f *convertTargetFlags) options() ([]pdf.ConvertOption, error) {
	if *f.tempDir != "" {
		if info, err := os.Stat(*f.tempDir); err != nil {
			return nil, fmt.Errorf("--temp-dir: %w", err)
		} else if !info.IsDir() {
			return nil, fmt.Errorf("--temp-dir: %s is not a directory", *f.tempDir)
		}
	}
	opts := []pdf.ConvertOption{pdf.WithConvertVersion(*f.version), pdf.WithConvertTempDir(*f.tempDir), pdf.WithDefaultLanguage(*f.lang)}
	if len(f.profiles) == 0 {
		return opts, nil
	}
	req, err := parseProfiles(f.profiles)
	if err != nil {
		return nil, err
	}
	f.req = req
	if _, ok := facturxVariants[req.target]; ok {
		return nil, fmt.Errorf("%w %s: the profile requires an embedded invoice, which a conversion does not have; "+
			"convert to PDF/A-3b, then run litho remediate --profile %s --invoice", errConvertProfile, req.target, req.target)
	}
	switch {
	case pdfaProfiles[req.target] != "":
		opts = append(opts, pdf.WithConvertPDFA(pdfaProfiles[req.target]), pdf.WithConvertPDFUA(models.PDFUAProfile(req.pdfua)))
	case pdfuaProfiles[req.target] != "":
		opts = append(opts, pdf.WithConvertPDFUA(pdfuaProfiles[req.target]))
	default:
		return nil, fmt.Errorf("%w %s: litho convert writes the PDF/A and PDF/UA profiles; convert, then run litho remediate --profile %s",
			errConvertProfile, req.target, req.target)
	}
	return opts, nil
}

// conformance returns the conformance block of report, nil when no profile
// was asked for.
func (f *convertTargetFlags) conformance(report *pdf.ConvertReport) *convertConformanceJSON {
	if report.Conformance == nil {
		return nil
	}
	out := &convertConformanceJSON{Profile: profileNames(f.req.names), Fixed: report.Conformance.Fixed, Residual: residualMessages(report.Conformance, f.req)}
	if out.Fixed == nil {
		out.Fixed = []string{}
	}
	if out.Residual == nil {
		out.Residual = []string{}
	}
	return out
}

// reportJSON writes res with the conformance block of report to stdout,
// and returns the exit code: [exitValidationFail] under --strict when the
// profiles still find a fatal violation, as litho remediate --strict does.
func (f *convertTargetFlags) reportJSON(stdout, stderr io.Writer, res convertResult, report *pdf.ConvertReport) int {
	if code := jsonOut(stdout, stderr, convertTargetResult{convertResult: res, Conformance: f.conformance(report)}); code != exitOK {
		return code
	}
	return f.exitCode(report)
}

// reportText writes what the profiles still find in the output to stderr,
// one line each, and returns the exit code as [convertTargetFlags.reportJSON]
// does.
func (f *convertTargetFlags) reportText(stderr io.Writer, report *pdf.ConvertReport) int {
	if c := f.conformance(report); c != nil {
		for _, r := range c.Residual {
			fmt.Fprintf(stderr, "residual: %s\n", r)
		}
	}
	return f.exitCode(report)
}

// exitCode returns [exitValidationFail] under --strict when the profiles
// still find a fatal violation in the output, [exitOK] otherwise. The file
// is written and claims the profiles either way (see [pdf.WithConvertPDFA]):
// the status is what tells a pipeline it does not conform.
func (f *convertTargetFlags) exitCode(report *pdf.ConvertReport) int {
	if *f.strict && report.Conformance != nil && len(fatalResiduals(report.Conformance)) > 0 {
		return exitValidationFail
	}
	return exitOK
}
