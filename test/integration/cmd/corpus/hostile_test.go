package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// cheapHostile reports whether a hostile document builds in milliseconds:
// the bombs and floods inflate or write hundreds of megabytes, minutes under
// -race, and `corpus build` runs them at every corpus prepare.
func cheapHostile(name string) bool {
	return !strings.Contains(name, "bomb") && !strings.Contains(name, "flood") && name != "hostile-long-word"
}

// TestHostileDocsBuild checks every hostile document of each build corpus
// has a unique valid fixture ID, and builds the cheap ones from the
// corpus's committed base twice: each builds, to the same bytes.
func TestHostileDocsBuild(t *testing.T) {
	t.Chdir(filepath.Join("..", "..", "..", ".."))
	for _, format := range []string{"docx", "xlsx", "pptx"} {
		base, err := readParts(filepath.Join(corpusRoot(), "litho", format, "parts", hostileBase))
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		seen := map[string]bool{}
		for _, h := range hostileDocs(format) {
			if seen[h.name] || !validInputID(h.name) || !strings.HasPrefix(h.name, "hostile-") {
				t.Errorf("%s: hostile document %q is not a unique hostile- fixture ID", format, h.name)
			}
			seen[h.name] = true
			if !cheapHostile(h.name) {
				continue
			}
			a, err := h.build(base)
			if err != nil {
				t.Errorf("%s/%s: %v", format, h.name, err)
				continue
			}
			b, err := h.build(base)
			if err != nil || string(a) != string(b) {
				t.Errorf("%s/%s: a second build differs (%v)", format, h.name, err)
			}
		}
	}
}

// TestRobustnessExpectationsNameFixtures checks each build corpus's
// robustness.json: it names fixtures the corpus has, expects ends a
// conversion can have, and says how every hostile document must end.
func TestRobustnessExpectationsNameFixtures(t *testing.T) {
	t.Chdir(filepath.Join("..", "..", "..", ".."))

	results := map[string]bool{"pdf": true, "any": true}
	for _, s := range convertSentinels {
		if s.name != "panic" && s.name != "timeout" {
			results[s.name] = true
		}
	}
	all, err := manifests()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if !isBuildCorpus(m) {
			continue
		}
		names, err := builtNames(m)
		if err != nil {
			t.Fatalf("%s: %v", m.Name, err)
		}
		data, err := os.ReadFile(filepath.Join(m.SourceDir(), robustnessFile))
		if err != nil {
			t.Fatalf("%s: %v", m.Name, err)
		}
		var expect map[string]robustnessExpectation
		if err := json.Unmarshal(data, &expect); err != nil {
			t.Fatalf("%s: %v", m.Name, err)
		}
		for id, e := range expect {
			if !slices.Contains(names, id) {
				t.Errorf("%s: %s expects %s, which is no fixture", m.Name, robustnessFile, id)
			}
			if !results[e.Result] {
				t.Errorf("%s: %s expects %s to end %q, which no conversion does", m.Name, robustnessFile, id, e.Result)
			}
			if e.Why == "" {
				t.Errorf("%s: %s gives no reason for %s", m.Name, robustnessFile, id)
			}
		}
		for _, n := range names {
			if _, ok := expect[n]; strings.HasPrefix(n, "hostile-") && !ok {
				t.Errorf("%s: %s says nothing of %s", m.Name, robustnessFile, n)
			}
		}
	}
}
