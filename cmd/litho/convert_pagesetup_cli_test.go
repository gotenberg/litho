package main

import (
	"bytes"
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// TestConvert_PaperFlagsReachEveryFormat checks that --default-paper and
// --landscape reach a text file, and that a flag the format does not take
// comes back as an option-ignored warning rather than vanishing.
func TestConvert_PaperFlagsReachEveryFormat(t *testing.T) {
	t.Parallel()
	src := writeTempText(t, "a.txt", "AB")
	res, out := convertJSON(t, "--default-paper", "letter", "--landscape", "--scale", "1.5", "--pages", "1", src)
	r, err := pdf.OpenFile(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	p, err := r.Page(context.Background(), 1)
	if err != nil || math.Abs(p.Width-792) > 0.01 || math.Abs(p.Height-612) > 0.01 {
		t.Errorf("page %vx%v, %v, want landscape Letter", p.Width, p.Height, err)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Code != pdf.CodeOptionIgnored {
		t.Errorf("warnings %+v, want one option-ignored for --scale", res.Warnings)
	}
}

// TestConvert_RefusesAnOptionOutOfRangeForAnyFormat checks that a flag out
// of range fails even for a format that does not take it.
func TestConvert_RefusesAnOptionOutOfRangeForAnyFormat(t *testing.T) {
	t.Parallel()
	src := writeTempText(t, "a.txt", "A")
	out := filepath.Join(t.TempDir(), "out.pdf")
	for _, flags := range [][]string{{"--scale", "5"}, {"--font-size", "0"}, {"--default-paper", "B7"}} {
		args := append([]string{"convert", "--font-dir", convertFontDir(t), "-o", out}, append(flags, src)...)
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitError || stderr.Len() == 0 {
			t.Errorf("%q: exit %d, stderr %q, want 1 with a message", flags, rc, stderr.String())
		}
	}
}
