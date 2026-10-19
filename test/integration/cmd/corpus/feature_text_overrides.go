package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// textOverrideFile is the layout of a committed text override file (see
// [Manifest.GoldenOverrides]). Each entry corrects one page of a PDF.js
// golden.
type textOverrideFile struct {
	Overrides []textOverride `json:"overrides"`
}

// textOverride replaces the PDF.js reading of one page. PDFJS records the
// reading it replaces, whitespace-collapsed, so an override goes stale
// loudly when a new PDF.js reads the page otherwise. Exactly one of
// Expected and Unscored is set: Expected is the right reading, and
// Unscored drops a page on which no reading is right, such as codes ISO
// 32000-1, 9.10.2 gives no Unicode value and the two readers read
// differently. Reason states why PDF.js is wrong, or why no reading is
// right, citing the clause.
type textOverride struct {
	Fixture  string `json:"fixture"`
	Page     int    `json:"page"`
	PDFJS    string `json:"pdfjs"`
	Expected string `json:"expected,omitempty"`
	Unscored bool   `json:"unscored,omitempty"`
	Reason   string `json:"reason"`
}

type textOverrideKey struct {
	fixture string
	page    int
}

var (
	// errTextOverrideInvalid flags an entry that breaks the override
	// contract (see [textOverride]).
	errTextOverrideInvalid = errors.New("invalid text override")
	// errTextOverrideNoGolden flags an override for a page no golden holds.
	errTextOverrideNoGolden = errors.New("text override names no golden page")
)

// readTextOverrides parses and validates an override file. It checks every
// entry on its own; [textFeature.LoadOverrides] checks them against the
// goldens.
func readTextOverrides(path string) ([]textOverride, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from the committed corpus manifest
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var file textOverrideFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	seen := make(map[textOverrideKey]bool, len(file.Overrides))
	for i, o := range file.Overrides {
		key := textOverrideKey{o.Fixture, o.Page}
		switch {
		case o.Fixture == "" || o.Page < 1:
			return nil, fmt.Errorf("%w: entry %d names no fixture page", errTextOverrideInvalid, i)
		case seen[key]:
			return nil, fmt.Errorf("%w: %s page %d listed twice", errTextOverrideInvalid, o.Fixture, o.Page)
		case o.Reason == "":
			return nil, fmt.Errorf("%w: %s page %d gives no reason", errTextOverrideInvalid, o.Fixture, o.Page)
		case o.Unscored == (o.Expected != ""):
			return nil, fmt.Errorf("%w: %s page %d sets neither or both of expected and unscored", errTextOverrideInvalid, o.Fixture, o.Page)
		case !o.Unscored && normalizeWhitespace(o.Expected) == normalizeWhitespace(o.PDFJS):
			return nil, fmt.Errorf("%w: %s page %d expects the reading it replaces", errTextOverrideInvalid, o.Fixture, o.Page)
		}
		seen[key] = true
	}
	return file.Overrides, nil
}

// LoadOverrides indexes the committed corrections to the PDF.js goldens.
// An override for a page no golden holds fails the load: the goldens and
// the fixtures have drifted apart. One whose recorded PDF.js reading no
// longer matches the golden fails its fixture in [textFeature.Compare]
// instead, so the cell drops and the gate names it.
func (f *textFeature) LoadOverrides(path string) error {
	overrides, err := readTextOverrides(path)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range overrides {
		golden := f.goldens[o.Fixture]
		if golden == nil || !goldenHasPage(golden, o.Page) {
			return fmt.Errorf("%w: %s page %d", errTextOverrideNoGolden, o.Fixture, o.Page)
		}
		f.overrides[textOverrideKey{o.Fixture, o.Page}] = o
	}
	return nil
}

func goldenHasPage(golden *pdfjsReference, page int) bool {
	for _, p := range golden.Pages {
		if p.PageNumber == page {
			return true
		}
	}
	return false
}
