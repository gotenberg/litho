package main

import (
	"context"
	"encoding/hex"
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

// remediateResult is the report of remediate. A pair's Profile is a JSON
// array, and each of its Residual entries starts with the profile it belongs
// to (see [residualMessages]).
type remediateResult struct {
	Profile      profileNames `json:"profile"`
	Fixed        []string     `json:"fixed"`
	Residual     []string     `json:"residual,omitempty"`
	RefusedFonts []string     `json:"refused_fonts,omitempty"`
}

func runRemediate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("remediate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	var profiles profileFlags
	fs.Var(&profiles, "profile", "Compliance profile (required, e.g., PDF/A-2b, PDF/UA-1, Factur-X; repeatable, "+
		"to pair a PDF/A, Factur-X or ZUGFeRD profile with the PDF/UA profile of its PDF version, in either order)")
	aggressive := fs.Bool("aggressive", false, "Enable aggressive fixes that may alter visible content")
	format := fs.String("f", "text", "Output format: text or json")
	invoice := fs.String("invoice", "", "Cross-Industry Invoice XML to embed (Factur-X / ZUGFeRD profiles)")
	level := fs.String("level", "", "Factur-X conformance level: MINIMUM, BASIC WL, BASIC, EN 16931, EXTENDED, XRECHNUNG")
	compactFlags := registerCompactFlags(fs)
	fonts := registerFontFlags(fs)
	documentID := registerDocumentIDFlag(fs)
	strict := fs.Bool("strict", false,
		"Exit 2 when violations remain that could not be fixed")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || len(profiles) == 0 {
		fmt.Fprintln(stderr, "Usage: litho remediate <file> --profile PROFILE [--profile PDF/UA-N] [--aggressive] [--font-dir DIR] [--system-fonts] [--no-font-subset] [--document-id HEX] [--compact] [--recompress-images] [--subset-fonts] [--strict] -o out.pdf")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Profiles: "+strings.Join(remediateProfileNames(), ", "))
		fmt.Fprintln(stderr, "Repeat --profile for a pair: "+validPairs())
		return exitError
	}

	// A pair that cannot exist fails here, before the document opens, as a
	// usage error: the library would refuse it with
	// pdf.ErrIncompatibleProfiles only once the remediation runs.
	req, err := parseProfiles(profiles)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	// The PDF/A side of a pair, or the lone profile, decides the method, the
	// encryption warning and what --compact may do: no part of ISO 14289
	// forbids encryption or packing, so the PDF/UA side of a pair adds no
	// restriction to them.
	profile := req.target

	fontOpts, err := fonts.options()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	idOpt, err := documentIDOption(*documentID)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	// This command has no encryption flag, so it can never be encrypting. A
	// PDF/A output, Factur-X's PDF/A-3 included, cannot take encryption back.
	if _, facturX := facturxVariants[profile]; pdfaProfiles[profile] != "" || facturX {
		warnEncryptionDroppedByPDFA(stderr, false, r)
	} else {
		warnEncryptionDropped(stderr, r)
	}

	w := pdf.NewWriter(r, defaultPageSize.writeOptions()...)
	defer releaseWriter(w, stderr)

	var remOpts []pdf.RemediateOption
	if *aggressive {
		remOpts = append(remOpts, pdf.WithAggressiveFixes())
	}
	remOpts = append(remOpts, fontOpts...)
	if idOpt != nil {
		remOpts = append(remOpts, idOpt)
	}
	// parseProfiles pairs PDF/UA with a PDF/A, Factur-X or ZUGFeRD target
	// only, the targets whose methods take the option.
	if req.isPair() {
		remOpts = append(remOpts, pdf.WithPDFUA(pdfuaProfiles[req.pdfua]))
	}

	var report *pdf.RemediationReport

	if pdfaP, ok := pdfaProfiles[profile]; ok {
		report, err = w.RemediatePDFA(ctx, pdfaP, remOpts...)
	} else if pdfuaP, ok := pdfuaProfiles[profile]; ok {
		report, err = w.RemediatePDFUA(ctx, pdfuaP, remOpts...)
	} else if wtpdfP, ok := wtpdfProfiles[profile]; ok {
		report, err = w.RemediateWTPDF(ctx, wtpdfP, remOpts...)
	} else if variant, ok := facturxVariants[profile]; ok {
		report, err = remediateFacturX(ctx, w, variant, *invoice, *level, remOpts, stderr)
		if err == errBadFacturXInput {
			return exitError
		}
	} else {
		fmt.Fprintf(stderr, "unknown profile %q. Valid: %s\n", profile, strings.Join(remediateProfileNames(), ", "))
		return exitError
	}

	if err != nil {
		fmt.Fprintf(stderr, "remediation: %v\n", err)
		return exitError
	}

	// Remediation forces a full rewrite for most documents but not all, so the
	// flag is accepted the way pipe accepts it: it applies when a rewrite
	// happens. PDF/A-1 keeps dedup and withholds packing, since packing would
	// hand back a file that no longer conforms to the profile this command was
	// asked to produce. A pair packs as its PDF/A side does.
	warnPackingWithheld(stderr, *compactFlags.compact, profile)

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	var compactionSkipped bool
	writeOpts := append(profileCompactWriteOptions(compactFlags.get(), profile), pdf.WithCompactionSkipped(&compactionSkipped))
	if err := w.WriteTo(ctx, dst, writeOpts...); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}
	warnCompactIncremental(stderr, compactFlags.get(), compactionSkipped, profile)

	result := remediateResult{
		Profile:  profileNames(req.names),
		Fixed:    report.Fixed,
		Residual: residualMessages(report, req),
	}
	for _, refusal := range report.FontRefusals {
		result.RefusedFonts = append(result.RefusedFonts, refusal.String())
	}

	// -o - writes the document to stdout, so the report goes to stderr rather
	// than landing inside the bytes the caller is capturing. See reportWriter.
	reportOut := reportWriter(*output, stdout, stderr)

	switch *format {
	case "json":
		// Not a direct return: the strict verdict below applies to both
		// formats, and returning here once skipped it for JSON callers.
		if code := jsonOut(reportOut, stderr, result); code != exitOK {
			return code
		}
	default:
		if req.isPair() {
			fmt.Fprintf(reportOut, "Profiles: %s\n", strings.Join(result.Profile, ", "))
		} else {
			fmt.Fprintf(reportOut, "Profile:  %s\n", profile)
		}
		fmt.Fprintf(reportOut, "Fixed:    %d\n", len(result.Fixed))
		for _, f := range result.Fixed {
			fmt.Fprintf(reportOut, "  %s\n", f)
		}
		if len(result.Residual) > 0 {
			fmt.Fprintf(reportOut, "Residual: %d\n", len(result.Residual))
			for _, r := range result.Residual {
				fmt.Fprintf(reportOut, "  %s\n", r)
			}
		}
		if len(result.RefusedFonts) > 0 {
			fmt.Fprintf(reportOut, "Fonts left unembedded: %d\n", len(result.RefusedFonts))
			for _, r := range result.RefusedFonts {
				fmt.Fprintf(reportOut, "  %s\n", r)
			}
		}
	}

	// Remediation writes the profile's identification metadata whatever else
	// remains (ISO 19005-1, 6.7.11 and ISO 14289-1, clause 5 make those
	// entries requirements in their own right), so a document with fatal
	// residuals still ASSERTS a conformance it does not have. Exiting 0 tells
	// a pipeline the file is ready, and only the report says otherwise. A
	// pair's residual holds the violations of both profiles, so either one
	// failing fails the verdict.
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

// residualMessages renders the residual violations of report, a remediation
// to req. One profile keeps the bare messages. A pair prefixes each with the
// profile it belongs to (see [profileRequest.profileOf]) and groups them in
// the order the caller named the profiles: a clause number alone does not
// say which standard it is from.
func residualMessages(report *pdf.RemediationReport, req profileRequest) []string {
	var out []string
	if !req.isPair() {
		for _, e := range report.Residual {
			out = append(out, e.Error())
		}
		return out
	}
	// ResidualViolations maps Residual one for one, in order. Its messages
	// differ from Residual's for an error that is no compliance violation,
	// so the text stays Residual's and the violation lends its standard.
	violations := report.ResidualViolations()
	for _, profile := range req.names {
		for i, e := range report.Residual {
			if req.profileOf(violations[i].Specification) == profile {
				out = append(out, profile+": "+e.Error())
			}
		}
	}
	return out
}

// fatalResiduals returns the residual violations that fail conformance.
//
// Severity is worth filtering on for one profile family only. The warning
// grade covers four Factur-X and CII rules, a collapsed Schematron bucket and
// three lenient-profile tolerances, while every PDF/A, PDF/UA and WTPDF rule is
// fatal under the binary conformance model. So this narrows the set only on the
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
		fmt.Fprintln(stderr, `Usage: litho remediate <file> --profile Factur-X [--profile PDF/UA-1] --invoice cii.xml --level "EN 16931" -o out.pdf`)
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

// fontFlags are the --font-dir, --system-fonts and --no-font-subset flags
// remediate and the pipe's remediate stage share.
type fontFlags struct {
	dir      *string
	system   *bool
	noSubset *bool
}

// registerFontFlags declares the font flags on fs.
func registerFontFlags(fs *flag.FlagSet) fontFlags {
	return fontFlags{
		dir: fs.String("font-dir", "",
			"Directory of TrueType, OpenType or CFF font files, searched recursively, to embed for the fonts the document does not embed: a font takes the file of its name, its family, or a metric-compatible family (Arial for Helvetica), subset to the glyphs it shows unless --no-font-subset; the report lists each font refused and why (the licence to embed them is yours)"),
		system: fs.Bool("system-fonts", false,
			"Also search the operating system's font directories, after --font-dir, for the fonts the document does not embed, matched as --font-dir matches them; the output then depends on the fonts installed on this host"),
		noSubset: fs.Bool("no-font-subset", false,
			"Embed the --font-dir or --system-fonts programs whole instead of subset to the glyphs the document shows, so later edits can show other characters"),
	}
}

// options returns the remediation options the font flags ask for: none when
// --font-dir is empty and --system-fonts unset, or an error when --font-dir
// names no readable directory or --no-font-subset comes without either, since
// there is then no program to keep whole.
func (f fontFlags) options() ([]pdf.RemediateOption, error) {
	if *f.dir == "" && !*f.system {
		if *f.noSubset {
			return nil, errors.New("--no-font-subset needs --font-dir or --system-fonts")
		}
		return nil, nil
	}
	var opts []pdf.RemediateOption
	if *f.dir != "" {
		opt, err := fontDirOption(*f.dir)
		if err != nil {
			return nil, err
		}
		opts = append(opts, opt)
	}
	if *f.system {
		opts = append(opts, pdf.WithSystemFonts())
	}
	if *f.noSubset {
		opts = append(opts, pdf.WithWholeFontPrograms())
	}
	return opts, nil
}

// fontDirOption returns the remediation option embedding the font files of
// dir, or an error when dir is no readable directory.
func fontDirOption(dir string) (pdf.RemediateOption, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("--font-dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("--font-dir: %s is not a directory", dir)
	}
	return pdf.WithFontFiles(os.DirFS(dir)), nil
}

// documentIDBytes is the length of the /ID seed [pdf.WithDocumentID] takes.
const documentIDBytes = 16

// registerDocumentIDFlag declares the --document-id flag remediate and the
// pipe's remediate stage share.
func registerDocumentIDFlag(fs *flag.FlagSet) *string {
	return fs.String("document-id", "",
		"32 hex digits pinning the trailer /ID remediation writes when the document has no valid one (default: derived from the file and the options)")
}

// documentIDOption returns the remediation option pinning the trailer /ID to
// the 16 bytes hexID spells, nil when hexID is empty, or an error when hexID is
// not 32 hex digits. Remediation writes an /ID when the document has no valid
// one, derived from the file and the options unless pinned, so two runs of
// this command over the same file write the same bytes either way.
func documentIDOption(hexID string) (pdf.RemediateOption, error) {
	if hexID == "" {
		return nil, nil
	}
	id, err := hex.DecodeString(hexID)
	if err != nil {
		return nil, fmt.Errorf("--document-id: %w", err)
	}
	if len(id) != documentIDBytes {
		return nil, fmt.Errorf("--document-id: want %d hex digits, got %d", 2*documentIDBytes, len(hexID))
	}
	return pdf.WithDocumentID(id), nil
}
