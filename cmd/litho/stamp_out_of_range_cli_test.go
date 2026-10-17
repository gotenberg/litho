package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestStamps_RefuseAnOutOfRangePage holds every stamp surface of the CLI, the
// three commands and the three pipe stages, to the refusal the library makes
// for a page outside the document: a non-zero exit, a message on stderr, and
// no output file. The control stamps every page of the same file, so a surface
// that failed for another reason fails the control too.
func TestStamps_RefuseAnOutOfRangePage(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	overlay := writeTempPDF(t, untaggedTextPDF())
	img := filepath.Join(t.TempDir(), "stamp.png")
	if err := os.WriteFile(img, onePixelPNG(t), 0o600); err != nil {
		t.Fatalf("writing the PNG fixture: %v", err)
	}

	surfaces := []struct {
		name string
		args func(out string, pages []string) []string
	}{
		{"stamp-text", func(out string, pages []string) []string {
			return append(append([]string{"stamp-text", "-text", "X"}, pages...), "-o", out, src)
		}},
		{"stamp-image", func(out string, pages []string) []string {
			return append(append([]string{"stamp-image", "-image", img}, pages...), "-o", out, src)
		}},
		{"stamp-pdf", func(out string, pages []string) []string {
			return append(append([]string{"stamp-pdf", "-stamp", overlay}, pages...), "-o", out, src)
		}},
		{"pipe stamp-text", func(out string, pages []string) []string {
			return append(append([]string{"pipe", src, "stamp-text", "-text", "X"}, pages...), "-o", out)
		}},
		{"pipe stamp-image", func(out string, pages []string) []string {
			return append(append([]string{"pipe", src, "stamp-image", "-image", img}, pages...), "-o", out)
		}},
		{"pipe stamp-pdf", func(out string, pages []string) []string {
			return append(append([]string{"pipe", src, "stamp-pdf", "-stamp", overlay}, pages...), "-o", out)
		}},
	}
	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "stamped.pdf")
			var stdout, stderr bytes.Buffer
			args := s.args(out, []string{"-pages", "99"})
			if rc := run(context.Background(), args, &stdout, &stderr); rc == exitOK {
				t.Fatalf("%v succeeded; stderr: %s", args, stderr.String())
			}
			if stderr.Len() == 0 {
				t.Errorf("%v failed silently: nothing on stderr", args)
			}
			if _, err := os.Stat(out); err == nil {
				t.Errorf("%s exists: a refused stamp must not leave an output file", out)
			}

			stdout.Reset()
			stderr.Reset()
			args = s.args(out, nil)
			if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
				t.Fatalf("control %v: exit %d, stderr: %s", args, rc, stderr.String())
			}
		})
	}
}
