package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// errInputsUnfetched reports a converter corpus whose sources are listed but
// not on disk: the corpus's make target has not run.
var errInputsUnfetched = errors.New("sources not provisioned")

// loadInputFixtures lists the fixtures of a converter corpus from its
// [InputSpec], in ID order, with the manifest's skips applied.
func loadInputFixtures(m *Manifest) ([]Fixture, error) {
	in := m.Inputs
	var (
		fixtures []Fixture
		err      error
	)
	switch {
	case in.List != "" && in.Glob == "":
		fixtures, err = listInputFixtures(filepath.Join(m.SourceDir(), in.List), m.TestdataRoot(), m.ProvisionedBy)
	case in.Glob != "" && in.List == "":
		fixtures, err = globInputFixtures(m.SourceDir(), in.Glob)
	default:
		return nil, errors.New("manifest inputs must set exactly one of list and glob")
	}
	if err != nil {
		return nil, err
	}
	for i := range fixtures {
		fixtures[i].SkipReason = m.Skips[fixtures[i].ID]
	}
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].ID < fixtures[j].ID })
	return fixtures, nil
}

// listInputFixtures reads a committed list of {"id": ...} objects; each
// fixture's source is the file at its ID under root. A listed source missing
// on disk fails the load with the make target that fetches it, provisionedBy,
// rather than scoring every fixture as an error.
func listInputFixtures(listPath, root, provisionedBy string) ([]Fixture, error) {
	data, err := os.ReadFile(listPath) //nolint:gosec // committed corpus list
	if err != nil {
		return nil, fmt.Errorf("read input list: %w", err)
	}
	var entries []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode input list %s: %w", listPath, err)
	}
	fixtures := make([]Fixture, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !validInputID(e.ID) {
			return nil, fmt.Errorf("input list %s: invalid id %q", listPath, e.ID)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("input list %s: duplicate id %q", listPath, e.ID)
		}
		seen[e.ID] = true
		p := filepath.Join(root, filepath.FromSlash(e.ID))
		if _, err := os.Stat(p); err != nil {
			remedy := "provision it"
			if provisionedBy != "" {
				remedy = "run " + provisionedBy
			}
			return nil, fmt.Errorf("%w: %s is missing (%s)", errInputsUnfetched, p, remedy)
		}
		fixtures = append(fixtures, Fixture{ID: e.ID, PDFPath: p})
	}
	return fixtures, nil
}

// validInputID reports whether id is a clean relative slash path that stays
// inside the directory it is joined to.
func validInputID(id string) bool {
	if id == "" || strings.HasPrefix(id, "/") || strings.Contains(id, `\`) {
		return false
	}
	for seg := range strings.SplitSeq(id, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// globInputFixtures matches pattern, which holds exactly one "*", under dir;
// each match is one fixture keyed on what the "*" matched.
func globInputFixtures(dir, pattern string) ([]Fixture, error) {
	prefix, suffix, ok := strings.Cut(pattern, "*")
	if !ok || strings.Contains(suffix, "*") || strings.ContainsAny(pattern, "?[") {
		return nil, fmt.Errorf("input glob %q must hold exactly one * and no other wildcard", pattern)
	}
	matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pattern)))
	if err != nil {
		return nil, fmt.Errorf("input glob %q: %w", pattern, err)
	}
	fixtures := make([]Fixture, 0, len(matches))
	for _, p := range matches {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil, fmt.Errorf("input glob %q: %w", pattern, err)
		}
		rel = filepath.ToSlash(rel)
		id := strings.TrimSuffix(strings.TrimPrefix(rel, prefix), suffix)
		if id == "" || strings.Contains(id, "/") {
			continue
		}
		fixtures = append(fixtures, Fixture{ID: id, PDFPath: p})
	}
	if len(fixtures) == 0 {
		return nil, fmt.Errorf("input glob %q matches nothing under %s", pattern, dir)
	}
	return fixtures, nil
}
