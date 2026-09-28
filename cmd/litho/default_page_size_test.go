package main

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// noMediaBoxPDF builds two pages that state no /MediaBox, each drawing a word,
// so no page of the document gives a size a write can take.
func noMediaBoxPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 /Resources << /Font << /F1 6 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /Contents 5 0 R >>",
		"<< /Length 36 >>\nstream\nBT /F1 12 Tf 20 20 Td (Sized) Tj ET\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// firstPageSize returns the size of page 1 of the PDF at path.
func firstPageSize(t *testing.T, path string) (float64, float64) {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // path is this test's own temp file
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	r, err := pdf.Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("opening output: %v", err)
	}
	defer r.Close()
	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page(1): %v", err)
	}
	return p.Width, p.Height
}

// TestDefaultPageSizeFlag_SizesEveryWrite pins that --default-page-size names
// the size a page with no media box takes, when no page of the document has
// one, on every command that writes the page, and that US Letter is the
// default. An update that writes no page, as set-metadata appends, leaves the
// page as the file states it.
func TestDefaultPageSizeFlag_SizesEveryWrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(src, noMediaBoxPDF(), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	commands := [][]string{
		{"split", src, "--pages", "1"},
		{"merge", src, src},
		{"delete", src, "--pages", "2"},
		{"rotate", src, "--angle", "180"},
		{"flatten-form", src},
		{"stamp-text", src, "--text", "MARK"},
		{"remediate", src, "--profile", "PDF/A-2b"},
		{"pipe", src, "rotate", "--angle", "180", "split", "--pages", "1", "set-metadata", "--title", "Sized"},
	}
	for _, size := range []struct {
		flag []string
		want [2]float64
	}{
		{nil, [2]float64{612, 792}},
		{[]string{"--default-page-size", "a4"}, [2]float64{595.28, 841.89}},
		{[]string{"--default-page-size=420x595"}, [2]float64{420, 595}},
	} {
		for _, cmd := range commands {
			name := cmd[0] + " " + strings.Join(size.flag, " ")
			t.Run(name, func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "out.pdf")
				args := append(append(append([]string{}, cmd...), size.flag...), "-o", out)
				var stdout, stderr bytes.Buffer
				if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
					t.Fatalf("%v: exit %d, stderr: %s", args, rc, stderr.String())
				}
				w, h := firstPageSize(t, out)
				if math.Abs(w-size.want[0]) > 0.01 || math.Abs(h-size.want[1]) > 0.01 {
					t.Errorf("page 1 measures %g by %g, want %v", w, h, size.want)
				}
			})
		}
	}
}

// TestDefaultPageSizeFlag_RefusesAnUnknownSize pins that a value naming no
// size, or a side ISO 32000-1, Annex C.2 does not allow, fails before any
// output is written.
func TestDefaultPageSizeFlag_RefusesAnUnknownSize(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(src, noMediaBoxPDF(), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	for _, value := range []string{"b5", "612", "612x", "2x792", "612x14401", "NaNx792", ""} {
		for _, cmd := range [][]string{{"split", src, "--pages", "1"}, {"pipe", src, "rotate", "--angle", "90"}} {
			out := filepath.Join(t.TempDir(), "out.pdf")
			args := append(append([]string{}, cmd...), "--default-page-size", value, "-o", out)
			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), args, &stdout, &stderr); rc == exitOK {
				t.Errorf("%s --default-page-size %q: exit 0, want a refusal", cmd[0], value)
			}
			if !strings.Contains(stderr.String(), "default-page-size") {
				t.Errorf("%s --default-page-size %q: stderr %q names no flag", cmd[0], value, stderr.String())
			}
			if _, err := os.Stat(out); err == nil {
				t.Errorf("%s --default-page-size %q wrote an output", cmd[0], value)
			}
		}
	}
}

// TestPageSizeFlag_Set pins the sizes the flag names.
func TestPageSizeFlag_Set(t *testing.T) {
	for value, want := range map[string][2]float64{
		"letter": {612, 792}, "LEGAL": {612, 1008}, "a3": {841.89, 1190.55},
		"A4": {595.28, 841.89}, "a5": {419.53, 595.28}, "300x400.5": {300, 400.5}, " 3 x 14400 ": {3, 14400},
	} {
		var f pageSizeFlag
		if err := f.Set(value); err != nil {
			t.Errorf("Set(%q): %v", value, err)
			continue
		}
		if f.width != want[0] || f.height != want[1] || len(f.writeOptions()) != 1 {
			t.Errorf("Set(%q) = %g by %g, want %v", value, f.width, f.height, want)
		}
	}
	var unset pageSizeFlag
	if opts := unset.writeOptions(); opts != nil {
		t.Errorf("an unset flag selects %d options, want none", len(opts))
	}
}
