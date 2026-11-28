package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInstalledFontsDigest_NamesTheFilesOfEachDirectory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	fonts := filepath.Join(root, "test", "integration", "testdata", "fonts")
	writeFiles(t, fonts, map[string]string{
		"a/A.ttf":   "a",
		"a/OFL.txt": "licence",
		"b/B.otf":   "b",
	})
	if got, err := installedFontsDigest(nil); got != "" || err != nil {
		t.Errorf("no directory: %q, %v; want no digest", got, err)
	}
	ab, err := installedFontsDigest([]string{"a", "b"})
	if err != nil || ab == "" {
		t.Fatalf("a, b: %q, %v", ab, err)
	}
	ba, err := installedFontsDigest([]string{"b", "a"})
	if err != nil || ba == ab {
		t.Errorf("b, a: %q, %v; want another digest than a, b", ba, err)
	}
	writeFiles(t, fonts, map[string]string{"a/A.ttf": "another release"})
	if again, err := installedFontsDigest([]string{"a", "b"}); err != nil || again == ab {
		t.Errorf("a font that changed keeps the digest %q, %v", again, err)
	}
	if _, err := installedFontsDigest([]string{"missing"}); !errors.Is(err, errFontsMissing) {
		t.Errorf("missing directory: %v", err)
	}

	got, err := withInstalledFonts([]string{"image"}, []string{"a", "b"})
	want := []string{"image", filepath.Join(fixtureFontsRoot(), "a"), filepath.Join(fixtureFontsRoot(), "b")}
	if err != nil || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("withInstalledFonts = %v, %v; want %v", got, err, want)
	}
	if got, err := withInstalledFonts(nil, []string{"a"}); got != nil || err != nil {
		t.Errorf("system fonts: %v, %v; want nil", got, err)
	}
	if _, err := withInstalledFonts([]string{"image"}, []string{"missing"}); !errors.Is(err, errFontsMissing) {
		t.Errorf("missing directory: %v", err)
	}
}

// Not parallel: it changes the working directory, where the corpus, its
// testdata and the fixture fonts are found.
//
// TestBuildReferences_OneConverterPerInstalledFontSet checks that a build
// asks one converter for the sources that name no installed fonts and one
// for each set of installed fonts, records the fonts' digest beside the
// sources that name some and none beside the others, and that a font that
// changes makes those references alone stale.
func TestBuildReferences_OneConverterPerInstalledFontSet(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := stubCorpus(t, dir, "stub/fonts", engineChromium, map[string]map[string]string{
		"page":  {"index.html": "<p>x</p>"},
		"flex":  {"index.html": "<p>x</p>", "options.json": `{"installedFonts": ["a"]}`},
		"flex2": {"index.html": "<p>y</p>", "options.json": `{"installedFonts": ["a"]}`},
	})
	fonts := filepath.Join("test", "integration", "testdata", "fonts")
	writeFiles(t, fonts, map[string]string{"a/A.ttf": "a"})
	var (
		opened []string
		convs  = map[string]*recordingConverter{}
	)
	open := func(_ context.Context, fonts []string) (referenceConverter, error) {
		key := strings.Join(fonts, ",")
		opened = append(opened, key)
		convs[key] = &recordingConverter{}
		return convs[key], nil
	}
	pins := &resolvedPins{Values: map[string]string{"GOTENBERG_IMAGE": "img:1"}}
	if err := buildReferences(t.Context(), open, m, pins, nil); err != nil {
		t.Fatal(err)
	}
	slices.Sort(opened)
	if !slices.Equal(opened, []string{"", "a"}) {
		t.Errorf("opened %q, want one plain converter and one with a", opened)
	}
	if got := convs[""].names; !slices.Equal(got, []string{"page"}) {
		t.Errorf("the plain converter built %v", got)
	}
	got := convs["a"].names
	slices.Sort(got)
	if !slices.Equal(got, []string{"flex", "flex2"}) {
		t.Errorf("the converter with a built %v", got)
	}
	man := readReferenceManifest(t, m)
	if man.References["page"].FontsSHA256 != "" || man.References["flex"].FontsSHA256 == "" {
		t.Errorf("fonts digests %q and %q, want none for page", man.References["page"].FontsSHA256, man.References["flex"].FontsSHA256)
	}

	fixtures := []Fixture{
		{ID: "page", PDFPath: filepath.Join(m.SourceDir(), "sources", "page")},
		{ID: "flex", PDFPath: filepath.Join(m.SourceDir(), "sources", "flex")},
	}
	for _, fx := range fixtures {
		if why, _ := checkReference(man, m.References, fx.ID, fx, pins.Values); why != "" {
			t.Errorf("%s: %s", fx.ID, why)
		}
	}
	writeFiles(t, fonts, map[string]string{"a/A.ttf": "another release"})
	if why, rebuild := checkReference(man, m.References, "flex", fixtures[1], pins.Values); !rebuild || !strings.Contains(why, "installed fonts changed") {
		t.Errorf("flex after its font changed: %q, %v", why, rebuild)
	}
	if why, _ := checkReference(man, m.References, "page", fixtures[0], pins.Values); why != "" {
		t.Errorf("page after another font changed: %s", why)
	}
}
