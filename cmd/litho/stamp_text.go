package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

func runStampText(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stamp-text", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	text := fs.String("text", "", "Text to stamp (required)")
	opacity := fs.Float64("opacity", 1.0, "Opacity (0.0-1.0)")
	rotation := fs.Float64("rotation", 0, "Counter-clockwise rotation in degrees")
	color := fs.String("color", "", "RGB hex color (e.g., #ff0000)")
	fontPath := fs.String("font", "", "Path to a TTF font file")
	fontSize := fs.Float64("font-size", 36, "Font size in points")
	x := fs.Float64("x", 0, "X position in points from bottom-left")
	y := fs.Float64("y", 0, "Y position in points from bottom-left")
	pages := fs.String("pages", "", "Page range (default: all)")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() < 1 || *output == "" || *text == "" {
		fmt.Fprintln(stderr, "Usage: litho stamp-text <file> --text TEXT [--opacity 0.5] [--rotation 45] [--color \"#ff0000\"] [--font path.ttf] [--font-size 36] [--x 0] [--y 0] [--pages RANGE] -o out.pdf")
		return exitError
	}

	stamp := pdf.TextStamp{
		Text:     *text,
		X:        *x,
		Y:        *y,
		FontSize: *fontSize,
		Opacity:  *opacity,
		Rotation: *rotation,
	}

	if *color != "" {
		rgb, err := parseHexColor(*color)
		if err != nil {
			fmt.Fprintf(stderr, "parsing color: %v\n", err)
			return exitError
		}
		stamp.Color = rgb
	}

	if *fontPath != "" {
		data, err := os.ReadFile(*fontPath)
		if err != nil {
			fmt.Fprintf(stderr, "reading font file: %v\n", err)
			return exitError
		}
		font, err := pdf.NewTTFFont(data)
		if err != nil {
			fmt.Fprintf(stderr, "loading font: %v\n", err)
			return exitError
		}
		stamp.Font = font
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	w := pdf.NewWriter(r)

	var pageList []int
	if *pages != "" {
		pageCount, err := r.PageCount(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "reading page count: %v\n", err)
			return exitError
		}
		pageList, err = parsePages(*pages, pageCount)
		if err != nil {
			fmt.Fprintf(stderr, "parsing pages: %v\n", err)
			return exitError
		}
	}

	if err := w.StampText(stamp, pageList...); err != nil {
		fmt.Fprintf(stderr, "stamping text: %v\n", err)
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

	return exitOK
}

// parseHexColor converts a hex color string like "#ff0000" or "ff0000" into
// an [3]float64 with components in the 0.0-1.0 range.
func parseHexColor(s string) ([3]float64, error) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return [3]float64{}, fmt.Errorf("invalid hex color %q: must be 6 hex digits", s)
	}
	r, err := strconv.ParseUint(s[0:2], 16, 8)
	if err != nil {
		return [3]float64{}, fmt.Errorf("invalid hex color %q: %w", s, err)
	}
	g, err := strconv.ParseUint(s[2:4], 16, 8)
	if err != nil {
		return [3]float64{}, fmt.Errorf("invalid hex color %q: %w", s, err)
	}
	b, err := strconv.ParseUint(s[4:6], 16, 8)
	if err != nil {
		return [3]float64{}, fmt.Errorf("invalid hex color %q: %w", s, err)
	}
	return [3]float64{float64(r) / 255.0, float64(g) / 255.0, float64(b) / 255.0}, nil
}
