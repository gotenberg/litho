package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSVGListAndHashesAgree(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(corpusSourceDir, "svg")
	data, err := os.ReadFile(filepath.Join(dir, "tests.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tests []*svgTest
	if err := json.Unmarshal(data, &tests); err != nil {
		t.Fatal(err)
	}
	if len(tests) < 150 || len(tests) > 250 {
		t.Errorf("%d tests, want 150 to 250", len(tests))
	}
	hashes, err := os.ReadFile(filepath.Join(dir, "files.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	listed := make(map[string]bool)
	sc := bufio.NewScanner(bytes.NewReader(hashes))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 || len(fields[0]) != 64 {
			t.Fatalf("files.sha256: malformed line %q", sc.Text())
		}
		listed[fields[1]] = true
	}
	need := func(p string) {
		if !listed[p] {
			t.Errorf("%s is read by a test but not in files.sha256", p)
		}
	}
	need("LICENSE-MIT")
	need("crates/resvg/tests/fonts/NotoSans-Regular.ttf")
	need("crates/resvg/tests/fonts/Noto-LICENSE-OFL.txt")
	for _, e := range tests {
		need("crates/resvg/tests/tests/" + e.ID)
		need("crates/resvg/tests/tests/" + e.PNG)
	}
}

func TestSVGDraw_ComparesWithTheReference(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "shapes", "rect")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	svgSrc := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="5" fill="red"/></svg>`
	if err := os.WriteFile(filepath.Join(dir, "half.svg"), []byte(svgSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRef := func(name string, split int) {
		img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
		for y := range 20 {
			for x := range 20 {
				c := color.NRGBA{A: 0}
				if y < split {
					c = color.NRGBA{R: 255, A: 255}
				}
				img.SetNRGBA(x, y, c)
			}
		}
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRef("half.png", 10)
	writeRef("wrong.png", 2)
	ahem, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "pkg", "internal", "layout", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	f := newSVGDrawFeature()
	f.fontDirs = []string{ahem}
	f.tests["shapes/rect/half.svg"] = &svgTest{ID: "shapes/rect/half.svg", PNG: "shapes/rect/half.png", Width: 20, Height: 20}
	f.tests["shapes/rect/wrong.svg"] = &svgTest{ID: "shapes/rect/half.svg", PNG: "shapes/rect/wrong.png", Width: 20, Height: 20}
	for id, want := range map[string]string{"shapes/rect/half.svg": outcomePass, "shapes/rect/wrong.svg": outcomeFail} {
		out, err := f.Run(context.Background(), filepath.Join(root, filepath.FromSlash("shapes/rect/half.svg")), id)
		if err != nil {
			t.Fatal(err)
		}
		if v := f.Compare(id, out); v.Outcome != want {
			t.Errorf("%s: outcome %s (%s), want %s", id, v.Outcome, v.Reason, want)
		}
	}
}
