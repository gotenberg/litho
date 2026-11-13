package css

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// webrefDifferencesFile lists where litho's property table and the
// pinned webref data differ, each with its reason.
const webrefDifferencesFile = "testdata/webref-differences.tsv"

// webrefProperty is one property of webref's css.json.
type webrefProperty struct {
	Name          string `json:"name"`
	Initial       string `json:"initial"`
	Inherited     string `json:"inherited"`
	LegacyAliasOf string `json:"legacyAliasOf"`
}

// TestPropertyTableAgainstWebref holds litho's property table to the
// specifications' property definitions, as the pinned @webref/css data
// gathers them: every name litho parses or ignores by design is a
// property there or one of its legacy name aliases; every longhand is
// inherited when its definition says so; and a longhand's initial value
// as written there parses and computes as the initial keyword does.
// Every exception is listed in testdata/webref-differences.tsv with its
// clause, its Chromium measurement, or "open" for one still to decide;
// an entry that no longer holds fails too.
func TestPropertyTableAgainstWebref(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(specsDir + "/webref-css.json")
	if err != nil {
		t.Skipf("%s/webref-css.json is absent: run testdata/fetch-specs.sh", specsDir)
	}
	var ref struct {
		Properties []webrefProperty `json:"properties"`
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
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
