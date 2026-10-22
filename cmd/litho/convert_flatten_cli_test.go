package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// TestConvertColorsThenFlattenStaysCMYK runs the print recipe, convert-colors
// to CMYK and then flatten-transparency, and checks that the composites the
// flattening draws are DeviceCMYK, as everything the page paints is: the
// conversion makes its target the output intent, which a page group naming no
// space composites in.
func TestConvertColorsThenFlattenStaysCMYK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := writeTempPDF(t, transparentCLIPDF())
	dir := t.TempDir()
	converted, flattened := filepath.Join(dir, "cmyk.pdf"), filepath.Join(dir, "flat.pdf")
	var stdout, stderr bytes.Buffer
	if rc := run(ctx, []string{"convert-colors", "--target", "cmyk", "-o", converted, src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("convert-colors exit %d: %s", rc, stderr.String())
	}
	if rc := run(ctx, []string{"flatten-transparency", "-o", flattened, converted}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("flatten-transparency exit %d: %s", rc, stderr.String())
	}
	r, err := pdf.OpenFile(ctx, flattened)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	images, err := r.ExtractImages(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) == 0 {
		t.Fatal("no composite image")
	}
	for _, im := range images {
		if im.ColorSpace != "DeviceCMYK" {
			t.Errorf("composite %s in %s, want DeviceCMYK", im.Filename, im.ColorSpace)
		}
	}
	uses, err := r.ExtractColorSpaces(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range uses {
		if u.Family != "DeviceCMYK" {
			t.Errorf("%s %s paints in %s, outside the CMYK target", u.Source, u.Operator, u.Family)
		}
	}
}
