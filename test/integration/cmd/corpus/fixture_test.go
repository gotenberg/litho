package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestWalkPDFsFailsOnAMissingRoot pins that an absent pdfs/ tree fails the
// load. Skipping the walk error scored such a corpus as zero fixtures, so its
// metrics vanished from the run instead of failing it.
func TestWalkPDFsFailsOnAMissingRoot(t *testing.T) {
	m := &Manifest{Name: "w35-no-such-corpus", PDFRoot: "pdfs"}
	pdfs, err := walkPDFs(m)
	if err == nil {
		t.Fatalf("walkPDFs on a missing root returned %d files and no error", len(pdfs))
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("walkPDFs error = %v, want one wrapping fs.ErrNotExist", err)
	}
}

func TestProfileFromXMP(t *testing.T) {
	cases := []struct {
		name string
		xmp  string
		want string
	}{
		{
			name: "empty",
			xmp:  "",
			want: "",
		},
		{
			name: "no claim",
			xmp:  `<rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>X</dc:title></rdf:Description>`,
			want: "",
		},
		{
			name: "PDF/A-1b attribute form",
			xmp:  `<rdf:Description pdfaid:part="1" pdfaid:conformance="B"/>`,
			want: "PDF_A-1b",
		},
		{
			name: "PDF/A-2u element form",
			xmp:  `<rdf:Description><pdfaid:part>2</pdfaid:part><pdfaid:conformance>U</pdfaid:conformance></rdf:Description>`,
			want: "PDF_A-2u",
		},
		{
			name: "PDF/A-3a lowercase conformance",
			xmp:  `<x pdfaid:part="3" pdfaid:conformance="a"/>`,
			want: "PDF_A-3a",
		},
		{
			name: "PDF/A-4 plain (rev only)",
			xmp:  `<x pdfaid:part="4" pdfaid:rev="2020"/>`,
			want: "PDF_A-4",
		},
		{
			name: "PDF/A-4e",
			xmp:  `<x pdfaid:part="4" pdfaid:conformance="E" pdfaid:rev="2020"/>`,
			want: "PDF_A-4e",
		},
		{
			name: "PDF/A-4f",
			xmp:  `<x pdfaid:part="4" pdfaid:conformance="F"/>`,
			want: "PDF_A-4f",
		},
		{
			name: "PDF/A-1 missing conformance",
			xmp:  `<x pdfaid:part="1"/>`,
			want: "",
		},
		{
			name: "PDF/UA-1",
			xmp:  `<x pdfuaid:part="1"/>`,
			want: "PDF_UA-1",
		},
		{
			name: "PDF/UA-2 element form",
			xmp:  `<rdf:Description><pdfuaid:part>2</pdfuaid:part></rdf:Description>`,
			want: "PDF_UA-2",
		},
		{
			name: "UA wins over A when both claimed",
			xmp:  `<x pdfaid:part="2" pdfaid:conformance="b" pdfuaid:part="1"/>`,
			want: "PDF_UA-1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := profileFromXMP([]byte(c.xmp))
			if got != c.want {
				t.Errorf("profileFromXMP() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestLoadFixturesExpand(t *testing.T) {
	pdfs := []string{"/tmp/alpha.pdf", "/tmp/beta.pdf"}
	m := &Manifest{ProfileMode: "expand", Profiles: []string{"PDF_A-1b", "PDF_A-2b"}}
	got, err := loadFixturesExpand(m, pdfs)
	if err != nil {
		t.Fatalf("loadFixturesExpand: %v", err)
	}
	want := []string{
		"PDF_A-1b/alpha", "PDF_A-1b/beta",
		"PDF_A-2b/alpha", "PDF_A-2b/beta",
	}
	if diff := compareIDs(got, want); diff != "" {
		t.Errorf("ID mismatch:\n%s", diff)
	}
}

func TestLoadFixturesExpandRequiresProfiles(t *testing.T) {
	if _, err := loadFixturesExpand(&Manifest{}, []string{"/tmp/x.pdf"}); err == nil {
		t.Fatal("expected error for empty profiles in expand mode")
	}
}

func TestLoadFixturesNone(t *testing.T) {
	pdfs := []string{"/tmp/alpha.pdf", "/tmp/beta.pdf"}
	m := &Manifest{ProfileMode: "none", Skips: map[string]string{"alpha": "skipped"}}
	got := loadFixturesNone(m, pdfs)
	if diff := compareIDs(got, []string{"alpha", "beta"}); diff != "" {
		t.Errorf("ID mismatch:\n%s", diff)
	}
	for _, fx := range got {
		switch fx.ID {
		case "alpha":
			if fx.SkipReason == "" {
				t.Errorf("alpha: expected SkipReason, got empty")
			}
		case "beta":
			if fx.SkipReason != "" {
				t.Errorf("beta: expected no SkipReason, got %q", fx.SkipReason)
			}
		}
	}
}

func TestLoadFixturesAutoNoClaimFallsBackToBasename(t *testing.T) {
	// Stub PDFs are empty; pdf.Open fails so detectClaimedProfile
	// returns "" and the auto loader falls back to basename IDs.
	dir := t.TempDir()
	for _, name := range []string{"alpha.pdf", "beta.pdf"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte{}, 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	pdfs := []string{filepath.Join(dir, "alpha.pdf"), filepath.Join(dir, "beta.pdf")}
	m := &Manifest{ProfileMode: "auto", Profiles: []string{"PDF_A-2b"}}
	got, err := loadFixturesAuto(context.Background(), m, pdfs)
	if err != nil {
		t.Fatalf("loadFixturesAuto: %v", err)
	}
	if diff := compareIDs(got, []string{"alpha", "beta"}); diff != "" {
		t.Errorf("ID mismatch:\n%s", diff)
	}
}

func TestLoadFixturesUnknownMode(t *testing.T) {
	m := &Manifest{ProfileMode: "bogus"}
	if _, err := LoadFixtures(context.Background(), m); err == nil {
		t.Fatal("expected error for unknown profile_mode")
	}
}

func TestEffectiveProfileModeDefaultsToExpand(t *testing.T) {
	m := &Manifest{}
	if got := m.EffectiveProfileMode(); got != "expand" {
		t.Errorf("EffectiveProfileMode() = %q, want %q", got, "expand")
	}
}

func TestLoadClaimsCSV(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "claims.csv")
	body := "alpha,PDF_A-2b\n" +
		"\n" + // blank line ignored
		"beta,PDF_UA-1\n" +
		"malformed_line\n" + // no comma -> dropped
		",PDF_A-1b\n" + // empty base -> dropped
		"gamma,\n" + // empty profile -> dropped
		"delta,PDF_A-4e\n"
	if err := os.WriteFile(csv, []byte(body), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	got, err := loadClaimsCSV(csv)
	if err != nil {
		t.Fatalf("loadClaimsCSV: %v", err)
	}
	want := map[string]string{
		"alpha": "PDF_A-2b",
		"beta":  "PDF_UA-1",
		"delta": "PDF_A-4e",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %#v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestLoadClaimsCSVMissing(t *testing.T) {
	if _, err := loadClaimsCSV(filepath.Join(t.TempDir(), "nope.csv")); err == nil {
		t.Fatal("expected error for missing csv")
	}
}

func compareIDs(got []Fixture, want []string) string {
	gotIDs := make([]string, len(got))
	for i, fx := range got {
		gotIDs[i] = fx.ID
	}
	sort.Strings(gotIDs)
	sort.Strings(want)
	if len(gotIDs) != len(want) {
		return "got " + sliceFmt(gotIDs) + "\nwant " + sliceFmt(want)
	}
	for i := range gotIDs {
		if gotIDs[i] != want[i] {
			return "got " + sliceFmt(gotIDs) + "\nwant " + sliceFmt(want)
		}
	}
	return ""
}

func sliceFmt(s []string) string {
	var out strings.Builder
	out.WriteString("[")
	for i, v := range s {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(v)
	}
	return out.String() + "]"
}
