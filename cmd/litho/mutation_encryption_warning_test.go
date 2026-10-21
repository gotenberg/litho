package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Twelve output-producing commands turned an encrypted input into an
// unprotected one and said nothing.
//
// `f2a37513` warned on the five page operations, and the record inferred the
// rest of the surface was three more commands. Measured, it is twelve: attach,
// detach, autotag, flatten-form, set-bookmarks, set-form, set-metadata,
// stamp-text, stamp-image, stamp-pdf, remediate and redact.
//
// redact is the one that matters. The caller proves they hold the password,
// removes confidential content, and gets back a document anyone can open.
//
// The plaintext-out DEFAULT is untouched and remains deliberate, exactly as
// `f2a37513` left it. Only the silence is closed. convert-colors, which came
// later, warns from the start and has its row in the table.

// encFixtures carries the inputs the table needs, built once per subtest so a
// command that writes cannot disturb another's source.
type encFixtures struct {
	doc       string // encrypted two-page document
	onePage   string // encrypted single-page document
	form      string // encrypted document carrying form fields
	fieldName string
	data      string // a file to attach
	png       string
	stampPDF  string
}

func newEncFixtures(t *testing.T) encFixtures {
	t.Helper()

	plainForm := writeTempPDF(t, formFixturePDF())
	fields := listFormFields(t, plainForm)
	if len(fields) == 0 {
		t.Fatal("the form fixture reports no fields, so the set-form row would prove nothing")
	}

	dir := t.TempDir()
	png := filepath.Join(dir, "stamp.png")
	if err := os.WriteFile(png, onePixelPNG(t), 0o600); err != nil {
		t.Fatalf("writing the PNG fixture: %v", err)
	}

	return encFixtures{
		doc:     encryptTempPDF(t, twoPageTempPDF(t), "s3cret"),
		onePage: encryptTempPDF(t, writeTempPDF(t, untaggedTextPDF()), "s3cret"),
		form:    encryptTempPDF(t, plainForm, "s3cret"),

		fieldName: fields[0].Name,
		data:      dataFile(t, "table.csv", "a,b\n1,2\n"),
		png:       png,
		stampPDF:  writeTempPDF(t, untaggedTextPDF()),
	}
}

// TestMutationCommands_WarnWhenEncryptionIsDropped is the sibling of
// TestPageOperations_WarnWhenEncryptionIsDropped, for the commands that stayed
// silent.
//
// redact uses the SINGLE-page input on purpose. The two-page fixture is the
// one-page one merged with itself, so the text removed from page 1 still exists
// on page 2 and redaction correctly refuses with ErrRedactionIncomplete. That
// refusal happens on a plaintext input too, so it is the fixture rather than
// anything about encryption, and the warning is emitted before the work either
// way.
func TestMutationCommands_WarnWhenEncryptionIsDropped(t *testing.T) {
	for _, tc := range []struct {
		desc string
		args func(f encFixtures, out string) []string
	}{
		{"attach", func(f encFixtures, out string) []string {
			return []string{"attach", "-p", "s3cret", "-name", "table.csv", "-data", f.data, "-o", out, f.doc}
		}},
		{"detach", func(f encFixtures, out string) []string {
			return []string{"detach", "-p", "s3cret", "-name", "absent.csv", "-o", out, f.doc}
		}},
		{"autotag", func(f encFixtures, out string) []string {
			return []string{"autotag", "-p", "s3cret", "-o", out, f.doc}
		}},
		{"flatten-form", func(f encFixtures, out string) []string {
			return []string{"flatten-form", "-p", "s3cret", "-o", out, f.doc}
		}},
		{"set-bookmarks", func(f encFixtures, out string) []string {
			return []string{"set-bookmarks", "-p", "s3cret", "-generate", "-o", out, f.doc}
		}},
		{"set-form", func(f encFixtures, out string) []string {
			return []string{"set-form", "-p", "s3cret", "-field", f.fieldName + "=x", "-o", out, f.form}
		}},
		{"set-metadata", func(f encFixtures, out string) []string {
			return []string{"set-metadata", "-p", "s3cret", "-title", "T", "-o", out, f.doc}
		}},
		{"stamp-text", func(f encFixtures, out string) []string {
			return []string{"stamp-text", "-p", "s3cret", "-text", "DRAFT", "-o", out, f.doc}
		}},
		{"stamp-image", func(f encFixtures, out string) []string {
			return []string{"stamp-image", "-p", "s3cret", "-image", f.png, "-o", out, f.doc}
		}},
		{"stamp-pdf", func(f encFixtures, out string) []string {
			return []string{"stamp-pdf", "-p", "s3cret", "-stamp", f.stampPDF, "-o", out, f.doc}
		}},
		{"remediate", func(f encFixtures, out string) []string {
			return []string{"remediate", "-p", "s3cret", "--profile", "PDF/A-2b", "-o", out, f.doc}
		}},
		{"redact", func(f encFixtures, out string) []string {
			return []string{"redact", "-p", "s3cret", "-region", "1:0,650,600,120", "-o", out, f.onePage}
		}},
		{"convert-colors", func(f encFixtures, out string) []string {
			return []string{"convert-colors", "-p", "s3cret", "--target", "cmyk", "-o", out, f.doc}
		}},
		{"flatten-transparency", func(f encFixtures, out string) []string {
			return []string{"flatten-transparency", "-p", "s3cret", "--dpi", "72", "-o", out, f.doc}
		}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			f := newEncFixtures(t)
			out := filepath.Join(t.TempDir(), "out.pdf")

			var stdout, stderr bytes.Buffer
			rc := run(context.Background(), tc.args(f, out), &stdout, &stderr)
			if rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
			}
			if !strings.Contains(stderr.String(), encryptionWarning) {
				t.Errorf("no encryption warning: the output is unprotected and the caller is not told; stderr: %q",
					stderr.String())
			}
			// Without this the assertion above could pass on a command that
			// kept the protection, where there would be nothing to warn about.
			if !opensWithoutAPassword(t, out) {
				t.Error("the output is still encrypted, so no downgrade happened and this row proves nothing")
			}
		})
	}
}

// TestDecrypt_DoesNotWarnAboutTheDowngradeItWasAskedFor is the control that
// keeps the warning from being blanket-applied through the shared helper.
//
// decrypt, autotag and flatten-form all run through runSimpleMutation. Warning
// unconditionally there would make decrypt report the outcome the user
// requested as a loss, so it opts out via withoutEncryptionWarning.
func TestDecrypt_DoesNotWarnAboutTheDowngradeItWasAskedFor(t *testing.T) {
	enc := encryptTempPDF(t, twoPageTempPDF(t), "s3cret")
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"decrypt", "-p", "s3cret", "-o", out, enc,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	if strings.Contains(stderr.String(), encryptionWarning) {
		t.Errorf("decrypt warned that the output will not be encrypted, which is what it was asked to do; stderr: %q",
			stderr.String())
	}
	if !opensWithoutAPassword(t, out) {
		t.Error("decrypt did not actually remove the protection, so this control proves nothing")
	}
}

// TestMutationCommands_NoWarningForAPlainInput is the second control: nothing
// was lost, so nothing is reported. Without it, a version that printed the
// warning unconditionally would pass the table above.
func TestMutationCommands_NoWarningForAPlainInput(t *testing.T) {
	for _, tc := range []struct {
		desc string
		args func(src, out string) []string
	}{
		{"autotag, through the shared helper", func(src, out string) []string {
			return []string{"autotag", "-o", out, src}
		}},
		{"set-metadata, through its own path", func(src, out string) []string {
			return []string{"set-metadata", "-title", "T", "-o", out, src}
		}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			src := writeTempPDF(t, untaggedTextPDF())
			out := filepath.Join(t.TempDir(), "out.pdf")

			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), tc.args(src, out), &stdout, &stderr); rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
			}
			if strings.Contains(stderr.String(), encryptionWarning) {
				t.Errorf("warned about a plain input; stderr: %q", stderr.String())
			}
		})
	}
}
