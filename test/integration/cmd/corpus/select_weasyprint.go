package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The weasyprint corpus's fixtures: the draw tests of the WeasyPrint release
// versions.env pins, read from its Python sources.

// maxWeasyPrintResourceBytes caps the size of a resource copied into git:
// 256 KiB. Only the CMYK profile and the JPEG embedding it exceed it, and the
// fixtures naming them, colour-managed images, are left out and counted.
const maxWeasyPrintResourceBytes = 256 * 1024

// maxWeasyPrintTreeBytes bounds one file read from the release tarball.
// Anti-DoS guard for test tooling: the largest file of the release is a few
// megabytes.
const maxWeasyPrintTreeBytes = 64 << 20

// weasyPrintFontFiles are the test font's files: weasyprint.otf is the face
// the test UA stylesheet declares, the others the same glyphs in other
// formats. A fixture naming one does not copy it: the font goes to the
// fixture fonts.
var weasyPrintFontFiles = map[string]bool{"weasyprint.otf": true, "weasyprint.otb": true, "weasyprint.woff": true} //nolint:gochecknoglobals // static set

// selectWeasyPrint writes the weasyprint corpus's fixtures from the
// WeasyPrint release versions.env pins (WEASYPRINT_VERSION), downloaded and
// checked against WEASYPRINT_SHA256 unless checkout names an unpacked one.
//
// For every assert_pixels(expected, html) call of tests/draw/test_*.py whose
// two arguments are literal strings, it writes one fixture,
// fixtures/<module>__<test>.json: the test name, the HTML, the expected
// pixel rows, and the colour each pixel character stands for
// (PIXELS_BY_CHAR of tests/draw/__init__.py, read from the release). An
// argument is literal when it is a string constant, possibly implicitly
// concatenated, or a name bound to one: at module level, or in the test
// function itself and never rebound. Anything else (an f-string, a
// parameter of a parametrized test, a call) is computed, and the call is
// skipped and counted. A test calling assert_pixels more than once yields
// one fixture per call, suffixed _2, _3. A test WeasyPrint marks xfail keeps
// the mark.
//
// It copies the files of tests/resources the fixtures name and the test UA
// stylesheet into resources/, the release's LICENSE beside it, and the test
// font into the fixture fonts, checked against WEASYPRINT_FONT_SHA256.
func selectWeasyPrint(ctx context.Context, checkout, _, dir string, pins map[string]string) error {
	tree, err := weasyPrintTree(ctx, checkout, pins)
	if err != nil {
		return err
	}
	pixels, err := weasyPrintPixels(tree["tests/draw/__init__.py"])
	if err != nil {
		return err
	}
	fixturesDir := filepath.Join(dir, "fixtures")
	if err := os.RemoveAll(fixturesDir); err != nil {
		return err
	}
	if err := os.MkdirAll(fixturesDir, 0o755); err != nil {
		return err
	}

	var modules []string
	for name := range tree {
		if d, base := path.Split(name); d == "tests/draw/" && strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
			modules = append(modules, base)
		}
	}
	sort.Strings(modules)
	fixtures := map[string]weasyPrintFixture{}
	var skipped, badGrid []string
	for _, module := range modules {
		found, skip, bad, err := weasyPrintModule(module, tree["tests/draw/"+module], pixels)
		if err != nil {
			return fmt.Errorf("%s: %w", module, err)
		}
		for _, f := range found {
			fixtures[f.Name] = f
		}
		skipped, badGrid = append(skipped, skip...), append(badGrid, bad...)
	}

	resources := map[string][]byte{}
	for name, data := range tree {
		if rel, ok := strings.CutPrefix(name, "tests/resources/"); ok {
			resources[rel] = data
		}
	}
	names := make([]string, 0, len(fixtures))
	for n := range fixtures {
		names = append(names, n)
	}
	sort.Strings(names)
	used := map[string]bool{"tests_ua.css": true}
	var oversized []string
	written := 0
	for _, n := range names {
		f := fixtures[n]
		var named []string
		large := false
		for rel, data := range resources {
			if strings.Contains(f.HTML, rel) && !weasyPrintFontFiles[rel] {
				named = append(named, rel)
				large = large || len(data) > maxWeasyPrintResourceBytes
			}
		}
		if large {
			oversized = append(oversized, n)
			continue
		}
		for _, rel := range named {
			used[rel] = true
		}
		if err := os.WriteFile(filepath.Join(fixturesDir, n+".json"), f.json(), 0o644); err != nil { //nolint:gosec // committed fixtures
			return err
		}
		written++
	}

	resourcesDir := filepath.Join(dir, "resources")
	if err := os.RemoveAll(resourcesDir); err != nil {
		return err
	}
	for rel := range used {
		data, ok := resources[rel]
		if !ok {
			return fmt.Errorf("tests/resources/%s is missing from the release", rel)
		}
		dst := filepath.Join(resourcesDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil { //nolint:gosec // committed resources
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "LICENSE"), tree["LICENSE"], 0o644); err != nil { //nolint:gosec // committed licence
		return err
	}
	font := resources["weasyprint.otf"]
	sum := sha256.Sum256(font)
	if got, want := hex.EncodeToString(sum[:]), pins["WEASYPRINT_FONT_SHA256"]; got != want {
		return fmt.Errorf("the WeasyPrint test font hashes to %s, WEASYPRINT_FONT_SHA256 pins %s", got, want)
	}
	fonts := filepath.Join(testdataDir(), fixtureFontsDir, "weasyprint")
	if err := os.MkdirAll(fonts, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(fonts, "weasyprint.otf"), font, 0o644); err != nil { //nolint:gosec // fixture font
		return err
	}

	fmt.Printf("  weasyprint: %d fixtures written, %d calls skipped (computed grid or HTML), %d naming a resource over %d bytes, %d with an unparsable grid\n",
		written, len(skipped), len(oversized), maxWeasyPrintResourceBytes, len(badGrid))
	for _, t := range skipped {
		fmt.Println("    skipped " + t)
	}
	for _, t := range oversized {
		fmt.Println("    oversized resource " + t)
	}
	for _, t := range badGrid {
		fmt.Println("    bad grid " + t)
	}
	return nil
}

// weasyPrintTree returns the files of the release, by their path under its
// top directory: the unpacked checkout, or the pinned tarball.
func weasyPrintTree(ctx context.Context, checkout string, pins map[string]string) (map[string][]byte, error) {
	tree := map[string][]byte{}
	keep := func(name string) bool {
		return name == "LICENSE" || strings.HasPrefix(name, "tests/draw/") || strings.HasPrefix(name, "tests/resources/")
	}
	if checkout != "" {
		return tree, walkFiles(checkout, func(p string) error {
			rel, err := filepath.Rel(checkout, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if !keep(rel) {
				return nil
			}
			data, err := os.ReadFile(p) //nolint:gosec // the release checkout
			tree[rel] = data
			return err
		})
	}
	version := pins["WEASYPRINT_VERSION"]
	body, err := fetchVerified(ctx, http.DefaultClient, "https://github.com/Kozea/WeasyPrint/archive/refs/tags/v"+version+".tar.gz", pins["WEASYPRINT_SHA256"], "WEASYPRINT_SHA256")
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		_, rel, ok := strings.Cut(h.Name, "/")
		if h.Typeflag != tar.TypeReg || !ok || !keep(rel) {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxWeasyPrintTreeBytes))
		if err != nil {
			return nil, err
		}
		tree[rel] = data
	}
	if tree["tests/draw/__init__.py"] == nil {
		return nil, errors.New("the release has no tests/draw/__init__.py")
	}
	return tree, nil
}

// weasyPrintFixture is one fixture file.
type weasyPrintFixture struct {
	Name     string
	Source   string
	XFail    bool
	HTML     string
	Expected []string
	Pixels   map[string][]int
}

// json writes the fixture as Python's json.dump(indent=2, sort_keys=True,
// ensure_ascii=False) does, the format the corpus has always had.
func (f weasyPrintFixture) json() []byte {
	var b strings.Builder
	b.WriteString("{\n  \"expected\": [")
	for i, r := range f.Expected {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("\n    " + pyJSONString(r))
	}
	if len(f.Expected) > 0 {
		b.WriteString("\n  ")
	}
	b.WriteString("],\n  \"html\": " + pyJSONString(f.HTML))
	b.WriteString(",\n  \"name\": " + pyJSONString(f.Name))
	b.WriteString(",\n  \"pixels\": {")
	keys := make([]string, 0, len(f.Pixels))
	for k := range f.Pixels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("\n    " + pyJSONString(k) + ": ")
		v := f.Pixels[k]
		if v == nil {
			b.WriteString("null")
			continue
		}
		b.WriteByte('[')
		for j, n := range v {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString("\n      " + strconv.Itoa(n))
		}
		if len(v) > 0 {
			b.WriteString("\n    ")
		}
		b.WriteByte(']')
	}
	if len(keys) > 0 {
		b.WriteString("\n  ")
	}
	b.WriteString("},\n  \"source\": " + pyJSONString(f.Source))
	b.WriteString(",\n  \"xfail\": " + strconv.FormatBool(f.XFail) + "\n}\n")
	return []byte(b.String())
}

// pyJSONString quotes s as Python's json module does with ensure_ascii off.
func pyJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// weasyPrintPixels reads PIXELS_BY_CHAR, a dict literal of one-character
// strings to RGB tuples or None, from tests/draw/__init__.py.
func weasyPrintPixels(src []byte) (map[string][]int, error) {
	stmts, err := pyParse(src)
	if err != nil {
		return nil, err
	}
	for _, s := range stmts {
		t := s.tokens
		if len(t) < 3 || t[0].kind != pyName || t[0].text != "PIXELS_BY_CHAR" || !t[1].is("=") {
			continue
		}
		return pyPixelDict(t[2:])
	}
	return nil, errors.New("PIXELS_BY_CHAR not found in tests/draw/__init__.py")
}

// pyPixelDict reads {'c': (r, g, b), 'z': None, ...}.
func pyPixelDict(t []pyToken) (map[string][]int, error) {
	bad := errors.New("PIXELS_BY_CHAR is not a dict of strings to tuples of integers or None")
	if len(t) < 2 || !t[0].is("{") || !t[len(t)-1].is("}") {
		return nil, bad
	}
	out := map[string][]int{}
	t = t[1 : len(t)-1]
	for len(t) > 0 {
		if len(t) < 3 || t[0].kind != pyString || !t[1].is(":") {
			return nil, bad
		}
		key, ok := t[0].str()
		if !ok {
			return nil, bad
		}
		t = t[2:]
		switch {
		case t[0].kind == pyName && t[0].text == "None":
			out[key] = nil
			t = t[1:]
		case t[0].is("(") || t[0].is("["):
			closing := ")"
			if t[0].is("[") {
				closing = "]"
			}
			vals := []int{}
			i := 1
			for ; i < len(t) && !t[i].is(closing); i++ {
				if t[i].is(",") {
					continue
				}
				n, err := strconv.Atoi(t[i].text)
				if err != nil || t[i].kind != pyNumber {
					return nil, bad
				}
				vals = append(vals, n)
			}
			if i == len(t) {
				return nil, bad
			}
			out[key] = vals
			t = t[i+1:]
		default:
			return nil, bad
		}
		if len(t) > 0 {
			if !t[0].is(",") {
				return nil, bad
			}
			t = t[1:]
		}
	}
	return out, nil
}

// weasyPrintModule returns the fixtures of one test module, and the IDs of
// the calls it skipped and of those whose grid is unparsable.
func weasyPrintModule(module string, src []byte, pixels map[string][]int) (found []weasyPrintFixture, skipped, badGrid []string, err error) {
	stmts, err := pyParse(src)
	if err != nil {
		return nil, nil, nil, err
	}
	moduleNames := pyStringBindings(stmts)
	for _, fn := range stmts {
		name, params, ok := fn.testFunction()
		if !ok {
			continue
		}
		local := map[string]string{}
		for k, v := range moduleNames {
			local[k] = v
		}
		for k, v := range pyStringBindings(fn.body) {
			local[k] = v
		}
		for _, p := range params {
			delete(local, p)
		}
		for i, call := range fn.calls("assert_pixels") {
			id := strings.TrimSuffix(module, ".py") + "__" + name
			if i > 0 {
				id += "_" + strconv.Itoa(i+1)
			}
			args, keywords := call.args()
			var expected, html string
			okExpected, okHTML := false, false
			if len(args) == 2 {
				expected, okExpected = pyLiteral(args[0], local)
				html, okHTML = pyLiteral(args[1], local)
			}
			if !okExpected || !okHTML || keywords {
				skipped = append(skipped, id)
				continue
			}
			grid := pixelRows(expected)
			if !validGrid(grid, pixels) {
				badGrid = append(badGrid, id)
				continue
			}
			found = append(found, weasyPrintFixture{
				Name: id, Source: "tests/draw/" + module + "::" + name, XFail: fn.xfail(),
				HTML: html, Expected: grid, Pixels: pixels,
			})
		}
	}
	return found, skipped, badGrid, nil
}

// pixelRows parses an expected grid as tests/draw/__init__.py's
// parse_pixels does: each line up to a "#", stripped, the empty ones left
// out.
func pixelRows(expected string) []string {
	var rows []string
	for _, line := range pySplitLines(expected) {
		line, _, _ = strings.Cut(line, "#")
		if line = strings.TrimFunc(line, unicode.IsSpace); line != "" {
			rows = append(rows, line)
		}
	}
	return rows
}

// pySplitLines splits s at the line boundaries Python's str.splitlines
// recognises.
func pySplitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			lines = append(lines, s[start:i])
			if r == '\r' && strings.HasPrefix(s[i+n:], "\n") {
				n++
			}
			start = i + n
		}
		i += n
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// validGrid reports whether the rows are one length and every character is
// a pixel.
func validGrid(rows []string, pixels map[string][]int) bool {
	if len(rows) == 0 {
		return false
	}
	width := utf8.RuneCountInString(rows[0])
	for _, r := range rows {
		if utf8.RuneCountInString(r) != width {
			return false
		}
		for _, c := range r {
			if _, ok := pixels[string(c)]; !ok {
				return false
			}
		}
	}
	return true
}
