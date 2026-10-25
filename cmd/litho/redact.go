package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// regionFlags collects repeated --region page:x,y,width,height values.
type regionFlags []string

func (f *regionFlags) String() string { return strings.Join(*f, ", ") }
func (f *regionFlags) Set(val string) error {
	*f = append(*f, val)
	return nil
}

// redactResult is the JSON report, naming what was removed per vector.
//
// Redaction is destructive and security-critical, so the report enumerates
// what went rather than reporting a bare success: a caller has to be able to
// check that what it meant to remove is what was removed.
type redactResult struct {
	File                    string           `json:"file"`
	RemovedText             []string         `json:"removedText"`
	BlankedImages           []string         `json:"blankedImages,omitempty"`
	ImagesBlankedInCopy     map[int][]string `json:"imagesBlankedInCopy,omitempty"`
	FormsCopied             map[int]int      `json:"formsCopied,omitempty"`
	RemovedPaths            int              `json:"removedPaths"`
	ClippedPaths            int              `json:"clippedPaths,omitempty"`
	RemovedImages           int              `json:"removedImages,omitempty"`
	RasterisedUnits         int              `json:"rasterisedUnits,omitempty"`
	BakedAnnotations        int              `json:"bakedAnnotations,omitempty"`
	LossyBlankedImages      int              `json:"lossyBlankedImages,omitempty"`
	RemovedAnnotations      []int            `json:"removedAnnotations,omitempty"`
	ClippedAnnotations      []int            `json:"clippedAnnotations,omitempty"`
	DroppedPageEntries      []string         `json:"droppedPageEntries,omitempty"`
	FontsFullyRemovedOnPage map[int][]int    `json:"fontsFullyRemovedOnPage,omitempty"`
	ScrubbedAnnotations     []int            `json:"scrubbedAnnotations,omitempty"`
	ScrubbedFormFields      []int            `json:"scrubbedFormFields,omitempty"`
	ScrubbedOutlineItems    []int            `json:"scrubbedOutlineItems,omitempty"`
	CopiesOnOtherPages      []otherCopy      `json:"copiesOnOtherPages,omitempty"`
	CopiesOnRedactedPages   []samePageCopy   `json:"copiesOnRedactedPages,omitempty"`
}

// otherCopy is a removed string a page outside the regions still draws. It is
// a warning: the redaction succeeded, and the output keeps the copy.
type otherCopy struct {
	Token     string `json:"token"`
	ObjectNum int    `json:"object"`
	Where     string `json:"where"`
	Pages     []int  `json:"pages"`
}

// samePageCopy is a removed string a redacted page still draws as visible
// text outside every region, with the boxes of the runs that spell it. It is
// a warning: the redaction succeeded, and the output keeps the copy.
type samePageCopy struct {
	Token     string       `json:"token"`
	ObjectNum int          `json:"object"`
	Page      int          `json:"page"`
	Boxes     [][4]float64 `json:"boxes,omitempty"`
}

// parseRedactRegion reads one "page:x,y,width,height" argument.
//
// The flag takes x, y, width and height because that is how a box is measured
// off a page, while [pdf.RedactRegion.Rect] is [llx, lly, urx, ury], the shape
// of an annotation /Rect (ISO 32000-1, Table 166). The conversion happens here,
// once and explicitly: passing a width where an upper-right coordinate belongs
// would clear the wrong area, and on a security operation silently redacting
// the wrong box is the worst failure available.
func parseRedactRegion(spec string) (pdf.RedactRegion, error) {
	pageText, rectText, found := strings.Cut(spec, ":")
	if !found {
		return pdf.RedactRegion{}, fmt.Errorf("region %q: want page:x,y,width,height", spec)
	}

	page, err := strconv.Atoi(strings.TrimSpace(pageText))
	if err != nil || page < 1 {
		return pdf.RedactRegion{}, fmt.Errorf("region %q: page must be a positive whole number", spec)
	}

	parts := strings.Split(rectText, ",")
	if len(parts) != 4 {
		return pdf.RedactRegion{}, fmt.Errorf("region %q: want four numbers x,y,width,height", spec)
	}
	var n [4]float64
	for i, part := range parts {
		value, convErr := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if convErr != nil {
			return pdf.RedactRegion{}, fmt.Errorf("region %q: %q is not a number", spec, part)
		}
		n[i] = value
	}
	if n[2] <= 0 || n[3] <= 0 {
		return pdf.RedactRegion{}, fmt.Errorf("region %q: width and height must be greater than zero", spec)
	}

	return pdf.RedactRegion{
		Page: page,
		Rect: [4]float64{n[0], n[1], n[0] + n[2], n[1] + n[3]},
	}, nil
}

func runRedact(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("redact", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultPageSize := registerDefaultPageSizeFlag(fs)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	format := fs.String("f", "text", "Report format: text or json")
	// Redaction re-encodes an image it cannot keep in its own colour space,
	// such as an indexed image over DeviceCMYK, as RGB, and this profile is
	// what its CMYK colours are read in when the document names none.
	cmykProfile := fs.String("cmyk-profile", "", "ICC profile DeviceCMYK is read in when the document names none (default: built-in SWOP profile)")
	strict := fs.Bool("strict", false, "Refuse when a place outside the regions still draws removed text or a blanked image, instead of warning")
	rasterFallback := fs.Bool("raster-fallback", false, "Rasterise the form XObjects and shadings the regions reach, refused otherwise, as images cut out of the regions")
	noClipAnnots := fs.Bool("no-clip-annotations", false, "Refuse every annotation reaching outside a region, instead of keeping the links and closed pop-ups that draw nothing, a link cut to the part outside")
	// Clipping runs by default now; the flag stays so scripts that pass it
	// keep working.
	fs.Bool("clip-annotations", true, "Deprecated: annotations that draw nothing are clipped by default (see --no-clip-annotations)")
	glyphOutlines := fs.Bool("glyph-outlines", false, "Also remove a glyph whose outline, not only its advance box, reaches into a region, such as a script face's swash or an italic overhang")
	noPixelCheck := fs.Bool("no-pixel-check", false, "Skip the pixel check, which renders each redacted page before and after and refuses unless the regions show only paper or blanked images and nothing else changed")
	// The check runs by default now; the flag stays so scripts that pass it
	// keep working.
	fs.Bool("pixel-check", true, "Deprecated: the pixel check runs by default (see --no-pixel-check)")
	jpegQuality := fs.Int("jpeg-quality", 0, "Encode the photographic tiles no region touches, of the JPEG and JPEG 2000 photographs the redaction blanks and of the --raster-fallback images, as JPEG at this quality, 1 to 100, unless Flate is no larger (default: every image lossless)")

	var regionSpecs regionFlags
	fs.Var(&regionSpecs, "region", "Region to clear, page:x,y,width,height (repeatable, required)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || len(regionSpecs) == 0 {
		fmt.Fprintln(stderr, "Usage: litho redact <file> --region page:x,y,width,height [--region ...] [--cmyk-profile profile.icc] [--strict] [--raster-fallback] [--no-clip-annotations] [--glyph-outlines] [--jpeg-quality Q] [--no-pixel-check] -o out.pdf")
		return exitError
	}

	regions := make([]pdf.RedactRegion, 0, len(regionSpecs))
	for _, spec := range regionSpecs {
		region, err := parseRedactRegion(spec)
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return exitError
		}
		regions = append(regions, region)
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

	// Redaction is the case that matters most: the caller proved they hold the
	// password, removed confidential content, and got back a document anyone
	// can open. This command has no encryption flag, so it is never encrypting.
	warnEncryptionDropped(stderr, r)

	w := pdf.NewWriter(r, defaultPageSize.writeOptions()...)
	defer releaseWriter(w, stderr)

	// Before the output file exists: a refused redaction must leave nothing
	// behind, and a half-redacted file on disk is the failure this command
	// exists to prevent.
	var redactOpts []pdf.RedactOption
	if *strict {
		redactOpts = append(redactOpts, pdf.WithStrictRedaction())
	}
	if *rasterFallback {
		redactOpts = append(redactOpts, pdf.WithRedactionRasterFallback())
	}
	if *noClipAnnots {
		redactOpts = append(redactOpts, pdf.WithoutRedactionAnnotationClipping())
	}
	if *glyphOutlines {
		redactOpts = append(redactOpts, pdf.WithRedactionGlyphOutlines())
	}
	if *jpegQuality != 0 {
		redactOpts = append(redactOpts, pdf.WithRedactionJPEGQuality(*jpegQuality))
	}
	if *noPixelCheck {
		redactOpts = append(redactOpts, pdf.WithoutRedactionPixelCheck())
	}
	report, err := w.Redact(ctx, regions, redactOpts...)
	if err != nil {
		fmt.Fprintf(stderr, "redacting: %v\n", err)
		if errors.Is(err, pdf.ErrRedactionIncomplete) && report != nil {
			for _, hit := range report.SurvivingCopies {
				fmt.Fprintf(stderr, "  copy of %q survives in object %d (%s)\n",
					hit.Token, hit.ObjectNum, hit.Where)
			}
			for _, objNum := range report.UnreadableObjects {
				fmt.Fprintf(stderr, "  object %d could not be read, so it is not proved clean\n", objNum)
			}
		}
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

	// -o - writes the document to stdout, so the report goes to stderr rather
	// than landing inside the bytes the caller is capturing. See reportWriter.
	return printRedactReport(reportWriter(*output, stdout, stderr), stderr, *format, fs.Arg(0), report)
}

// printRedactReport writes the report in the requested format.
func printRedactReport(stdout, stderr io.Writer, format, file string, report *pdf.RedactReport) int {
	result := redactResult{
		File:                    file,
		RemovedText:             report.RemovedText,
		BlankedImages:           report.BlankedImages,
		ImagesBlankedInCopy:     report.ImagesBlankedInCopy,
		FormsCopied:             report.FormsCopied,
		RemovedPaths:            report.RemovedPaths,
		ClippedPaths:            report.ClippedPaths,
		RemovedImages:           report.RemovedImages,
		RasterisedUnits:         report.RasterisedUnits,
		BakedAnnotations:        report.BakedAnnotations,
		LossyBlankedImages:      report.LossyBlankedImages,
		RemovedAnnotations:      report.RemovedAnnotations,
		ClippedAnnotations:      report.ClippedAnnotations,
		DroppedPageEntries:      report.DroppedPageEntries,
		FontsFullyRemovedOnPage: report.FontsFullyRemovedOnPage,
		ScrubbedAnnotations:     report.ScrubbedAnnotations,
		ScrubbedFormFields:      report.ScrubbedFormFields,
		ScrubbedOutlineItems:    report.ScrubbedOutlineItems,
	}
	for _, hit := range report.CopiesOnOtherPages {
		result.CopiesOnOtherPages = append(result.CopiesOnOtherPages,
			otherCopy{Token: hit.Token, ObjectNum: hit.ObjectNum, Where: hit.Where, Pages: hit.Pages})
	}
	// A copy left on another page is a warning, so it reaches stderr whatever
	// the format: a caller reading only the JSON still sees it in its fields.
	for _, c := range result.CopiesOnOtherPages {
		fmt.Fprintf(stderr, "warning: a copy of %q remains on page(s) %v, object %d (%s); --strict refuses it\n",
			c.Token, c.Pages, c.ObjectNum, c.Where)
	}
	for _, hit := range report.CopiesOnRedactedPages {
		page := 0
		if len(hit.Pages) > 0 {
			page = hit.Pages[0]
		}
		result.CopiesOnRedactedPages = append(result.CopiesOnRedactedPages,
			samePageCopy{Token: hit.Token, ObjectNum: hit.ObjectNum, Page: page, Boxes: hit.Boxes})
	}
	for _, c := range result.CopiesOnRedactedPages {
		if len(c.Boxes) == 0 {
			fmt.Fprintf(stderr, "warning: page %d content still holds %q, in operator numbers only, object %d; --strict refuses it\n",
				c.Page, c.Token, c.ObjectNum)
			continue
		}
		fmt.Fprintf(stderr, "warning: page %d still shows %q outside the regions at %v, object %d; --strict refuses it\n",
			c.Page, c.Token, c.Boxes, c.ObjectNum)
	}
	// So is an image blanked in a copy: the image keeps its samples, in the
	// file and wherever else it is painted.
	for _, page := range slices.Sorted(maps.Keys(result.ImagesBlankedInCopy)) {
		fmt.Fprintf(stderr, "warning: page %d blanked image(s) %v in a copy; the image itself remains where else it is painted; --strict refuses it\n",
			page, result.ImagesBlankedInCopy[page])
	}

	switch format {
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(stderr, "encoding report: %v\n", err)
			return exitError
		}
	case "text":
		fmt.Fprintf(stdout, "Removed %d text run(s), %d path(s), %d image(s)\n",
			len(result.RemovedText), result.RemovedPaths, len(result.BlankedImages))
		if n := result.ClippedPaths; n > 0 {
			fmt.Fprintf(stdout, "Clipped %d path(s) at the region edges\n", n)
		}
		if n := result.RemovedImages; n > 0 {
			fmt.Fprintf(stdout, "Removed %d image placement(s) inside the regions\n", n)
		}
		if n := result.RasterisedUnits; n > 0 {
			fmt.Fprintf(stdout, "Rasterised %d painting element(s) outside the regions\n", n)
		}
		if n := result.BakedAnnotations; n > 0 {
			fmt.Fprintf(stdout, "Drew %d annotation(s) reaching outside the regions into the page and removed them\n", n)
		}
		if n := result.LossyBlankedImages; n > 0 {
			fmt.Fprintf(stdout, "Re-encoded %d blanked photograph(s) as JPEG outside the regions\n", n)
		}
		for _, page := range slices.Sorted(maps.Keys(result.FormsCopied)) {
			fmt.Fprintf(stdout, "Page %d draws %d copied form(s) holding images blanked in a copy\n",
				page, result.FormsCopied[page])
		}
		if n := len(result.RemovedAnnotations); n > 0 {
			fmt.Fprintf(stdout, "Removed %d annotation(s) inside the regions\n", n)
		}
		if n := len(result.ClippedAnnotations); n > 0 {
			fmt.Fprintf(stdout, "Cut %d link(s) to the part outside the regions\n", n)
		}
		if n := len(result.ScrubbedAnnotations) + len(result.ScrubbedFormFields) + len(result.ScrubbedOutlineItems); n > 0 {
			fmt.Fprintf(stdout, "Scrubbed %d document-wide copy holder(s)\n", n)
		}
		// Naming these is the point: litho cannot re-subset the embedded
		// program, so its outlines still describe the removed glyphs. Pages
		// print in ascending order: ranging the map printed them in a
		// different order on every run (CONTRIBUTING "Deterministic output").
		for _, page := range slices.Sorted(maps.Keys(result.FontsFullyRemovedOnPage)) {
			fmt.Fprintf(stdout, "Page %d no longer draws with font object(s) %v; their embedded outlines remain\n",
				page, result.FontsFullyRemovedOnPage[page])
		}
	default:
		fmt.Fprintf(stderr, "unknown format %q. Valid: text, json\n", format)
		return exitError
	}
	return exitOK
}
