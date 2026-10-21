package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// convertColorsResult is the JSON report of convert-colors.
type convertColorsResult struct {
	File                 string   `json:"file"`
	Target               string   `json:"target"`
	ConvertedOperators   int      `json:"convertedOperators"`
	ConvertedColorSpaces int      `json:"convertedColorSpaces"`
	ConvertedImages      int      `json:"convertedImages"`
	ConvertedShadings    int      `json:"convertedShadings"`
	KeptSpotColorants    []string `json:"keptSpotColorants,omitempty"`
	OutputIntent         string   `json:"outputIntent,omitempty"`
	KeptOutputIntents    []string `json:"keptOutputIntents,omitempty"`
	// ReplacedOutputIntents names the intents --replace-output-intents
	// pointed at the target profile, by /OutputConditionIdentifier.
	ReplacedOutputIntents []string `json:"replacedOutputIntents,omitempty"`
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
	bpc, convertSpots       bool
	noOutputIntent          bool
	replaceOutputIntents    bool
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
	if f.bpc {
		opts = append(opts, pdf.WithBlackPointCompensation())
	}
	if f.convertSpots {
		opts = append(opts, pdf.WithSpotConversion())
	}
	if f.noOutputIntent {
		opts = append(opts, pdf.WithoutOutputIntent())
	}
	if f.replaceOutputIntents {
		opts = append(opts, pdf.WithReplacedOutputIntents())
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
	var f convertColorsFlags
	fs.StringVar(&f.target, "target", "output-intent", "Colour space to convert to: output-intent, cmyk, rgb, gray or icc")
	fs.StringVar(&f.profile, "profile", "", "ICC profile of --target icc")
	cmykProfile := fs.String("cmyk-profile", "", "ICC profile of --target cmyk, and the one DeviceCMYK is read in when the document names none (default: built-in SWOP profile)")
	fs.StringVar(&f.intent, "intent", "RelativeColorimetric", "Rendering intent: RelativeColorimetric, Perceptual, Saturation or AbsoluteColorimetric")
	fs.BoolVar(&f.bpc, "bpc", false, "Apply black point compensation")
	fs.BoolVar(&f.convertSpots, "convert-spots", false, "Convert spot colours (Separation, DeviceN) to process colour instead of keeping them")
	fs.BoolVar(&f.noOutputIntent, "no-output-intent", false, "Leave the output intents as they are instead of making the target the output intent")
	fs.BoolVar(&f.replaceOutputIntents, "replace-output-intents", false, "Point a PDF/X or other non-PDF/A output intent over another CMYK profile at the target profile instead of refusing")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}
	if fs.NArg() < 1 || *output == "" {
		fmt.Fprintln(stderr, "Usage: litho convert-colors <file> [--target output-intent|cmyk|rgb|gray|icc] [--profile profile.icc] [--cmyk-profile profile.icc] [--intent NAME] [--bpc] [--convert-spots] [--no-output-intent] [--replace-output-intents] -o out.pdf")
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

	var extra []pdf.OpenOption
	if *cmykProfile != "" {
		profile, err := os.ReadFile(filepath.Clean(*cmykProfile))
		if err != nil {
			fmt.Fprintf(stderr, "reading the CMYK profile: %v\n", err)
			return exitError
		}
		extra = append(extra, pdf.WithDefaultCMYKProfile(profile))
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
		KeptSpotColorants:     report.KeptSpotColorants,
		OutputIntent:          report.OutputIntent,
		KeptOutputIntents:     report.KeptOutputIntents,
		ReplacedOutputIntents: report.ReplacedOutputIntents,
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
	return exitOK
}
