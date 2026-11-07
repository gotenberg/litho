package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// placeFlag collects the values of the repeatable -place flag, parsed as they
// are given, so a malformed one fails before any file opens.
type placeFlag struct {
	paths      []string
	placements []pdf.Placement
}

// String implements [flag.Value].
func (f *placeFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(f.paths, " ")
}

// Set implements [flag.Value]: comma-separated key=value pairs, src
// required, page, on, x, y, scale and rotation optional.
func (f *placeFlag) Set(value string) error {
	pl := pdf.Placement{Page: 1, TargetPage: 1, Scale: 1}
	var src string
	seen := make(map[string]bool)
	for pair := range strings.SplitSeq(value, ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(pair), "=")
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if !ok || key == "" {
			return fmt.Errorf("%q is not key=value", pair)
		}
		if seen[key] {
			return fmt.Errorf("%s given twice", key)
		}
		seen[key] = true
		var err error
		switch key {
		case "src":
			src = val
		case "page":
			pl.Page, err = strconv.Atoi(val)
		case "on":
			pl.TargetPage, err = strconv.Atoi(val)
		case "x":
			pl.X, err = strconv.ParseFloat(val, 64)
		case "y":
			pl.Y, err = strconv.ParseFloat(val, 64)
		case "scale":
			pl.Scale, err = strconv.ParseFloat(val, 64)
		case "rotation":
			pl.Rotation, err = strconv.ParseFloat(val, 64)
		default:
			return fmt.Errorf("unknown key %q: expected src, page, on, x, y, scale or rotation", key)
		}
		if err != nil {
			return fmt.Errorf("%s: %q is not a number", key, val)
		}
	}
	if src == "" {
		return errors.New("src is required")
	}
	f.paths = append(f.paths, src)
	f.placements = append(f.placements, pl)
	return nil
}

func runCompose(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compose", flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password, for every source")
	width := fs.Float64("width", 0, "Page width in points, 3 to 14400 (required)")
	height := fs.Float64("height", 0, "Page height in points, 3 to 14400 (required)")
	var places placeFlag
	fs.Var(&places, "place", "Draw a source page: src=PATH[,page=N][,on=N][,x=X][,y=Y][,scale=S][,rotation=DEG]. "+
		"page is the source page and on the composed page, both 1-based and 1 by default; "+
		"x and y, in points, place the bottom-left of the source page as a viewer shows it; "+
		"scale defaults to 1 and rotation, counter-clockwise in degrees, to 0. Repeatable, drawn in order")
	compactFlags := registerCompactFlags(fs)

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	if fs.NArg() > 0 || *output == "" || len(places.placements) == 0 {
		fmt.Fprintln(stderr, "Usage: litho compose -width W -height H -place src=a.pdf[,page=1][,on=1][,x=0][,y=0][,scale=1][,rotation=0] [-place ...] [--compact] [--recompress-images] [--subset-fonts] -o out.pdf")
		return exitError
	}

	// Each path opens once, however many placements name it, so its
	// placements share what they draw from it.
	readers := make(map[string]*pdf.Reader)
	var opened []*pdf.Reader
	var closers []func()
	defer func() {
		for _, c := range closers {
			c()
		}
	}()
	placements := make([]pdf.Placement, len(places.placements))
	for i, pl := range places.placements {
		path := places.paths[i]
		r, ok := readers[path]
		if !ok {
			var closer func()
			var err error
			r, closer, err = openPDF(ctx, path, *password)
			if err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
			readers[path] = r
			opened = append(opened, r)
			closers = append(closers, closer)
		}
		pl.Source = r
		placements[i] = pl
	}

	// The output carries none of the sources' protection and none of their
	// form fields, which a composed page does not draw.
	warnEncryptionDropped(stderr, opened...)
	warnXFADropped(ctx, stderr, opened...)

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	var dropped []pdf.DroppedSignature
	writeOpts := append(compactWriteOptions(compactFlags.get()), pdf.WithDroppedSignatures(&dropped))
	if err := pdf.Compose(ctx, *width, *height, placements, dst, writeOpts...); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "composing: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	warnSignaturesDropped(stderr, dropped)
	return exitOK
}
