package css

import (
	"bufio"
	"html"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// specsDir holds the standards testdata/fetch-specs.sh downloads.
const specsDir = "testdata/specs"

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

// TestUserAgentSheet holds ua.css to the CSS the HTML Standard's Rendering
// section expects (15): every declaration the Standard gives an HTML
// document is in ua.css with the same value, and every one ua.css adds is
// the Standard's, unless testdata/ua-differences.tsv lists it with its
// reason. An entry the sheets no longer differ on fails too, so the list
// stays exact. The Standard is read from the copy testdata/fetch-specs.sh
// downloads at its pinned commit.
func TestUserAgentSheet(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(specsDir + "/html-source")
	if err != nil {
		t.Skipf("%s/html-source is absent: run testdata/fetch-specs.sh", specsDir)
	}
	spec := map[string]string{}
	for _, d := range renderingDecls(t, string(src)) {
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
	listed := readUADifferences(t)
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

// readUADifferences reads the listed differences: lines of a kind, a
// condition|selector|property key and a reason, tab-separated, and
// comment lines starting with #.
func readUADifferences(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open(uaDifferencesFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() // read-only file: Close reports nothing a read did not
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 || strings.TrimSpace(parts[2]) == "" {
			t.Fatalf("%s: %q is not a kind, a key and a reason", uaDifferencesFile, line)
		}
		out[parts[0]+"\t"+parts[1]] = parts[2]
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
