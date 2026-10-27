package main

import (
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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The PDF.js revision the generated tables come from: PDFJS_REVISION of
// test/integration/corpus/versions.env, the master commit the text goldens are
// generated with, so litho reads a font with the tables PDF.js read it with
// when the goldens were made. The download uses the commit, which cannot move.
const (
	pdfjsCommit = "1591ddfa8d0dfda931812cd7b654e12442c14c22"
	pdfjsRepo   = "https://github.com/mozilla/pdf.js"
	rawBaseURL  = "https://raw.githubusercontent.com/mozilla/pdf.js"
	sourcePath  = "src/core/standard_fonts.js"
	outputName  = "pdfjs_glyphs.go"
)

// maxSourceBytes caps the downloaded source. Anti-DoS guard for a build-time
// tool, not a format limit: standard_fonts.js is about 25 KB, so 1 MiB leaves
// room for upstream growth while refusing a runaway response.
const maxSourceBytes = 1 << 20

// httpClient bounds how long the generator waits for GitHub. The bare
// http.Get default has no timeout.
var httpClient = &http.Client{Timeout: 30 * time.Second}

var (
	errTableNotFound  = errors.New("lookup table not found")
	errUnexpectedStmt = errors.New("unexpected statement in lookup table")
	errEntryRange     = errors.New("lookup table entry out of range")
	errDuplicateEntry = errors.New("duplicate lookup table entry")
	errNoCopyright    = errors.New("copyright notice not found")
	errDuplicateName  = errors.New("duplicate font name entry")
)

// table names one PDF.js lookup table and the Go variable it becomes.
type table struct {
	JSName string // PDF.js function, e.g. getSupplementalGlyphMapForCalibri
	GoName string // generated map variable
	Doc    string // doc comment text, unwrapped
}

// tables lists the PDF.js glyph lookup tables the generated file carries.
var tables = []table{
	{
		JSName: "getGlyphMapForStandardFonts",
		GoName: "pdfjsStandardFontGlyphs",
		Doc: "pdfjsStandardFontGlyphs is PDF.js's getGlyphMapForStandardFonts: the " +
			"glyph ID to Unicode table PDF.js applies to a non-embedded standard font, " +
			"or a font it maps to one, that a CIDFontType2 draws through an Identity CMap.",
	},
	{
		JSName: "getSupplementalGlyphMapForArialBlack",
		GoName: "pdfjsArialBlackGlyphs",
		Doc: "pdfjsArialBlackGlyphs is PDF.js's getSupplementalGlyphMapForArialBlack: the " +
			"Arial Black glyph IDs PDF.js maps on top of [pdfjsStandardFontGlyphs] for a " +
			"non-embedded Arial Black.",
	},
	{
		JSName: "getSupplementalGlyphMapForCalibri",
		GoName: "pdfjsCalibriGlyphs",
		Doc: "pdfjsCalibriGlyphs is PDF.js's getSupplementalGlyphMapForCalibri: the " +
			"Calibri glyph IDs PDF.js maps on top of [pdfjsStandardFontGlyphs] for a " +
			"non-embedded Calibri.",
	},
}

// nameTable names one PDF.js lookup table keyed by font name and the Go
// variable it becomes. A flag table assigns true, the others a font name.
type nameTable struct {
	JSName string
	GoName string
	Flag   bool
	Doc    string
}

// nameTables lists the PDF.js font-name lookup tables the generated file
// carries: the ones fonts.js and evaluator.js consult to decide whether a font
// is standard, mapped to a standard one, or known at all.
var nameTables = []nameTable{
	{
		JSName: "getStdFontMap",
		GoName: "pdfjsStdFontMap",
		Doc: "pdfjsStdFontMap is PDF.js's getStdFontMap: the standard 14 fonts and the " +
			"names PDF.js reads as one of them, keyed by the normalised font name.",
	},
	{
		JSName: "getNonStdFontMap",
		GoName: "pdfjsNonStdFontMap",
		Doc: "pdfjsNonStdFontMap is PDF.js's getNonStdFontMap: the non-standard fonts " +
			"PDF.js substitutes another font for, keyed by the normalised font name.",
	},
	{
		JSName: "getSerifFonts",
		GoName: "pdfjsSerifFonts",
		Flag:   true,
		Doc:    "pdfjsSerifFonts is PDF.js's getSerifFonts: the font families PDF.js knows as serif faces.",
	},
	{
		JSName: "getSymbolsFonts",
		GoName: "pdfjsSymbolsFonts",
		Flag:   true,
		Doc:    "pdfjsSymbolsFonts is PDF.js's getSymbolsFonts: the font names PDF.js knows as symbol faces.",
	},
}

var (
	nameAssignRE = regexp.MustCompile(`^t(?:\.([A-Za-z_][A-Za-z0-9_]*)|\["([^"\\]+)"\]) = (?:"([^"\\]*)"|(true));$`)
	assignRE     = regexp.MustCompile(`^t\[(\d+)\] = (\d+);$`)
	copyrightRE  = regexp.MustCompile(`Copyright (\d{4}(?:-\d{4})?) Mozilla Foundation`)
)

func main() {
	src := flag.String("src", "", "read standard_fonts.js from this file instead of downloading it at the pinned commit")
	out := flag.String("out", "", "output file (default: pkg/internal/typography/stdfonts/"+outputName+")")
	flag.Parse()

	js, err := readSource(*src)
	if err != nil {
		fatalf("%v", err)
	}
	code, err := generate(js)
	if err != nil {
		fatalf("%v", err)
	}
	path := *out
	if path == "" {
		path = filepath.Join(findStdfontsDir(), outputName)
	}
	if err := os.WriteFile(path, code, 0o600); err != nil {
		fatalf("writing %s: %v", path, err)
	}
	fmt.Printf("Generated %s from PDF.js %s\n", path, pdfjsCommit)
}

// readSource returns standard_fonts.js from path, or from GitHub at
// [pdfjsCommit] when path is empty.
func readSource(path string) (string, error) {
	if path != "" {
		data, err := os.ReadFile(path) //nolint:gosec // the path is the developer's own command-line flag
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", path, err)
		}
		return string(data), nil
	}
	url := rawBaseURL + "/" + pdfjsCommit + "/" + sourcePath
	resp, err := httpClient.Get(url) //nolint:gosec // the URL is built from package constants
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", url, err)
	}
	if len(data) > maxSourceBytes {
		return "", fmt.Errorf("downloading %s: larger than %d bytes", url, maxSourceBytes)
	}
	return string(data), nil
}

// generate renders the gofmt-formatted Go file carrying every table of
// [tables], parsed from the standard_fonts.js source js.
func generate(js string) ([]byte, error) {
	m := copyrightRE.FindStringSubmatch(js)
	if m == nil {
		return nil, errNoCopyright
	}
	var buf bytes.Buffer
	buf.WriteString("// Code generated by cmd/gen-stdfonts; DO NOT EDIT.\n\n")
	buf.WriteString(comment(fmt.Sprintf("Derived from %s of PDF.js (%s), commit %s. "+
		"Copyright %s Mozilla Foundation, licensed under the Apache License, Version 2.0: "+
		"see LICENSE-pdfjs. The lookup tables are transcribed into Go maps with their values unchanged.",
		sourcePath, pdfjsRepo, pdfjsCommit, m[1])))
	buf.WriteString("\n")
	buf.WriteString("package stdfonts\n")
	for _, t := range tables {
		entries, err := parseLookupTable(js, t.JSName)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", t.JSName, err)
		}
		buf.WriteString("\n" + comment(t.Doc))
		fmt.Fprintf(&buf, "var %s = map[uint16]rune{\n", t.GoName)
		gids := make([]uint16, 0, len(entries))
		for gid := range entries {
			gids = append(gids, gid)
		}
		slices.Sort(gids)
		for _, gid := range gids {
			fmt.Fprintf(&buf, "\t%d: 0x%04X, // %s\n", gid, entries[gid], runeLabel(entries[gid]))
		}
		buf.WriteString("}\n")
	}
	for _, t := range nameTables {
		entries, err := parseNameTable(js, t.JSName, t.Flag)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", t.JSName, err)
		}
		buf.WriteString("\n" + comment(t.Doc))
		if t.Flag {
			fmt.Fprintf(&buf, "var %s = map[string]bool{\n", t.GoName)
		} else {
			fmt.Fprintf(&buf, "var %s = map[string]string{\n", t.GoName)
		}
		for _, name := range slices.Sorted(maps.Keys(entries)) {
			if t.Flag {
				fmt.Fprintf(&buf, "\t%q: true,\n", name)
			} else {
				fmt.Fprintf(&buf, "\t%q: %q,\n", name, entries[name])
			}
		}
		buf.WriteString("}\n")
	}
	code, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated source: %w", err)
	}
	return code, nil
}

// parseLookupTable returns the entries of the getLookupTableFactory body
// named fn in js. The body must hold only "t[gid] = unicode;" assignments,
// blank lines and line comments.
func parseLookupTable(js, fn string) (map[uint16]rune, error) {
	body, err := lookupTableBody(js, fn)
	if err != nil {
		return nil, err
	}
	entries := make(map[uint16]rune)
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		m := assignRE.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("%w: %q", errUnexpectedStmt, line)
		}
		gid, err := strconv.ParseUint(m[1], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("%w: glyph ID %s", errEntryRange, m[1])
		}
		cp, err := strconv.ParseUint(m[2], 10, 32)
		if err != nil || !utf8.ValidRune(rune(cp)) { //nolint:gosec // ParseUint bounds cp to 32 bits and ValidRune rejects anything past U+10FFFF
			return nil, fmt.Errorf("%w: code point %s", errEntryRange, m[2])
		}
		if _, dup := entries[uint16(gid)]; dup {
			return nil, fmt.Errorf("%w: glyph ID %d", errDuplicateEntry, gid)
		}
		entries[uint16(gid)] = rune(cp) //nolint:gosec // ParseUint bounds gid to 16 bits and ValidRune bounds cp
	}
	if len(entries) == 0 {
		return nil, errTableNotFound
	}
	return entries, nil
}

// parseNameTable returns the entries of the getLookupTableFactory body named
// fn in js, keyed by font name. The body must hold only `t.Name = "value";` or
// `t["Name"] = "value";` assignments (`= true;` when flag is set), blank lines
// and line comments. A flag table's values are empty.
func parseNameTable(js, fn string, flag bool) (map[string]string, error) {
	body, err := lookupTableBody(js, fn)
	if err != nil {
		return nil, err
	}
	entries := make(map[string]string)
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		m := nameAssignRE.FindStringSubmatch(line)
		if m == nil || flag != (m[4] != "") {
			return nil, fmt.Errorf("%w: %q", errUnexpectedStmt, line)
		}
		name := m[1] + m[2]
		if _, dup := entries[name]; dup {
			return nil, fmt.Errorf("%w: %q", errDuplicateName, name)
		}
		entries[name] = m[3]
	}
	if len(entries) == 0 {
		return nil, errTableNotFound
	}
	return entries, nil
}

// lookupTableBody returns the body of the getLookupTableFactory call named fn
// in js, between its opening brace and its closing "});".
func lookupTableBody(js, fn string) (string, error) {
	head := regexp.MustCompile(`const ` + regexp.QuoteMeta(fn) + ` = getLookupTableFactory\(\s*function \(t\) \{\n`)
	loc := head.FindStringIndex(js)
	if loc == nil {
		return "", errTableNotFound
	}
	body := js[loc[1]:]
	end := regexp.MustCompile(`\n\s*\}\s*\);`).FindStringIndex(body)
	if end == nil {
		return "", errTableNotFound
	}
	return body[:end[0]], nil
}

// commentWidth is the column the generated comments wrap at.
const commentWidth = 76

// comment renders text as "//" comment lines wrapped at [commentWidth].
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

// runeLabel names r for the generated comment: the character itself when it
// prints on its own left to right, its U+ form otherwise (controls, spaces,
// combining marks, format characters, right-to-left letters).
func runeLabel(r rune) string {
	if !unicode.IsGraphic(r) || unicode.IsSpace(r) || unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Hebrew, unicode.Arabic) {
		return fmt.Sprintf("U+%04X", r)
	}
	return string(r)
}

func findStdfontsDir() string {
	for _, c := range []string{".", "pkg/internal/typography/stdfonts"} {
		if _, err := os.Stat(filepath.Join(c, "lookup.go")); err == nil {
			return c
		}
	}
	fatalf("cannot find the stdfonts package directory; run from the repo root or pkg/internal/typography/stdfonts/")
	return ""
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-stdfonts: "+format+"\n", args...)
	os.Exit(1)
}
