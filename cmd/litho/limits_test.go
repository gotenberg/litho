package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// TestParseByteSize pins the size syntax of the byte-count limit flags: a
// plain integer or one followed by KiB, MiB or GiB.
func TestParseByteSize(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]int64{
		"0":      0,
		"1024":   1024,
		"1KiB":   1 << 10,
		"256MiB": 256 << 20,
		"4GiB":   4 << 30,
	} {
		if got, err := parseByteSize(in); err != nil || got != want {
			t.Errorf("parseByteSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "MiB", "1XiB", "1mib", "1.5MiB", "-1", "-1KiB", "9999999999999GiB"} {
		if got, err := parseByteSize(in); err == nil {
			t.Errorf("parseByteSize(%q) = %d, want an error", in, got)
		}
	}
	if _, err := parseByteSize("-2MiB"); !errors.Is(err, errNegative) {
		t.Errorf("parseByteSize(%q): err = %v, want errNegative", "-2MiB", err)
	}
}

// TestParseCount pins the plain-integer syntax of the pixel and count flags,
// which take no suffix.
func TestParseCount(t *testing.T) {
	t.Parallel()

	if got, err := parseCount("20"); err != nil || got != 20 {
		t.Errorf("parseCount(%q) = %d, %v; want 20", "20", got, err)
	}
	if got, err := parseCount64("200000000"); err != nil || got != 200_000_000 {
		t.Errorf("parseCount64(%q) = %d, %v; want 200000000", "200000000", got, err)
	}
	for _, in := range []string{"", "1KiB", "ten", "1e6"} {
		if _, err := parseCount(in); err == nil {
			t.Errorf("parseCount(%q): want an error", in)
		}
		if _, err := parseCount64(in); err == nil {
			t.Errorf("parseCount64(%q): want an error", in)
		}
	}
	if _, err := parseCount("-1"); !errors.Is(err, errNegative) {
		t.Errorf("parseCount(%q): err = %v, want errNegative", "-1", err)
	}
}

// nestedImagePDF builds a page that paints /Fm1, which paints /Fm2, which
// shows the image /Deep two forms below the page.
func nestedImagePDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Fm1 5 0 R >> >> >>",
		"<< /Length 7 >>\nstream\n/Fm1 Do\nendstream",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /XObject << /Fm2 6 0 R >> >> /Length 7 >>\nstream\n/Fm2 Do\nendstream",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /XObject << /Deep 7 0 R >> >> /Length 8 >>\nstream\n/Deep Do\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x00\nendstream",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

// TestRun_LimitFlags pins the resource-limit flags end to end: both spellings
// reach the library, which lists the image two forms down under the default
// depth and not under --max-form-depth 1, and a bad value exits 1 naming the
// flag and, past a ceiling, the budget too.
func TestRun_LimitFlags(t *testing.T) {
	path := writeTempPDF(t, nestedImagePDF())

	for _, tt := range []struct {
		name      string
		args      []string
		want      int
		listsDeep bool
		stderr    string
	}{
		{"no flag", []string{"images", path}, exitOK, true, ""},
		{"space form", []string{"--max-form-depth", "1", "images", path}, exitOK, false, ""},
		{"equals form", []string{"--max-form-depth=1", "images", path}, exitOK, false, ""},
		{"a depth that reaches it", []string{"--max-form-depth", "2", "images", path}, exitOK, true, ""},
		{"zero keeps the default", []string{"--max-form-depth", "0", "images", path}, exitOK, true, ""},
		{"every flag at once", []string{"--max-stream-bytes", "64MiB", "--max-image-pixels", "1000000", "--max-embedded-file-bytes=1MiB", "--max-form-depth", "1", "--max-cached-objects", "5", "--timeout", "30s", "images", path}, exitOK, false, ""},
		{"bad size", []string{"--max-stream-bytes", "64MB", "images", path}, exitError, false, "invalid --max-stream-bytes"},
		{"negative count", []string{"--max-cached-objects=-1", "images", path}, exitError, false, "invalid --max-cached-objects"},
		{"a suffix on a pixel count", []string{"--max-image-pixels", "1MiB", "images", path}, exitError, false, "invalid --max-image-pixels"},
		{"missing value", []string{"--max-form-depth"}, exitError, false, "--max-form-depth needs a number"},
		{"past the ceiling", []string{"--max-form-depth", "1000", "images", path}, exitError, false, "ceiling"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			rc := run(context.Background(), tt.args, &stdout, &stderr)
			if rc != tt.want {
				t.Fatalf("run(%q) = %d, want %d. stderr: %q", tt.args, rc, tt.want, stderr.String())
			}
			if got := strings.Contains(stdout.String(), "Deep"); got != tt.listsDeep {
				t.Errorf("run(%q) listed /Deep = %v, want %v. stdout: %q", tt.args, got, tt.listsDeep, stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("run(%q) stderr = %q, want it to hold %q", tt.args, stderr.String(), tt.stderr)
			}
		})
	}
}

// TestLimitFlags_AreAdvertised keeps the help honest: every resource-limit
// flag appears in it, with the default the library applies.
func TestLimitFlags_AreAdvertised(t *testing.T) {
	var usage bytes.Buffer
	printUsage(&usage)

	for _, f := range limitFlags {
		if !strings.Contains(usage.String(), "--"+f.name+" N") {
			t.Errorf("printUsage does not mention --%s", f.name)
		}
	}
	for _, want := range []string{"(default 256MiB)", "(default 100000000)", "(default 50MiB)", "(default 10)", "(default 50000)", "(default 50000000)"} {
		if !strings.Contains(usage.String(), want) {
			t.Errorf("printUsage does not show %s", want)
		}
	}
}

// TestRun_LimitFlagsPastTheirCeiling pins that a resource-limit flag past its
// ceiling is refused while the flags are parsed, before any document opens,
// with the error [pdf.Limits.Validate] returns for that budget, and that each
// ceiling itself is accepted. The document path does not exist, so any message
// naming it would mean the command ran.
func TestRun_LimitFlagsPastTheirCeiling(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.pdf")

	for _, tt := range []struct {
		flag, value string
		limits      pdf.Limits
	}{
		{"max-stream-bytes", "4294967297", pdf.Limits{MaxStreamBytes: 4<<30 + 1}},
		{"max-stream-bytes", "5GiB", pdf.Limits{MaxStreamBytes: 5 << 30}},
		{"max-image-pixels", "1073741825", pdf.Limits{MaxImagePixels: 1<<30 + 1}},
		{"max-embedded-file-bytes", "5GiB", pdf.Limits{MaxEmbeddedFileBytes: 5 << 30}},
		{"max-form-depth", "257", pdf.Limits{MaxFormDepth: 257}},
		{"max-cached-objects", "8388608", pdf.Limits{MaxCachedObjects: 8_388_608}},
		{"max-render-pixels", "1073741825", pdf.Limits{MaxRenderPixels: 1<<30 + 1}},
	} {
		t.Run(tt.flag+"="+tt.value, func(t *testing.T) {
			verr := tt.limits.Validate()
			if !errors.Is(verr, pdf.ErrInvalidLimits) {
				t.Fatalf("Validate(%+v) = %v, want ErrInvalidLimits", tt.limits, verr)
			}
			want := fmt.Sprintf("invalid --%s %q: %v\n", tt.flag, tt.value, verr)

			for _, args := range [][]string{
				{"--" + tt.flag, tt.value, "info", missing},
				{"--" + tt.flag + "=" + tt.value, "info", missing},
				// A valid flag first: the refusal still names this one.
				{"--max-form-depth", "3", "--" + tt.flag, tt.value, "info", missing},
			} {
				var stdout, stderr bytes.Buffer
				if rc := run(context.Background(), args, &stdout, &stderr); rc != exitError {
					t.Errorf("run(%q) = %d, want %d", args, rc, exitError)
				}
				if stderr.String() != want {
					t.Errorf("run(%q) stderr = %q, want %q", args, stderr.String(), want)
				}
				if stdout.Len() != 0 {
					t.Errorf("run(%q) wrote %q to stdout, want nothing", args, stdout.String())
				}
			}
		})
	}

	// The ceilings pdf.Limits documents, lower on a 32-bit platform.
	bytesCeiling := strconv.FormatInt(min(4<<30, math.MaxInt), 10)
	path := writeTempPDF(t, nestedImagePDF())
	for _, args := range [][]string{
		{"--max-stream-bytes", bytesCeiling, "images", path},
		{"--max-image-pixels", strconv.FormatInt(min(1<<30, math.MaxInt/4), 10), "images", path},
		{"--max-embedded-file-bytes", bytesCeiling, "images", path},
		{"--max-form-depth", "256", "images", path},
		{"--max-cached-objects", "8388607", "images", path},
		{"--max-render-pixels", strconv.FormatInt(min(1<<30, math.MaxInt/16), 10), "images", path},
	} {
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
			t.Errorf("run(%q) = %d, want %d at the ceiling. stderr: %q", args, rc, exitOK, stderr.String())
		}
	}
}
