package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestGoldenOverrideFilesAreWellFormed pins every override file a corpus
// manifest declares: it parses, its feature takes overrides, and each entry
// names a fixture of its corpus. The runner reports a broken file only as a load
// error, which the gate reads as a missing cell rather than a regression.
func TestGoldenOverrideFilesAreWellFormed(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "corpus")
	manifests, err := filepath.Glob(filepath.Join(root, "*", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, path := range manifests {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for feature, rel := range m.GoldenOverrides {
			if _, ok := m.Goldens[feature]; !ok {
				t.Errorf("%s: overrides %s, which has no golden", path, feature)
			}
			if !takesOverrides(feature) {
				t.Errorf("%s: overrides %s, which takes none", path, feature)
				continue
			}
			overrides, err := readTextOverrides(filepath.Join(filepath.Dir(path), rel))
			if err != nil {
				t.Errorf("%s: %v", path, err)
				continue
			}
			pdfDir, ok := overridePDFDir(path, m)
			for _, o := range overrides {
				checked++
				if !ok {
					continue
				}
				pdf := filepath.Join(pdfDir, o.Fixture+".pdf")
				if _, err := os.Stat(pdf); err != nil {
					t.Errorf("%s: %s page %d names no committed fixture: %v", rel, o.Fixture, o.Page, err)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no golden override to check")
	}
}

// overridePDFDir returns the directory holding the PDFs the overrides of
// the corpus whose manifest is at path name: the committed tree of a corpus
// that commits its PDFs (litho), else the provisioned testdata of a
// downloaded one (govdocs). ok is false for a downloaded corpus this
// checkout has not provisioned, whose fixtures the test cannot see.
func overridePDFDir(path string, m Manifest) (dir string, ok bool) {
	committed := filepath.Join(filepath.Dir(path), m.PDFRoot)
	if info, err := os.Stat(committed); err == nil && info.IsDir() {
		return committed, true
	}
	leaf := m.Source
	if leaf == "" {
		leaf = m.Name
	}
	provisioned := filepath.Join("..", "..", "testdata", leaf, m.PDFRoot)
	if info, err := os.Stat(provisioned); err == nil && info.IsDir() {
		return provisioned, true
	}
	return "", false
}

func takesOverrides(feature string) bool {
	for _, ff := range featureRegistry {
		f := ff()
		if f.Name() != feature {
			continue
		}
		_, ok := f.(GoldenOverrider)
		return ok
	}
	return false
}

func TestReadTextOverridesRejectsBrokenEntries(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"no fixture":   `{"overrides":[{"page":1,"pdfjs":"a","expected":"b","reason":"r"}]}`,
		"page zero":    `{"overrides":[{"fixture":"f","page":0,"pdfjs":"a","expected":"b","reason":"r"}]}`,
		"no reason":    `{"overrides":[{"fixture":"f","page":1,"pdfjs":"a","expected":"b"}]}`,
		"neither":      `{"overrides":[{"fixture":"f","page":1,"pdfjs":"a","reason":"r"}]}`,
		"both":         `{"overrides":[{"fixture":"f","page":1,"pdfjs":"a","expected":"b","unscored":true,"reason":"r"}]}`,
		"same reading": `{"overrides":[{"fixture":"f","page":1,"pdfjs":"a  b","expected":"a b","reason":"r"}]}`,
		"listed twice": `{"overrides":[{"fixture":"f","page":1,"pdfjs":"a","expected":"b","reason":"r"},{"fixture":"f","page":1,"pdfjs":"a","unscored":true,"reason":"r"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "o.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readTextOverrides(path); !errors.Is(err, errTextOverrideInvalid) {
				t.Errorf("readTextOverrides = %v, want %v", err, errTextOverrideInvalid)
			}
		})
	}
}

// TestTextCompareAppliesOverrides pins how an override changes a verdict: an
// expected reading replaces the golden, an unscored page drops out, a page
// whose golden no longer reads as the override recorded fails, and a fixture
// with every page unscored is skipped.
func TestTextCompareAppliesOverrides(t *testing.T) {
	t.Parallel()
	golden := func(pages ...string) *pdfjsReference {
		ref := &pdfjsReference{File: "f.pdf", NumPages: len(pages)}
		for i, text := range pages {
			ref.Pages = append(ref.Pages, pdfjsRefPage{PageNumber: i + 1, Text: text})
		}
		return ref
	}
	output := func(pages ...string) []byte {
		out := textOutput{NumPages: len(pages)}
		for i, text := range pages {
			out.Pages = append(out.Pages, textOutputPage{PageNumber: i + 1, Text: text})
		}
		raw, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	cases := []struct {
		name     string
		golden   *pdfjsReference
		override textOverride
		litho    []byte
		match    bool
		skipped  bool
	}{
		{
			name:     "expected reading replaces the golden",
			golden:   golden("veri cation"),
			override: textOverride{Page: 1, PDFJS: "veri cation", Expected: "verication"},
			litho:    output("verication"),
			match:    true,
		},
		{
			name:     "the golden's reading no longer counts",
			golden:   golden("veri cation"),
			override: textOverride{Page: 1, PDFJS: "veri cation", Expected: "verication"},
			litho:    output("veri cation"),
		},
		{
			name:     "stale override fails the fixture",
			golden:   golden("veri fication"),
			override: textOverride{Page: 1, PDFJS: "veri cation", Expected: "verication"},
			litho:    output("verication"),
		},
		{
			name:     "every page unscored skips the fixture",
			golden:   golden("\x01"),
			override: textOverride{Page: 1, PDFJS: "\x01", Unscored: true},
			litho:    output("A"),
			skipped:  true,
		},
		{
			name:     "an unscored page leaves the others scored",
			golden:   golden("\x01", "AB"),
			override: textOverride{Page: 1, PDFJS: "\x01", Unscored: true},
			litho:    output("A", "AB"),
			match:    true,
		},
		{
			name:     "a scored page still fails beside an unscored one",
			golden:   golden("\x01", "AB"),
			override: textOverride{Page: 1, PDFJS: "\x01", Unscored: true},
			litho:    output("A", "A B"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newTextFeature()
			f.goldens["f"] = c.golden
			c.override.Fixture = "f"
			f.overrides[textOverrideKey{"f", c.override.Page}] = c.override
			v := f.Compare("PDF_A-2b/f", c.litho)
			if v.Match != c.match || v.Skipped != c.skipped {
				t.Errorf("Compare = match %v skipped %v, want match %v skipped %v (%+v)", v.Match, v.Skipped, c.match, c.skipped, v)
			}
		})
	}
}

func TestLoadOverridesRefusesAPageNoGoldenHolds(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "o.json")
	body := `{"overrides":[{"fixture":"f","page":2,"pdfjs":"a","expected":"b","reason":"r"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newTextFeature()
	f.goldens["f"] = &pdfjsReference{Pages: []pdfjsRefPage{{PageNumber: 1}}}
	if err := f.LoadOverrides(path); !errors.Is(err, errTextOverrideNoGolden) {
		t.Errorf("LoadOverrides = %v, want %v", err, errTextOverrideNoGolden)
	}
}

// TestOncePerPDFKeepsTheFirstFixtureOfEachPDF pins how a profile-agnostic
// feature sees an "expand" corpus: one fixture per PDF, under its basename.
func TestOncePerPDFKeepsTheFirstFixtureOfEachPDF(t *testing.T) {
	t.Parallel()
	in := []Fixture{
		{ID: "PDF_A-1b/a", PDFPath: "/p/a.pdf"},
		{ID: "PDF_A-2b/a", PDFPath: "/p/a.pdf"},
		{ID: "PDF_A-1b/b", PDFPath: "/p/b.pdf", SkipReason: "bomb"},
		{ID: "PDF_A-2b/b", PDFPath: "/p/b.pdf"},
		{ID: "c", PDFPath: "/p/c.pdf"},
	}
	want := []Fixture{
		{ID: "a", PDFPath: "/p/a.pdf"},
		{ID: "b", PDFPath: "/p/b.pdf", SkipReason: "bomb"},
		{ID: "c", PDFPath: "/p/c.pdf"},
	}
	if got := oncePerPDF(in); !reflect.DeepEqual(got, want) {
		t.Errorf("oncePerPDF = %+v, want %+v", got, want)
	}
}
