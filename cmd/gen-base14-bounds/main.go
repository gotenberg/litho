package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The PDFBox commit the AFM files are read at. The download uses the commit,
// which cannot move.
const (
	pdfboxCommit = "78be2c012e6b5f0c0b368dc9863fc01e9ec78f85"
	pdfboxRepo   = "https://github.com/apache/pdfbox"
	rawBaseURL   = "https://raw.githubusercontent.com/apache/pdfbox"
	afmDir       = "pdfbox/src/main/resources/org/apache/pdfbox/resources/afm"
	outputName   = "base14_bounds.go"
	commentWidth = 80
)

// maxSourceBytes caps each downloaded AFM file. Anti-DoS guard for a
// build-time tool, not a format limit: the largest Core 14 AFM, Times-Roman,
// is about 60 KB, so 1 MiB leaves room while refusing a runaway response.
const maxSourceBytes = 1 << 20

// httpClient bounds how long the generator waits for GitHub. The bare
// http.Get default has no timeout.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// fonts lists the standard 14 fonts (ISO 32000-1, 9.6.2.2), each read from
// the AFM file of its name.
var fonts = []string{
	"Courier", "Courier-Bold", "Courier-BoldOblique", "Courier-Oblique",
	"Helvetica", "Helvetica-Bold", "Helvetica-BoldOblique", "Helvetica-Oblique",
	"Symbol",
	"Times-Bold", "Times-BoldItalic", "Times-Italic", "Times-Roman",
	"ZapfDingbats",
}

// license is the notice Adobe's Core 14 AFM ReadMe (MustRead.html) requires
// to travel with the AFM data, unmodified.
const license = "This file and the 14 PostScript(R) AFM files it accompanies may be " +
	"used, copied, and distributed for any purpose and without charge, with or " +
	"without modification, provided that all copyright notices are retained; " +
	"that the AFM files are not distributed without this file; that all " +
	"modifications to this file or any of the AFM files are prominently noted " +
	"in the modified file(s); and that this paragraph is not modified. Adobe " +
	"Systems has no responsibility or obligation to support the use of the AFM files."

var (
	errFontName      = errors.New("FontName does not match the file")
	errNoCopyright   = errors.New("copyright comment not found")
	errNoCharMetrics = errors.New("character metrics not found")
	errCharMetric    = errors.New("malformed character metric")
	errDuplicateName = errors.New("duplicate glyph name")
)

// afm is what the generator reads of one AFM file.
type afm struct {
	copyright string
	bounds    map[string][4]int
}

func main() {
	src := flag.String("src", "", "read the AFM files from this directory instead of downloading them at the pinned commit")
	out := flag.String("out", "", "output file (default: pkg/internal/typography/"+outputName+")")
	flag.Parse()

	parsed := make(map[string]afm, len(fonts))
	for _, name := range fonts {
		data, err := readSource(*src, name)
		if err != nil {
			fatalf("%v", err)
		}
		a, err := parseAFM(data, name)
		if err != nil {
			fatalf("parsing %s.afm: %v", name, err)
		}
		parsed[name] = a
	}
	code, err := generate(parsed)
	if err != nil {
		fatalf("%v", err)
	}
	path := *out
	if path == "" {
		path = filepath.Join(findTypographyDir(), outputName)
	}
	if err := os.WriteFile(path, code, 0o600); err != nil {
		fatalf("writing %s: %v", path, err)
	}
	fmt.Printf("Generated %s from PDFBox %s\n", path, pdfboxCommit)
}

// readSource returns the AFM file of font name from dir, or from GitHub at
// [pdfboxCommit] when dir is empty.
func readSource(dir, name string) ([]byte, error) {
	if dir != "" {
		path := filepath.Join(dir, name+".afm")
		data, err := os.ReadFile(path) //nolint:gosec // the directory is the developer's own command-line flag
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		return data, nil
	}
	url := rawBaseURL + "/" + pdfboxCommit + "/" + afmDir + "/" + name + ".afm"
	resp, err := httpClient.Get(url) //nolint:gosec // the URL is built from package constants
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	if len(data) > maxSourceBytes {
		return nil, fmt.Errorf("downloading %s: larger than %d bytes", url, maxSourceBytes)
	}
	return data, nil
}

// parseAFM reads the copyright comment and the glyph boxes of the AFM file
// data, which must name font name. Each line between StartCharMetrics and
// EndCharMetrics is a character metric: semicolon-separated keys, of which N
// names the glyph and B gives its box llx lly urx ury (Adobe Technical Note
// #5004, section 8.2).
func parseAFM(data []byte, name string) (afm, error) {
	a := afm{bounds: make(map[string][4]int)}
	inMetrics, sawMetrics := false, false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "FontName "):
			if got := strings.TrimSpace(strings.TrimPrefix(line, "FontName ")); got != name {
				return afm{}, fmt.Errorf("%w: %q", errFontName, got)
			}
		case strings.HasPrefix(line, "Comment Copyright "):
			if a.copyright == "" {
				a.copyright = strings.TrimSpace(strings.TrimPrefix(line, "Comment "))
			}
		case strings.HasPrefix(line, "StartCharMetrics"):
			inMetrics, sawMetrics = true, true
		case strings.HasPrefix(line, "EndCharMetrics"):
			inMetrics = false
		case inMetrics && line != "":
			glyph, box, err := parseCharMetric(line)
			if err != nil {
				return afm{}, err
			}
			if _, dup := a.bounds[glyph]; dup {
				return afm{}, fmt.Errorf("%w: %q", errDuplicateName, glyph)
			}
			a.bounds[glyph] = box
		}
	}
	if err := sc.Err(); err != nil {
		return afm{}, err
	}
	if a.copyright == "" {
		return afm{}, errNoCopyright
	}
	if !sawMetrics || len(a.bounds) == 0 {
		return afm{}, errNoCharMetrics
	}
	return a, nil
}

// parseCharMetric returns the glyph name and box of one character metric
// line, such as "C 65 ; WX 667 ; N A ; B 14 0 654 718 ;".
func parseCharMetric(line string) (string, [4]int, error) {
	var (
		glyph  string
		box    [4]int
		hasBox bool
	)
	for field := range strings.SplitSeq(line, ";") {
		parts := strings.Fields(field)
		if len(parts) == 0 {
			continue
		}
		switch parts[0] {
		case "N":
			if len(parts) != 2 {
				return "", box, fmt.Errorf("%w: %q", errCharMetric, line)
			}
			glyph = parts[1]
		case "B":
			if len(parts) != 5 {
				return "", box, fmt.Errorf("%w: %q", errCharMetric, line)
			}
			for i := range box {
				v, err := strconv.Atoi(parts[i+1])
				if err != nil {
					return "", box, fmt.Errorf("%w: %q: %w", errCharMetric, line, err)
				}
				box[i] = v
			}
			hasBox = true
		}
	}
	if glyph == "" || !hasBox {
		return "", box, fmt.Errorf("%w: %q", errCharMetric, line)
	}
	return glyph, box, nil
}

// generate renders the gofmt-formatted Go file carrying the glyph boxes of
// every font of [fonts].
func generate(parsed map[string]afm) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by cmd/gen-base14-bounds; DO NOT EDIT.\n\n")
	buf.WriteString(comment(fmt.Sprintf("Derived from the Core 14 AFM files of Apache PDFBox (%s), "+
		"commit %s, directory %s. The glyph boxes are transcribed unchanged.", pdfboxRepo, pdfboxCommit, afmDir)))
	buf.WriteString("//\n")
	for _, name := range fonts {
		buf.WriteString(comment(name + ": " + parsed[name].copyright))
	}
	buf.WriteString("//\n")
	buf.WriteString(comment(license))
	buf.WriteString("\npackage typography\n\n")
	buf.WriteString(comment("standard14GlyphBounds maps each standard 14 font to the bounding box of " +
		"each of its glyphs, keyed by glyph name: llx, lly, urx and ury in glyph space units, " +
		"1000 to the em, from the B entries of its AFM file (Adobe Technical Note #5004, section 8.2)."))
	buf.WriteString("var standard14GlyphBounds = map[string]map[string][4]int16{\n")
	for _, name := range fonts {
		a, ok := parsed[name]
		if !ok {
			return nil, fmt.Errorf("no AFM data for %s", name)
		}
		fmt.Fprintf(&buf, "\t%q: {\n", name)
		for _, glyph := range slices.Sorted(maps.Keys(a.bounds)) {
			b := a.bounds[glyph]
			fmt.Fprintf(&buf, "\t\t%q: {%d, %d, %d, %d},\n", glyph, b[0], b[1], b[2], b[3])
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}\n")
	code, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated source: %w", err)
	}
	return code, nil
}

// comment wraps text into // lines of at most [commentWidth] columns.
func comment(text string) string {
	var b strings.Builder
	line := "//"
	for word := range strings.FieldsSeq(text) {
		if len(line) > len("//") && len(line)+1+len(word) > commentWidth {
			b.WriteString(line + "\n")
			line = "//"
		}
		line += " " + word
	}
	b.WriteString(line + "\n")
	return b.String()
}

func findTypographyDir() string {
	for _, c := range []string{".", "pkg/internal/typography"} {
		if _, err := os.Stat(filepath.Join(c, "base14.go")); err == nil {
			return c
		}
	}
	fatalf("cannot find the typography package directory; run from the repo root or pkg/internal/typography/")
	return ""
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-base14-bounds: "+format+"\n", args...)
	os.Exit(1)
}
