package css

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"html"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var updateSpecs = flag.Bool("update-css-specs", false, "rewrite the extracts of testdata/specs from testdata/specs/download")

const (
	// specsDir holds the extracts of the standards the tests of this
	// package read, each made from a pinned download.
	specsDir = "testdata/specs"
	// specsDownloadDir holds the downloads testdata/fetch-specs.sh makes,
	// which git ignores.
	specsDownloadDir = specsDir + "/download"
	// renderingFile holds the declarations of the CSS of the HTML
	// Standard's Rendering section.
	renderingFile = specsDir + "/html-rendering.tsv"
)

// uaDifferencesFile lists every declaration where ua.css and the CSS of
// the HTML Standard's Rendering section differ, each with its reason.
const uaDifferencesFile = "testdata/ua-differences.tsv"

// uaDecl is one declaration of a user-agent sheet: its selector, one of a
// rule's list, and its property, both normalised, and its value.
type uaDecl struct {
	key   string
	value string
}

var (
	cssComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cssBlock    = regexp.MustCompile(`(?s)<pre><code class="css">(.*?)</code></pre>`)
	htmlTag     = regexp.MustCompile(`<[^>]+>`)
	quotedValue = regexp.MustCompile(`\[\s*([^\]=~|^$*\s]+)\s*([~|^$*]?=)\s*"([^"]*)"\s*(i?)\s*\]`)
	combinator  = regexp.MustCompile(`\s*([>+~])\s*`)
)

// splitTop splits s at the separator sep outside parentheses and brackets.
func splitTop(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// normSelector writes a selector one way: white space collapsed, around
// combinators and inside parentheses, attribute values unquoted, lower
// case.
func normSelector(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = quotedValue.ReplaceAllStringFunc(s, func(m string) string {
		g := quotedValue.FindStringSubmatch(m)
		out := "[" + g[1] + g[2] + g[3]
		if g[4] != "" {
			out += " " + g[4]
		}
		return out + "]"
	})
	s = combinator.ReplaceAllString(s, " $1 ")
	s = strings.ReplaceAll(strings.ReplaceAll(s, "( ", "("), " )", ")")
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// sheetDecls reads the style rules of a sheet in its text, those inside
// @media and @supports keyed by their condition, the other at-rules
// skipped.
func sheetDecls(text, cond string) []uaDecl {
	text = cssComment.ReplaceAllString(text, "")
	var out []uaDecl
	for pos := 0; pos < len(text); {
		k := strings.IndexAny(text[pos:], "{;")
		if k < 0 {
			break
		}
		k += pos
		head := strings.TrimSpace(text[pos:k])
		if text[k] == ';' {
			pos = k + 1
			continue
		}
		depth, end := 0, len(text)
		for e := k; e < len(text); e++ {
			if text[e] == '{' {
				depth++
			} else if text[e] == '}' {
				if depth--; depth == 0 {
					end = e
					break
				}
			}
		}
		body := text[k+1 : min(end, len(text))]
		switch {
		case strings.HasPrefix(head, "@media"), strings.HasPrefix(head, "@supports"):
			out = append(out, sheetDecls(body, strings.TrimSpace(cond+" "+strings.Join(strings.Fields(head), " ")))...)
		case strings.HasPrefix(head, "@"):
		default:
			for _, sel := range splitTop(head, ',') {
				for _, d := range splitTop(body, ';') {
					p, v, ok := strings.Cut(d, ":")
					if !ok {
						continue
					}
					key := cond + "|" + normSelector(sel) + "|" + strings.ToLower(strings.TrimSpace(p))
					out = append(out, uaDecl{key: key, value: strings.ToLower(strings.Join(strings.Fields(v), " "))})
				}
			}
		}
		pos = end + 1
	}
	return out
}

// renderingDecls returns the declarations the HTML Standard's Rendering
// section expects of every HTML document: the CSS blocks of its section
// 15 but those introduced as presentational hints (litho implements them
// in hints.go), for quirks mode (litho has none), for ISO-8859-8
// documents, for select elements by appearance, and for the shadow tree of
// a details element's default summary.
func renderingDecls(t *testing.T, src string) []uaDecl {
	t.Helper()
	i := strings.Index(src, `<h2 split-filename="rendering" id="rendering">`)
	if i < 0 {
		t.Fatal("no Rendering section in the HTML Standard's source")
	}
	j := strings.Index(src[i+10:], "<h2 ")
	section := src[i : i+10+j]
	var out []uaDecl
	for _, m := range cssBlock.FindAllStringSubmatchIndex(section, -1) {
		before := section[:m[0]]
		k := max(strings.LastIndex(before, "</pre>"), strings.LastIndex(before, "</h3>"), strings.LastIndex(before, "</h4>"), strings.LastIndex(before, "</h5>"))
		intro := strings.Join(strings.Fields(htmlTag.ReplaceAllString(before[max(k, 0):], "")), " ")
		if strings.Contains(intro, "presentational hint") || strings.Contains(intro, "quirks mode") || strings.Contains(intro, "ISO-8859-8") ||
			strings.Contains(intro, "select elements when") || strings.Contains(intro, "default summary") {
			continue
		}
		out = append(out, sheetDecls(html.UnescapeString(htmlTag.ReplaceAllString(section[m[2]:m[3]], "")), "")...)
	}
	return out
}

// renderingExtract writes the declarations renderingDecls reads in src,
// the HTML Standard's source at commit, one a line in the Standard's
// order, as condition, selector, property and value separated by tabs,
// under a header that names the source and reproduces the notices of
// license, the Standard's LICENSE file at that commit.
func renderingExtract(t *testing.T, src, license []byte, commit string) []byte {
	t.Helper()
	// The LICENSE file is the notice, then the Creative Commons text, then
	// the BSD 3-Clause text, each pair separated by a line of four dashes.
	parts := strings.Split(string(license), "\n- - - -\n")
	if len(parts) < 3 || !strings.HasPrefix(strings.TrimSpace(parts[2]), "BSD 3-Clause License") {
		t.Fatal("the HTML Standard's LICENSE is not its notice, the Creative Commons text and the BSD 3-Clause text")
	}
	sum := sha256.Sum256(src)
	var b bytes.Buffer
	b.WriteString(commentLines(`The CSS of the HTML Standard's Rendering section (15) that TestUserAgentSheet
holds ua.css to: one declaration a line, as condition, selector, property
and value separated by tabs, in the Standard's order. The condition is
empty outside @media and @supports.

Source: https://github.com/whatwg/html, file source at commit
` + commit + `, SHA-256 ` + hex.EncodeToString(sum[:]) + `.
Made by TestSpecExtractsMatchDownloads from testdata/specs/download
(testdata/fetch-specs.sh): do not edit.

This file adapts the Standard: it keeps the declarations of the CSS blocks
of section 15 that apply to every HTML document, with comments removed,
white space collapsed, attribute values unquoted, and selectors,
properties and values in lower case.

The notices of the LICENSE file of whatwg/html at that commit, whose full
text, the Creative Commons Attribution 4.0 International Public License
included, is at https://github.com/whatwg/html/blob/` + commit + `/LICENSE:

` + strings.TrimSpace(parts[0]) + `

` + strings.TrimSpace(parts[2])))
	b.WriteString("\n")
	for _, d := range renderingDecls(t, string(src)) {
		// A selector may hold a bar; a condition and a property do not.
		cond, rest, _ := strings.Cut(d.key, "|")
		i := strings.LastIndexByte(rest, '|')
		sel, prop := rest[:i], rest[i+1:]
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", cond, sel, prop, d.value)
	}
	return b.Bytes()
}

// commentLines prefixes each line of text with "# ", or with "#" when the
// line is empty.
func commentLines(text string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		if line = strings.TrimRight(line, " "); line == "" {
			b.WriteString("#\n")
			continue
		}
		b.WriteString("# " + line + "\n")
	}
	return b.String()
}

// readRenderingDecls reads the declarations of renderingFile.
func readRenderingDecls(t *testing.T) []uaDecl {
	t.Helper()
	f, err := os.Open(renderingFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() // read-only file: Close reports nothing a read did not
	var out []uaDecl
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			t.Fatalf("%s: %q is not a condition, a selector, a property and a value", renderingFile, line)
		}
		out = append(out, uaDecl{key: fields[0] + "|" + fields[1] + "|" + fields[2], value: fields[3]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("%s holds no declaration", renderingFile)
	}
	return out
}

// TestUserAgentSheet holds ua.css to the CSS the HTML Standard's Rendering
// section expects (15): every declaration the Standard gives an HTML
// document is in ua.css with the same value, and every one ua.css adds is
// the Standard's, unless testdata/ua-differences.tsv lists it with its
// reason. An entry the sheets no longer differ on fails too, so the list
// stays exact. The Standard's declarations are read from renderingFile,
// extracted from the Standard at a pinned commit.
func TestUserAgentSheet(t *testing.T) {
	t.Parallel()
	spec := map[string]string{}
	for _, d := range readRenderingDecls(t) {
		spec[d.key] = d.value
	}
	ua := map[string]string{}
	for _, d := range sheetDecls(string(uaCSS), "") {
		ua[d.key] = d.value
	}
	var diffs []string
	for k, v := range spec {
		switch got, ok := ua[k]; {
		case !ok:
			diffs = append(diffs, "missing\t"+k)
		case got != v:
			diffs = append(diffs, "differs\t"+k)
		}
	}
	for k := range ua {
		if _, ok := spec[k]; !ok {
			diffs = append(diffs, "own\t"+k)
		}
	}
	slices.Sort(diffs)
	listed := readDifferences(t, uaDifferencesFile, 3)
	for _, d := range diffs {
		if _, ok := listed[d]; !ok {
			t.Errorf("not in %s: %s", uaDifferencesFile, d)
		}
		delete(listed, d)
	}
	for d := range listed {
		t.Errorf("listed in %s, but the sheets agree: %s", uaDifferencesFile, d)
	}
}

// TestSpecExtractsMatchDownloads holds the extracts of testdata/specs to
// the downloads they are made from: each is what the pinned download in
// testdata/specs/download gives. It skips when testdata/fetch-specs.sh has
// not run. After a pin moves, -update-css-specs rewrites the extracts.
func TestSpecExtractsMatchDownloads(t *testing.T) {
	t.Parallel()
	pinned, err := os.ReadFile(specsDownloadDir + "/.pinned")
	if err != nil {
		t.Skipf("%s is absent: run testdata/fetch-specs.sh", specsDownloadDir)
	}
	pins := strings.Fields(string(pinned))
	if len(pins) != 3 {
		t.Fatalf("%s/.pinned is %q, not a whatwg/html commit, a @webref/css version and a w3c/webref commit", specsDownloadDir, pinned)
	}
	read := func(name string) []byte {
		b, err := os.ReadFile(specsDownloadDir + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	extracts := []struct {
		path string
		data []byte
	}{
		{renderingFile, renderingExtract(t, read("html-source"), read("html-LICENSE"), pins[0])},
		{webrefFile, webrefExtract(t, read("webref-css.tgz"), read("webref-LICENSE"), pins[1], pins[2])},
	}
	for _, e := range extracts {
		if *updateSpecs {
			if err := os.WriteFile(e.path, e.data, 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(e.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, e.data) {
			t.Errorf("%s differs from what the pinned download gives: rerun with -update-css-specs and review the diff", e.path)
		}
	}
}
