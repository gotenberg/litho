package pdf

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestConvert_WritesTheVersionAsked checks the header of each version a
// conversion writes, and that the file reads back.
func TestConvert_WritesTheVersionAsked(t *testing.T) {
	dir := convertFontDir(t)
	for _, version := range []string{"1.4", "1.5", "1.6", "1.7", "2.0"} {
		data, _ := convertText(t, dir, "A line of text.", WithConvertVersion(version))
		if !bytes.HasPrefix(data, []byte("%PDF-"+version)) {
			t.Errorf("version %s: header %q", version, data[:8])
		}
		r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("version %s: Open: %v", version, err)
		}
		if got := r.Version(); got != version {
			t.Errorf("version %s: Reader.Version = %q", version, got)
		}
		if err := r.Close(); err != nil {
			t.Errorf("version %s: Close: %v", version, err)
		}
	}
}

// TestConvert_ConformsToTheProfilesAsked checks that a conversion at the
// version a profile builds on, remediated to that profile in a later
// pipeline stage, validates at that profile, reports nothing left, and
// declares that version.
func TestConvert_ConformsToTheProfilesAsked(t *testing.T) {
	dir := convertFontDir(t)
	for name, tc := range map[string]struct {
		pdfa    models.PDFAProfile
		pdfua   models.PDFUAProfile
		version string
	}{
		"PDF/A-1b":            {pdfa: models.PDFA1b, version: "1.4"},
		"PDF/A-2b":            {pdfa: models.PDFA2b, version: "1.7"},
		"PDF/A-3b":            {pdfa: models.PDFA3b, version: "1.7"},
		"PDF/A-4":             {pdfa: models.PDFA4, version: "2.0"},
		"PDF/UA-1":            {pdfua: models.PDFUA1, version: "1.7"},
		"PDF/UA-2":            {pdfua: models.PDFUA2, version: "2.0"},
		"PDF/A-2a + PDF/UA-1": {pdfa: models.PDFA2a, pdfua: models.PDFUA1, version: "1.7"},
		"PDF/A-4 + PDF/UA-2":  {pdfa: models.PDFA4, pdfua: models.PDFUA2, version: "2.0"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			c := FromText(strings.NewReader("A line of text.\nAnother line."), WithConvertFontDirs(dir), WithDefaultLanguage("en"), WithConvertVersion(tc.version))
			data, rep, err := convertAndRemediate(ctx, c, tc.version, tc.pdfa, tc.pdfua, "en")
			if err != nil {
				t.Fatalf("converting and remediating: %v", err)
			}
			if len(rep.Residual) > 0 {
				t.Errorf("Residual = %v, want none", rep.Residual)
			}
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close() //nolint:errcheck // test reader
			if got := r.Version(); got != tc.version {
				t.Errorf("Reader.Version = %q, want %q", got, tc.version)
			}
			if tc.pdfa != "" {
				if violations, err := r.ValidatePDFA(ctx, tc.pdfa); err != nil || len(violations) > 0 {
					t.Errorf("ValidatePDFA(%s) = %v, %v", tc.pdfa, violations, err)
				}
			}
			if tc.pdfua != "" {
				if violations, err := r.ValidatePDFUA(ctx, tc.pdfua); err != nil || len(violations) > 0 {
					t.Errorf("ValidatePDFUA(%s) = %v, %v", tc.pdfua, violations, err)
				}
			}
		})
	}
}

// TestConvert_RefusesAVersionItCannotWrite checks that a version a
// conversion does not write fails before the source is read.
func TestConvert_RefusesAVersionItCannotWrite(t *testing.T) {
	dir := convertFontDir(t)
	for _, version := range []string{"1.3", "1.8", "2", "x"} {
		var out bytes.Buffer
		_, err := FromText(strings.NewReader("A"), WithConvertFontDirs(dir), WithConvertVersion(version)).WriteTo(context.Background(), &out)
		if !errors.Is(err, ErrInvalidConvertOptions) {
			t.Errorf("version %q: error = %v, want ErrInvalidConvertOptions", version, err)
		}
		if out.Len() != 0 {
			t.Errorf("version %q: %d bytes written before the refusal", version, out.Len())
		}
	}
}

// TestConvert_LeavesNoTempFile checks that a conversion remediated in a
// pipeline writes its intermediate files in the directory
// WithConvertTempDir and WithPipelineTempDir name and removes them, on
// success and on failure, and that a failure writes nothing to dst.
func TestConvert_LeavesNoTempFile(t *testing.T) {
	dir := convertFontDir(t)
	tmp := t.TempDir()
	ctx := context.Background()
	conversion := func(tempDir string) *Conversion {
		return FromText(strings.NewReader("A line of text."), WithConvertFontDirs(dir), WithConvertTempDir(tempDir))
	}
	data, rep, err := convertAndRemediate(ctx, conversion(tmp), "1.7", models.PDFA2b, "", "", WithPipelineTempDir(tmp))
	if err != nil || rep == nil || len(data) == 0 {
		t.Fatalf("converting to PDF/A-2b: %v, report %v, %d bytes", err, rep, len(data))
	}
	if entries, err := os.ReadDir(tmp); err != nil || len(entries) != 0 {
		t.Errorf("temp dir holds %v (%v) after a conversion, want nothing", entries, err)
	}

	missing := filepath.Join(tmp, "missing")
	data, _, err = convertAndRemediate(ctx, conversion(missing), "1.7", models.PDFA2b, "", "", WithPipelineTempDir(missing))
	if err == nil {
		t.Fatal("converting into a missing temp dir succeeded")
	}
	if len(data) != 0 {
		t.Errorf("%d bytes written before the failure", len(data))
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := convertAndRemediate(cancelled, conversion(tmp), "1.7", models.PDFA2b, "", "", WithPipelineTempDir(tmp)); err == nil {
		t.Error("converting with a cancelled context succeeded")
	}
	if entries, err := os.ReadDir(tmp); err != nil || len(entries) != 0 {
		t.Errorf("temp dir holds %v (%v) after a failure, want nothing", entries, err)
	}
}
