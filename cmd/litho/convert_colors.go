package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// convertColorsResult is the JSON report of convert-colors.
type convertColorsResult struct {
	File                 string `json:"file"`
	Target               string `json:"target"`
	ConvertedOperators   int    `json:"convertedOperators"`
	ConvertedColorSpaces int    `json:"convertedColorSpaces"`
	ConvertedImages      int    `json:"convertedImages"`
	ConvertedShadings    int    `json:"convertedShadings"`
	// KOnlyBlackColors counts the colours --k-only-black wrote on the black
	// ink alone ([pdf.ColorReport.KOnlyBlackColors]).
	KOnlyBlackColors  int      `json:"kOnlyBlackColors,omitempty"`
	KeptSpotColorants []string `json:"keptSpotColorants,omitempty"`
	OutputIntent      string   `json:"outputIntent,omitempty"`
	KeptOutputIntents []string `json:"keptOutputIntents,omitempty"`
	// ReplacedOutputIntents names the intents --replace-output-intents
	// pointed at the target profile, by /OutputConditionIdentifier.
	ReplacedOutputIntents []string `json:"replacedOutputIntents,omitempty"`
	// TargetProfile, SourceProfiles and IntentFallbacks say which profiles
	// the conversion read and where a rendering intent fell back (see
	// [pdf.ColorReport]).
	TargetProfile   string              `json:"targetProfile"`
	SourceProfiles  map[string][]string `json:"sourceProfiles,omitempty"`
	IntentFallbacks []intentFallback    `json:"intentFallbacks,omitempty"`
	// OverprintPreserved, OverprintStates and OverprintChanged are
	// [pdf.ColorReport]'s counts of the paintings under overprint.
	OverprintPreserved int `json:"overprintPreserved,omitempty"`
	OverprintStates    int `json:"overprintStates,omitempty"`
	OverprintChanged   int `json:"overprintChanged,omitempty"`
	// BlackPointOperators, BlackPointInlineImages, BlackPointCopies and
	// BlackPointUnhonoured are [pdf.ColorReport]'s counts of what the
	// graphics states' /UseBlackPtComp changed.
	BlackPointOperators    int `json:"blackPointOperators,omitempty"`
	BlackPointInlineImages int `json:"blackPointInlineImages,omitempty"`
	BlackPointCopies       int `json:"blackPointCopies,omitempty"`
	BlackPointUnhonoured   int `json:"blackPointUnhonoured,omitempty"`
	// IntentOperators, IntentInlineImages, IntentCopies and
	// IntentUnhonoured are [pdf.ColorReport]'s counts of what the rendering
	// intents the content states changed.
	IntentOperators    int `json:"intentOperators,omitempty"`
	IntentInlineImages int `json:"intentInlineImages,omitempty"`
	IntentCopies       int `json:"intentCopies,omitempty"`
	IntentUnhonoured   int `json:"intentUnhonoured,omitempty"`
}

// intentFallback is one [pdf.IntentFallback] in the JSON report.
type intentFallback struct {
	Target     bool   `json:"target"`
	Components int    `json:"components"`
	Profile    string `json:"profile"`
	Table      string `json:"table"`
}

// renderingIntents maps the --intent values, the rendering intent names of
// ISO 32000-1, 8.6.5.8, Table 70, onto the library's.
//
//nolint:gochecknoglobals // static spec table, ISO 32000-1 Table 70
var renderingIntents = map[string]pdf.RenderingIntent{
	"RelativeColorimetric": pdf.IntentRelativeColorimetric,
	"Perceptual":           pdf.IntentPerceptual,
	"Saturation":           pdf.IntentSaturation,
	"AbsoluteColorimetric": pdf.IntentAbsoluteColorimetric,
}

// convertColorsFlags holds the flags that choose the conversion.
type convertColorsFlags struct {
	target, profile, intent string
	// noBPC is --no-bpc, which converts without black point compensation.
	noBPC, convertSpots  bool
	kOnlyBlack           bool
	noOutputIntent       bool
	replaceOutputIntents bool
	// jpegQuality is --jpeg-quality, 0 to keep every image lossless.
	jpegQuality int
	// cmykProfile is --cmyk-profile, the file the Reader is opened with as
	// its default CMYK profile.
	cmykProfile string
}

// registerConvertColorsFlags declares on fs the flags that choose the
// conversion, which the command and its pipe stage share so their names and
// defaults cannot drift apart.
func registerConvertColorsFlags(fs *flag.FlagSet) *convertColorsFlags {
	var f convertColorsFlags
	fs.StringVar(&f.target, "target", "output-intent", "Colour space to convert to: output-intent, cmyk, rgb, gray or icc")
	fs.StringVar(&f.profile, "profile", "", "ICC profile of --target icc")
	fs.StringVar(&f.cmykProfile, "cmyk-profile", "", "ICC profile of --target cmyk, and the one DeviceCMYK is read in when the document names none (default: built-in SWOP profile)")
	fs.StringVar(&f.intent, "intent", "RelativeColorimetric", "Rendering intent where the content states none (an ri operator, an ExtGState /RI or an image /Intent wins): RelativeColorimetric, Perceptual, Saturation or AbsoluteColorimetric")
	fs.BoolVar(&f.noBPC, "no-bpc", false, "Convert without black point compensation where no graphics state's /UseBlackPtComp chooses")
	// Compensation is the default now; the flag stays so scripts that pass
	// it keep working.
	fs.Bool("bpc", true, "Deprecated: black point compensation is the default (see --no-bpc)")
	fs.BoolVar(&f.convertSpots, "convert-spots", false, "Convert spot colours (Separation, DeviceN) to process colour instead of keeping them")
	fs.BoolVar(&f.kOnlyBlack, "k-only-black", false, "Under a CMYK target, convert DeviceGray colours and DeviceRGB black to the black ink alone instead of a four-colour black (images and shadings excepted)")
	fs.BoolVar(&f.noOutputIntent, "no-output-intent", false, "Leave the output intents as they are instead of making the target the output intent")
	fs.BoolVar(&f.replaceOutputIntents, "replace-output-intents", false, "Point a PDF/X or other non-PDF/A output intent over another CMYK profile at the target profile instead of refusing")
	fs.IntVar(&f.jpegQuality, "jpeg-quality", 0, "Re-encode converted JPEG and 8-bit JPEG 2000 images as JPEG at this quality, 1 to 100, instead of losslessly (0: lossless)")
	return &f
}

// colorTarget returns the target and options the flags select, reading the
// --profile file of the icc target.
func (f convertColorsFlags) colorTarget() (pdf.ColorTarget, []pdf.ColorOption, error) {
	if f.profile != "" && f.target != "icc" {
		return pdf.ColorTarget{}, nil, fmt.Errorf("--profile names the profile of --target icc, not of --target %s", f.target)
	}
	var target pdf.ColorTarget
	switch f.target {
	case "output-intent":
		target = pdf.OutputIntentColorTarget()
	case "cmyk":
		target = pdf.CMYKColorTarget()
	case "rgb":
		target = pdf.RGBColorTarget()
	case "gray":
		target = pdf.GrayColorTarget()
	case "icc":
		if f.profile == "" {
			return pdf.ColorTarget{}, nil, fmt.Errorf("--target icc needs --profile profile.icc")
		}
		profile, err := os.ReadFile(filepath.Clean(f.profile))
		if err != nil {
			return pdf.ColorTarget{}, nil, fmt.Errorf("reading the target profile: %w", err)
		}
		target = pdf.ICCColorTarget(profile)
	default:
		return pdf.ColorTarget{}, nil, fmt.Errorf("unknown target %q. Valid: output-intent, cmyk, rgb, gray, icc", f.target)
	}

	intent, ok := renderingIntents[f.intent]
	if !ok {
		return pdf.ColorTarget{}, nil, fmt.Errorf("unknown rendering intent %q. Valid: RelativeColorimetric, Perceptual, Saturation, AbsoluteColorimetric", f.intent)
	}
	opts := []pdf.ColorOption{pdf.WithColorIntent(intent)}
	if f.noBPC {
		opts = append(opts, pdf.WithoutBlackPointCompensation())
	}
	if f.convertSpots {
		opts = append(opts, pdf.WithSpotConversion())
	}
	if f.kOnlyBlack {
		opts = append(opts, pdf.WithKOnlyBlack())
	}
	if f.noOutputIntent {
		opts = append(opts, pdf.WithoutOutputIntent())
	}
	if f.replaceOutputIntents {
		opts = append(opts, pdf.WithReplacedOutputIntents())
	}
	if f.jpegQuality != 0 {
		// A quality outside 1 to 100 is passed on for the library to refuse
		// with its reason.
		opts = append(opts, pdf.WithJPEGQuality(f.jpegQuality))
	}
	return target, opts, nil
}

func runConvertColors(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("convert-colors", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	format := fs.String("f", "text", "Report format: text or json")
	f := registerConvertColorsFlags(fs)

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() < 1 || *output == "" {
		fmt.Fprintln(stderr, "Usage: litho convert-colors <file> [--target output-intent|cmyk|rgb|gray|icc] [--profile profile.icc] [--cmyk-profile profile.icc] [--intent NAME] [--no-bpc] [--convert-spots] [--no-output-intent] [--replace-output-intents] [--jpeg-quality 1-100] -o out.pdf")
		return exitError
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "unknown format %q. Valid: text, json\n", *format)
		return exitError
	}
	target, opts, err := f.colorTarget()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	extra, err := cmykProfileOptions(f.cmykProfile)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password, extra...)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	// This command has no encryption flag, so it is never encrypting.
	warnEncryptionDropped(stderr, r)

	w := pdf.NewWriter(r, defaultPageSize.writeOptions()...)
	defer releaseWriter(w, stderr)

	// Before the output file exists: a refused conversion leaves nothing
	// behind.
	report, err := w.ConvertColors(ctx, target, opts...)
	if err != nil {
		fmt.Fprintf(stderr, "converting colours: %v\n", err)
		return exitError
	}

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	if err := w.WriteTo(ctx, dst); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}
	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	// -o - writes the document to stdout, so the report goes to stderr. See
	// reportWriter.
	return printConvertColorsReport(reportWriter(*output, stdout, stderr), stderr, *format, fs.Arg(0), report)
}

// printConvertColorsReport writes the report in format, text or json.
func printConvertColorsReport(stdout, stderr io.Writer, format, file string, report *pdf.ColorReport) int {
	result := convertColorsResult{
		File:                  file,
		Target:                report.Target,
		ConvertedOperators:    report.ConvertedOperators,
		ConvertedColorSpaces:  report.ConvertedColorSpaces,
		ConvertedImages:       report.ConvertedImages,
		ConvertedShadings:     report.ConvertedShadings,
		KOnlyBlackColors:      report.KOnlyBlackColors,
		KeptSpotColorants:     report.KeptSpotColorants,
		OutputIntent:          report.OutputIntent,
		KeptOutputIntents:     report.KeptOutputIntents,
		ReplacedOutputIntents: report.ReplacedOutputIntents,
		TargetProfile:         report.TargetProfile,
		SourceProfiles:        report.SourceProfiles,
		OverprintPreserved:    report.OverprintPreserved,
		OverprintStates:       report.OverprintStates,
		OverprintChanged:      report.OverprintChanged,

		BlackPointOperators:    report.BlackPointOperators,
		BlackPointInlineImages: report.BlackPointInlineImages,
		BlackPointCopies:       report.BlackPointCopies,
		BlackPointUnhonoured:   report.BlackPointUnhonoured,

		IntentOperators:    report.IntentOperators,
		IntentInlineImages: report.IntentInlineImages,
		IntentCopies:       report.IntentCopies,
		IntentUnhonoured:   report.IntentUnhonoured,
	}
	for _, fb := range report.IntentFallbacks {
		result.IntentFallbacks = append(result.IntentFallbacks, intentFallback(fb))
	}
	if format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(stderr, "encoding report: %v\n", err)
			return exitError
		}
		return exitOK
	}
	fmt.Fprintf(stdout, "Converted %d colour operator(s), %d colour space(s), %d image(s) and %d shading(s) to %s\n",
		result.ConvertedOperators, result.ConvertedColorSpaces, result.ConvertedImages, result.ConvertedShadings, result.Target)
	if result.KOnlyBlackColors > 0 {
		fmt.Fprintf(stdout, "Wrote %d colour(s) on the black ink alone\n", result.KOnlyBlackColors)
	}
	if len(result.KeptSpotColorants) > 0 {
		fmt.Fprintf(stdout, "Kept spot colorant(s): %s\n", strings.Join(result.KeptSpotColorants, ", "))
	}
	if result.OutputIntent != "" {
		fmt.Fprintf(stdout, "Output intent: %s\n", result.OutputIntent)
	}
	if len(result.KeptOutputIntents) > 0 {
		fmt.Fprintf(stdout, "Kept output intent(s): %s\n", strings.Join(result.KeptOutputIntents, ", "))
	}
	if len(result.ReplacedOutputIntents) > 0 {
		fmt.Fprintf(stdout, "Output intent(s) pointed at the target profile: %s\n", strings.Join(result.ReplacedOutputIntents, ", "))
	}
	fmt.Fprintf(stdout, "Target profile: %s\n", result.TargetProfile)
	for _, family := range slices.Sorted(maps.Keys(result.SourceProfiles)) {
		fmt.Fprintf(stdout, "%s read through: %s\n", family, strings.Join(result.SourceProfiles[family], ", "))
	}
	for _, fb := range result.IntentFallbacks {
		side := "source"
		if fb.Target {
			side = "target"
		}
		fmt.Fprintf(stdout, "Rendering intent fell back to the %s table of a %s %d-component %s profile\n",
			fb.Table, side, fb.Components, fb.Profile)
	}
	if result.OverprintPreserved > 0 {
		fmt.Fprintf(stdout, "Kept %d overprinting painting(s) as they print, writing %d graphics state(s) with overprint mode 0\n", result.OverprintPreserved, result.OverprintStates)
	}
	if result.OverprintChanged > 0 {
		fmt.Fprintf(stdout, "Warning: %d overprinting painting(s) print otherwise on a device that overprints\n", result.OverprintChanged)
	}
	if n := result.BlackPointOperators + result.BlackPointInlineImages + result.BlackPointCopies; n > 0 {
		fmt.Fprintf(stdout, "Followed /UseBlackPtComp: %d colour operator(s), %d inline image(s) and %d image or shading copy(ies) converted under the graphics state's black point compensation\n",
			result.BlackPointOperators, result.BlackPointInlineImages, result.BlackPointCopies)
	}
	if result.BlackPointUnhonoured > 0 {
		fmt.Fprintf(stdout, "Warning: %d painting(s) under a /UseBlackPtComp converted with the conversion's own black point compensation instead\n", result.BlackPointUnhonoured)
	}
	if n := result.IntentOperators + result.IntentInlineImages + result.IntentCopies; n > 0 {
		fmt.Fprintf(stdout, "Followed the content's rendering intents: %d colour operator(s), %d inline image(s) and %d image or shading copy(ies) converted under the graphics state's intent\n",
			result.IntentOperators, result.IntentInlineImages, result.IntentCopies)
	}
	if result.IntentUnhonoured > 0 {
		fmt.Fprintf(stdout, "Warning: %d painting(s) under a rendering intent of the content converted with --intent instead\n", result.IntentUnhonoured)
	}
	return exitOK
}
