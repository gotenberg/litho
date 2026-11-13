package css

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

const (
	// webrefDifferencesFile lists where litho's property table and the
	// pinned webref data differ, each with its reason.
	webrefDifferencesFile = "testdata/webref-differences.tsv"
	// webrefFile holds the fields of each property of webref's css.json
	// that TestPropertyTableAgainstWebref reads.
	webrefFile = specsDir + "/webref-properties.json"
)

// webrefProperty is one property of webref's css.json, the fields
// TestPropertyTableAgainstWebref reads.
type webrefProperty struct {
	Name          string `json:"name"`
	Initial       string `json:"initial,omitempty"`
	Inherited     string `json:"inherited,omitempty"`
	LegacyAliasOf string `json:"legacyAliasOf,omitempty"`
	AnimationType string `json:"animationType,omitempty"`
}

// webrefExtract writes the properties of css.json in tgz, the @webref/css
// package at version, as a JSON object: where they come from, the notice
// of license, the LICENSE file of w3c/webref at licenseCommit, and the
// webrefProperty fields of every property, one a line, sorted by name.
func webrefExtract(t *testing.T, tgz, license []byte, version, licenseCommit string) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	var data []byte
	for data == nil {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			t.Fatal("the @webref/css package holds no package/css.json")
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Name == "package/css.json" {
			if data, err = io.ReadAll(tr); err != nil {
				t.Fatal(err)
			}
		}
	}
	var ref struct {
		Properties []webrefProperty `json:"properties"`
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	// Stable, so a name webref lists twice keeps its last definition last,
	// as TestPropertyTableAgainstWebref reads it.
	slices.SortStableFunc(ref.Properties, func(a, b webrefProperty) int { return strings.Compare(a.Name, b.Name) })
	sum := sha256.Sum256(tgz)
	header := []struct{ key, value string }{
		{"source", "package/css.json of @webref/css " + version + ", https://registry.npmjs.org/@webref/css/-/css-" + version + ".tgz"},
		{"sha256", hex.EncodeToString(sum[:])},
		{"made", "by TestSpecExtractsMatchDownloads from testdata/specs/download (testdata/fetch-specs.sh): do not edit"},
		{"license", "MIT, as the package.json of the package declares. The package carries no copyright or permission notice: the notice is the LICENSE file of https://github.com/w3c/webref at commit " + licenseCommit + "."},
	}
	var b bytes.Buffer
	b.WriteString("{\n")
	for _, h := range header {
		b.WriteString("  " + jsonString(t, h.key) + ": " + jsonString(t, h.value) + ",\n")
	}
	b.WriteString(`  "notice": [` + "\n")
	lines := strings.Split(strings.TrimRight(string(license), "\n"), "\n")
	for i, line := range lines {
		b.WriteString("    " + jsonString(t, line))
		if i < len(lines)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ],\n" + `  "properties": [` + "\n")
	for i, p := range ref.Properties {
		b.WriteString("    ")
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(p); err != nil {
			t.Fatal(err)
		}
		b.Truncate(b.Len() - 1) // Encode ends the value with a newline.
		if i < len(ref.Properties)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  ]\n}\n")
	return b.Bytes()
}

// jsonString writes s as a JSON string, with <, > and & as they are.
func jsonString(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// webrefAnimationTypes returns the animation type of each property of
// webrefFile.
func webrefAnimationTypes(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(webrefFile)
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		Properties []webrefProperty `json:"properties"`
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, p := range ref.Properties {
		out[p.Name] = p.AnimationType
	}
	return out
}

// TestPropertyTableAgainstWebref holds litho's property table to the
// specifications' property definitions, as the pinned @webref/css data
// gathers them: every name litho parses or ignores by design is a
// property there or one of its legacy name aliases; every longhand is
// inherited when its definition says so; and a longhand's initial value
// as written there parses and computes as the initial keyword does.
// Every exception is listed in testdata/webref-differences.tsv with its
// clause, its Chromium measurement, or "open" for one still to decide;
// an entry that no longer holds fails too. The webref data is read from
// webrefFile, extracted from a pinned version of the package.
func TestPropertyTableAgainstWebref(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(webrefFile)
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		Properties []webrefProperty `json:"properties"`
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	if len(ref.Properties) == 0 {
		t.Fatalf("%s holds no property", webrefFile)
	}
	byName := map[string]webrefProperty{}
	for _, p := range ref.Properties {
		byName[p.Name] = p
	}
	var diffs []string
	names := map[string]bool{}
	for n := range longhandByName {
		names[n] = true
	}
	for n := range shorthands {
		names[n] = true
	}
	for n := range silentProperties {
		names[n] = true
	}
	for n := range legacyAliases {
		names[n] = true
	}
	for n := range names {
		if _, ok := byName[n]; !ok {
			diffs = append(diffs, "unknown\t"+n)
		}
	}
	for i := range props {
		def := &props[i]
		p, ok := byName[def.name]
		if !ok {
			continue
		}
		switch {
		case p.Inherited == "yes" && !def.inherited, p.Inherited == "no" && def.inherited:
			diffs = append(diffs, "inherited\t"+def.name)
		}
		if p.Initial != "" && !initialComputesAsKeyword(t, def.name, p.Initial) {
			diffs = append(diffs, "initial\t"+def.name)
		}
	}
	slices.Sort(diffs)
	listed := readDifferences(t, webrefDifferencesFile, 3)
	for _, d := range diffs {
		if _, ok := listed[d]; !ok {
			t.Errorf("not in %s: %s", webrefDifferencesFile, d)
		}
		delete(listed, d)
	}
	for d := range listed {
		t.Errorf("listed in %s, but it holds: %s", webrefDifferencesFile, d)
	}
}

// initialComputesAsKeyword reports whether name: value parses and gives
// the style name: initial gives.
func initialComputesAsKeyword(t *testing.T, name, value string) bool {
	t.Helper()
	decls := ParseDeclarations(name+": "+value, ParseOptions{})
	if len(decls) != 1 || len(decls[0].entries) == 0 {
		return false
	}
	doc := `<body><div id=t></div></body>`
	got := compute(t, doc, `body { color: red } #t { `+name+`: `+value+` }`).of("t")
	want := compute(t, doc, `body { color: red } #t { `+name+`: initial }`).of("t")
	return equalStyle(got, want)
}

// readDifferences reads a list of differences: lines of n tab-separated
// fields, a kind, a key and, last, a reason, and comment lines starting
// with #. It returns the reason of each kind and key.
func readDifferences(t *testing.T, path string, n int) map[string]string {
	t.Helper()
	f, err := os.Open(path)
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
		if len(parts) != n || strings.TrimSpace(parts[n-1]) == "" {
			t.Fatalf("%s: %q is not %d fields ending with a reason", path, line, n)
		}
		out[strings.Join(parts[:n-1], "\t")] = parts[n-1]
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
