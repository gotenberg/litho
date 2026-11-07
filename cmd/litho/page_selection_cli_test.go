package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// threePageTempPDF builds a three-page document by merging the one-page
// fixture with itself twice: an exclusion or an open range needs pages on
// both sides of the one it names.
func threePageTempPDF(t *testing.T) string {
	t.Helper()

	plain := writeTempPDF(t, untaggedTextPDF())
	merged := filepath.Join(t.TempDir(), "three-page.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(),
		[]string{"merge", "-o", merged, plain, plain, plain}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("merge: exit %d, stderr: %s", rc, stderr.String())
	}
	return merged
}

// TestPageSelection_ReachesTheCommands pins that the commands hand the
// -pages value to the parser untouched, so exclusions, open ranges and
// keywords mixed with pages work wherever a page selection does.
func TestPageSelection_ReachesTheCommands(t *testing.T) {
	src := threePageTempPDF(t)
	if got := pageCountOf(t, src); got != 3 {
		t.Fatalf("precondition: fixture has %d pages, want 3", got)
	}

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"delete every page but the first", []string{"delete", "-pages", "!1"}, 1},
		{"split from the second page on", []string{"split", "-pages", "2-"}, 2},
		{"pipe delete every page but the last", []string{"pipe", src, "delete", "-pages", "!last"}, 1},
		{"pipe split the odd pages after the first", []string{"pipe", src, "split", "-pages", "odd,!1"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.pdf")
			args := append(append([]string{}, tc.args...), "-o", out)
			if tc.args[0] != "pipe" {
				args = append(args, src)
			}

			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
				t.Fatalf("%v: exit %d, stderr: %s", args, rc, stderr.String())
			}
			if got := pageCountOf(t, out); got != tc.want {
				t.Errorf("%v produced %d page(s), want %d", args, got, tc.want)
			}
		})
	}
}

// TestPageSelection_EmptySelectionIsRefused pins that an exclusion leaving no
// page reaches the user as the parser's error, not as "every page".
func TestPageSelection_EmptySelectionIsRefused(t *testing.T) {
	src := onePageTempPDF(t)
	out := filepath.Join(t.TempDir(), "deleted.pdf")

	errOut := pageOpFails(t, out, []string{"delete", "-pages", "!1", "-o", out, src})
	if !strings.Contains(errOut, "selects no pages") {
		t.Errorf("stderr = %q, want it to say the selection is empty", strings.TrimSpace(errOut))
	}
}
