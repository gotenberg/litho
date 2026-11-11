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

// TestConvert_WritesTheVersionAsked checks the header of each version
// Convert targets, and that the file reads back.
func TestConvert_WritesTheVersionAsked(t *testing.T) {
	dir := convertFontDir(t)
	for _, version := range []string{"1.4", "1.5", "1.6", "1.7", "2.0"} {
		data, rep := convertText(t, dir, "A line of text.", WithConvertVersion(version))
		if rep.Conformance != nil {
			t.Errorf("version %s: Conformance = %+v, want nil without a profile", version, rep.Conformance)
		}
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

// TestConvert_ConformsToTheProfilesAsked checks that a conversion to a
// profile validates at that profile, reports nothing left, and declares the
// version the profile builds on.
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
			data, rep := convertText(t, dir, "A line of text.\nAnother line.", WithConvertPDFA(tc.pdfa), WithConvertPDFUA(tc.pdfua), WithDefaultLanguage("en"))
			if rep.Conformance == nil {
				t.Fatal("Conformance = nil, want a report")
			}
			if len(rep.Conformance.Residual) > 0 {
				t.Errorf("Residual = %v, want none", rep.Conformance.Residual)
			}
			ctx := context.Background()
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

// TestConvert_RefusesATargetItCannotWrite checks the versions, profiles and
// combinations Convert refuses before it reads the source.
func TestConvert_RefusesATargetItCannotWrite(t *testing.T) {
	dir := convertFontDir(t)
	for name, tc := range map[string]struct {
		opts []ConvertOption
		also error
	}{
		"unknown version":        {opts: []ConvertOption{WithConvertVersion("1.3")}},
		"unknown PDF/A profile":  {opts: []ConvertOption{WithConvertPDFA("PDF/A-9z")}},
		"unknown PDF/UA profile": {opts: []ConvertOption{WithConvertPDFUA("PDF/UA-9")}},
		"PDF/A-4f":               {opts: []ConvertOption{WithConvertPDFA(models.PDFA4f)}},
		"PDF/A-1b at 1.7":        {opts: []ConvertOption{WithConvertPDFA(models.PDFA1b), WithConvertVersion("1.7")}},
		"PDF/A-2b at 2.0":        {opts: []ConvertOption{WithConvertPDFA(models.PDFA2b), WithConvertVersion("2.0")}},
		"PDF/UA-2 at 1.7":        {opts: []ConvertOption{WithConvertPDFUA(models.PDFUA2), WithConvertVersion("1.7")}},
		"PDF/A-2b with PDF/UA-2": {opts: []ConvertOption{WithConvertPDFA(models.PDFA2b), WithConvertPDFUA(models.PDFUA2)}, also: ErrIncompatibleProfiles},
		"PDF/A-4 with PDF/UA-1":  {opts: []ConvertOption{WithConvertPDFA(models.PDFA4), WithConvertPDFUA(models.PDFUA1)}, also: ErrIncompatibleProfiles},
	} {
		var out bytes.Buffer
		opts := append([]ConvertOption{WithConvertFontDirs(dir)}, tc.opts...)
		_, err := Convert(context.Background(), strings.NewReader("A"), FormatText, &out, opts...)
		if !errors.Is(err, ErrInvalidConvertOptions) {
			t.Errorf("%s: error = %v, want ErrInvalidConvertOptions", name, err)
		}
		if tc.also != nil && !errors.Is(err, tc.also) {
			t.Errorf("%s: error = %v, want it to wrap %v too", name, err, tc.also)
		}
		if out.Len() != 0 {
			t.Errorf("%s: %d bytes written before the refusal", name, out.Len())
		}
	}
}

// TestConvert_LeavesNoTempFile checks that a conversion to a profile writes
// its intermediate files in the directory WithConvertTempDir names and
// removes them, on success and on failure, and that a failure writes
// nothing to dst.
func TestConvert_LeavesNoTempFile(t *testing.T) {
	dir := convertFontDir(t)
	tmp := t.TempDir()
	data, rep := convertText(t, dir, "A line of text.", WithConvertPDFA(models.PDFA2b), WithConvertTempDir(tmp))
	if rep.Conformance == nil || len(data) == 0 {
		t.Fatalf("Conformance = %v, %d bytes", rep.Conformance, len(data))
	}
	if entries, err := os.ReadDir(tmp); err != nil || len(entries) != 0 {
		t.Errorf("temp dir holds %v (%v) after a conversion, want nothing", entries, err)
	}

	var out bytes.Buffer
	_, err := Convert(context.Background(), strings.NewReader("A"), FormatText, &out,
		WithConvertFontDirs(dir), WithConvertPDFA(models.PDFA2b), WithConvertTempDir(filepath.Join(tmp, "missing")))
	if err == nil {
		t.Fatal("Convert into a missing temp dir succeeded")
	}
	if out.Len() != 0 {
		t.Errorf("%d bytes written before the failure", out.Len())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Convert(ctx, strings.NewReader("A"), FormatText, &out, WithConvertFontDirs(dir), WithConvertPDFA(models.PDFA2b), WithConvertTempDir(tmp)); err == nil {
		t.Error("Convert with a cancelled context succeeded")
	}
	if entries, err := os.ReadDir(tmp); err != nil || len(entries) != 0 {
		t.Errorf("temp dir holds %v (%v) after a failure, want nothing", entries, err)
	}
}
