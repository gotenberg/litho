package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The specification the styles come from: CSS Counter Styles 3's source
// at a commit of w3c/csswg-drafts, and its digest.
const (
	srcURL    = "https://raw.githubusercontent.com/w3c/csswg-drafts/1d11d8e3c7b19f726a5dd4d43d02ec2c6aa17e89/css-counter-styles-3/Overview.bs"
	srcSHA256 = "4097d146eea07ebb3629afb3de181d060c72b266d3f799de4831c67951943037"
	outGoName = "counterstyles_gen.go"
	// stylesCount is the number of rules the generator writes.
	stylesCount = 49
)

// maxSrcBytes caps the downloaded file. Anti-DoS guard for a build-time
// tool, not a format limit: the file is 160,233 bytes.
const maxSrcBytes = 8 << 20

// httpClient bounds how long the generator waits for the download.
var httpClient = &http.Client{Timeout: 5 * time.Minute}

// crNames maps the names the draft gives the longhand East Asian styles to
// those of the Candidate Recommendation (CSS Counter Styles 3, 2021),
// which Chromium parses; the draft keeps them as legacy aliases.
var crNames = map[string]string{
	"japanese":      "japanese-informal",
	"korean-hangul": "korean-hangul-formal",
	"korean-hanja":  "korean-hanja-informal",
	"simp-chinese":  "simp-chinese-informal",
	"trad-chinese":  "trad-chinese-informal",
}

// draftOnly are the styles only the draft defines, which Chromium does
// not parse: they are left out.
var draftOnly = map[string]bool{"nko-ordinal": true}

// license is the W3C Software and Document License's notice, which the
// generated file carries (https://www.w3.org/copyright/software-license-2023/).
const license = `This software or document includes material copied from or derived
from CSS Counter Styles Level 3 (https://drafts.csswg.org/css-counter-styles-3/).
Copyright © 2026 World Wide Web Consortium.
https://www.w3.org/copyright/software-license-2023/

By obtaining and/or copying this work, you (the licensee) agree that you
have read, understood, and will comply with the following terms and
conditions.

Permission to copy, modify, and distribute this work, with or without
modification, for any purpose and without fee or royalty is hereby
granted, provided that you include the following on ALL copies of the
work or portions thereof, including modifications:

- The full text of this NOTICE in a location viewable to users of the
  redistributed or derivative work.
- Any pre-existing intellectual property disclaimers, notices, or terms
  and conditions. If none exist, the W3C software and document short
  notice should be included.
- Notice of any changes or modifications, through a copyright statement
  on the new code or document such as "This software or document
  includes material copied from or derived from [title and URI of the W3C
  document]. Copyright © [$year-of-document] World Wide Web Consortium.
  https://www.w3.org/copyright/software-license-2023/"

THIS WORK IS PROVIDED "AS IS," AND COPYRIGHT HOLDERS MAKE NO
REPRESENTATIONS OR WARRANTIES, EXPRESS OR IMPLIED, INCLUDING BUT NOT
LIMITED TO, WARRANTIES OF MERCHANTABILITY OR FITNESS FOR ANY PARTICULAR
PURPOSE OR THAT THE USE OF THE SOFTWARE OR DOCUMENT WILL NOT INFRINGE ANY
THIRD PARTY PATENTS, COPYRIGHTS, TRADEMARKS OR OTHER RIGHTS.

COPYRIGHT HOLDERS WILL NOT BE LIABLE FOR ANY DIRECT, INDIRECT, SPECIAL OR
CONSEQUENTIAL DAMAGES ARISING OUT OF ANY USE OF THE SOFTWARE OR DOCUMENT.

The name and trademarks of copyright holders may NOT be used in
advertising or publicity pertaining to the work without specific,
written prior permission. Title to copyright in this work will at all
times remain with copyright holders.

Changes: the style sheet blocks of sections 6 and 7 are joined into one
style sheet, comments and notes removed, the longhand East Asian styles
renamed to the Candidate Recommendation's names, nko-ordinal left out;
the character tables of 7.1.5 are written as lists.`

func main() {
	src := flag.String("src", "", "local copy of Overview.bs, instead of downloading it")
	out := flag.String("out", outGoName, "Go file to write")
	flag.Parse()
	if err := run(*src, *out); err != nil {
		fmt.Fprintln(os.Stderr, "gen-counter-styles:", err)
		os.Exit(1)
	}
}

// run reads the specification and writes the Go file.
func run(src, out string) error {
	data, err := readSrc(src)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != srcSHA256 {
		return fmt.Errorf("source digest %s, want %s", got, srcSHA256)
	}
	sheet, names, err := styleSheet(string(data))
	if err != nil {
		return err
	}
	if len(names) != stylesCount {
		return fmt.Errorf("%d styles, want %d: %v", len(names), stylesCount, names)
	}
	tables, err := cjkTables(string(data))
	if err != nil {
		return err
	}
	code, err := generate(sheet, tables)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Clean(out), code, 0o600)
}

// readSrc returns the source from src, or downloaded.
func readSrc(src string) ([]byte, error) {
	if src != "" {
		return os.ReadFile(filepath.Clean(src))
	}
	resp, err := httpClient.Get(srcURL)
	if err != nil {
		return nil, fmt.Errorf("downloading the specification: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // a read-only response body has nothing to flush
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading the specification: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSrcBytes+1))
	if err != nil {
		return nil, fmt.Errorf("downloading the specification: %w", err)
	}
	if len(data) > maxSrcBytes {
		return nil, errors.New("the specification is larger than expected")
	}
	return data, nil
}

var (
	stylesheetBlock = regexp.MustCompile(`(?s)<pre class=["']?stylesheet["']?>(.*?)</pre>`)
	counterRule     = regexp.MustCompile(`(?s)@counter-style\s+([\w-]+)\s*\{(.*?)\}`)
	cssComment      = regexp.MustCompile(`(?s)/\*.*?\*/`)
	htmlTag         = regexp.MustCompile(`<[^>]+>`)
	// trailing is the prose a block puts after a declaration on its
	// line, as nko-ordinal's suffix does.
	trailing = regexp.MustCompile(`;[^;\n]*\n`)
)

// section returns the text of s between the headings named from and to.
func section(s, from, to string) (string, error) {
	i := strings.Index(s, from)
	j := strings.Index(s, to)
	if i < 0 || j < i {
		return "", fmt.Errorf("no section between %q and %q", from, to)
	}
	return s[i:j], nil
}

// styleSheet returns the @counter-style rules of the style sheet blocks of
// sections 6 and 7, one per line in source order, and their names.
func styleSheet(s string) (string, []string, error) {
	sec, err := section(s, "<h2 id='predefined-counters'>", "<h2 id='additional-predefined'>")
	if err != nil {
		return "", nil, err
	}
	var b strings.Builder
	var names []string
	for _, m := range stylesheetBlock.FindAllStringSubmatch(sec, -1) {
		block := html.UnescapeString(htmlTag.ReplaceAllString(m[1], ""))
		block = cssComment.ReplaceAllString(block, "")
		for _, r := range counterRule.FindAllStringSubmatch(block, -1) {
			name := r[1]
			if draftOnly[name] {
				continue
			}
			if cr, ok := crNames[name]; ok {
				name = cr
			}
			if slices.Contains(names, name) {
				return "", nil, fmt.Errorf("style %s defined twice", name)
			}
			names = append(names, name)
			body := trailing.ReplaceAllString(r[2]+"\n", ";\n")
			var decls []string
			for d := range strings.SplitSeq(body, ";") {
				if d = strings.Join(strings.Fields(d), " "); d != "" {
					decls = append(decls, quoteSymbols(d))
				}
			}
			fmt.Fprintf(&b, "@counter-style %s { %s; }\n", name, strings.Join(decls, "; "))
		}
	}
	return b.String(), names, nil
}

// quoteSymbols writes each symbol of a symbols declaration d as a string.
// The specification writes some unquoted, as escapes separated by spaces
// ("\3007 \4E00"), which CSS Syntax 3, 4.3.7 reads as one identifier: a
// space after a hex escape belongs to the escape.
func quoteSymbols(d string) string {
	name, value, ok := strings.Cut(d, ":")
	if !ok || strings.TrimSpace(name) != "symbols" {
		return d
	}
	fields := strings.Fields(value)
	for i, f := range fields {
		if !strings.HasPrefix(f, `"`) && !strings.HasPrefix(f, "'") {
			fields[i] = `"` + f + `"`
		}
	}
	return name + ": " + strings.Join(fields, " ")
}

// cjkTable is one style's characters of 7.1.5: the ten digits, the second
// to fourth digit markers, the second to fourth group markers and the
// negative sign.
type cjkTable struct {
	name  string
	chars []string
}

// tableRows are the rows each table of 7.1.5 holds, in order.
var tableRows = []string{
	"Digit 0", "Digit 1", "Digit 2", "Digit 3", "Digit 4", "Digit 5", "Digit 6", "Digit 7", "Digit 8", "Digit 9",
	"Second Digit Marker", "Third Digit Marker", "Fourth Digit Marker",
	"Second Group Marker", "Third Group Marker", "Fourth Group Marker", "Negative Sign",
}

var (
	dataTable = regexp.MustCompile(`(?s)<table class="data">(.*?)</table>`)
	tableRow  = regexp.MustCompile(`<tr>`)
	tableCell = regexp.MustCompile(`<t[hd][^>]*>`)
	codepoint = regexp.MustCompile(`U\+([0-9A-Fa-f]{4,5})`)
)

// cjkTables returns the character tables of 7.1.5, each column a style,
// a character given by its code points, a cell spanning the columns
// shared by each.
func cjkTables(s string) ([]cjkTable, error) {
	sec, err := section(s, "<h4 id='extended-range-optional'>", "<h3 id='ethiopic-numeric-counter-style'>")
	if err != nil {
		return nil, err
	}
	var out []cjkTable
	for _, t := range dataTable.FindAllStringSubmatch(sec, -1) {
		var styles []string
		rows := map[string][]string{}
		for _, row := range tableRow.Split(t[1], -1) {
			var cells []string
			for _, c := range tableCell.Split(row, -1) {
				if c = strings.TrimSpace(htmlTag.ReplaceAllString(c, "")); c != "" {
					cells = append(cells, c)
				}
			}
			if len(cells) == 0 || cells[0] == "Values" {
				continue
			}
			if styles == nil {
				styles = cells
				continue
			}
			rows[cells[0]] = cells[1:]
		}
		tables := make([]cjkTable, len(styles))
		for i, name := range styles {
			if cr, ok := crNames[name]; ok {
				name = cr
			}
			tables[i].name = name
		}
		for _, label := range tableRows {
			cells, ok := rows[label]
			if !ok {
				return nil, fmt.Errorf("table of %v has no row %q", styles, label)
			}
			for i := range tables {
				cell := cells[min(i, len(cells)-1)]
				var ch strings.Builder
				for _, cp := range codepoint.FindAllStringSubmatch(cell, -1) {
					var r rune
					if _, err := fmt.Sscanf(cp[1], "%x", &r); err != nil {
						return nil, fmt.Errorf("code point %s: %w", cp[1], err)
					}
					ch.WriteRune(r)
				}
				if ch.Len() == 0 {
					return nil, fmt.Errorf("row %q of %s holds no code point", label, tables[i].name)
				}
				tables[i].chars = append(tables[i].chars, ch.String())
			}
		}
		out = append(out, tables...)
	}
	if len(out) != 9 {
		return nil, fmt.Errorf("%d character tables, want 9", len(out))
	}
	return out, nil
}

// generate writes the Go file.
func generate(sheet string, tables []cjkTable) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("// Code generated by gen-counter-styles; DO NOT EDIT.\n\n")
	for line := range strings.SplitSeq(license, "\n") {
		b.WriteString(strings.TrimRight("// "+line, " ") + "\n")
	}
	b.WriteString("\npackage css\n\n")
	b.WriteString("// predefinedCounterStyleSheet holds the @counter-style rules of CSS\n")
	b.WriteString("// Counter Styles 3, sections 6 and 7.\n")
	b.WriteString("const predefinedCounterStyleSheet = ")
	for i, line := range strings.SplitAfter(strings.TrimSuffix(sheet, "\n"), "\n") {
		if i > 0 {
			b.WriteString(" +\n\t")
		}
		fmt.Fprintf(&b, "%q", line)
	}
	b.WriteString("\n\n")
	b.WriteString("// cjkCharacters holds the characters of the longhand East Asian styles\n")
	b.WriteString("// (CSS Counter Styles 3, 7.1.5): the ten digits, the second to fourth\n")
	b.WriteString("// digit markers, the second to fourth group markers and the negative\n")
	b.WriteString("// sign. Immutable lookup table.\n")
	b.WriteString("var cjkCharacters = map[string][]string{\n")
	for _, t := range tables {
		fmt.Fprintf(&b, "\t%q: {", t.name)
		for i, c := range t.chars {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", c)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}
