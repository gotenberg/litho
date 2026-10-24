package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// TestConvertColorsFlags_SelectTheTarget pins how the flags map onto the
// library's target and options, and the combinations they refuse.
func TestConvertColorsFlags_SelectTheTarget(t *testing.T) {
	t.Parallel()
	profile := filepath.Join(t.TempDir(), "p.icc")
	if err := os.WriteFile(profile, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := convertColorsFlags{target: "output-intent", intent: "RelativeColorimetric"}

	for _, tc := range []struct {
		name  string
		edit  func(*convertColorsFlags)
		want  pdf.ColorTarget
		opts  int
		error string
	}{
		{"default", func(*convertColorsFlags) {}, pdf.OutputIntentColorTarget(), 1, ""},
		{"cmyk with every option", func(f *convertColorsFlags) {
			f.target, f.intent, f.bpc, f.convertSpots, f.noOutputIntent = "cmyk", "Perceptual", true, true, true
			f.replaceOutputIntents, f.jpegQuality, f.kOnlyBlack = true, 85, true
		}, pdf.CMYKColorTarget(), 7, ""},
		{"rgb", func(f *convertColorsFlags) { f.target = "rgb" }, pdf.RGBColorTarget(), 1, ""},
		{"gray", func(f *convertColorsFlags) { f.target = "gray" }, pdf.GrayColorTarget(), 1, ""},
		{"icc", func(f *convertColorsFlags) { f.target, f.profile = "icc", profile }, pdf.ICCColorTarget([]byte("bytes")), 1, ""},
		{"icc without a profile", func(f *convertColorsFlags) { f.target = "icc" }, pdf.ColorTarget{}, 0, "needs --profile"},
		{"profile without icc", func(f *convertColorsFlags) { f.target, f.profile = "cmyk", profile }, pdf.ColorTarget{}, 0, "--profile names"},
		{"unknown target", func(f *convertColorsFlags) { f.target = "lab" }, pdf.ColorTarget{}, 0, "unknown target"},
		{"unknown intent", func(f *convertColorsFlags) { f.intent = "perceptual" }, pdf.ColorTarget{}, 0, "unknown rendering intent"},
	} {
		f := base
		tc.edit(&f)
		target, opts, err := f.colorTarget()
		if tc.error != "" {
			if err == nil || !strings.Contains(err.Error(), tc.error) {
				t.Errorf("%s: err = %v, want one containing %q", tc.name, err, tc.error)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !targetsEqual(target, tc.want) || len(opts) != tc.opts {
			t.Errorf("%s: target %+v with %d option(s), want %+v with %d", tc.name, target, len(opts), tc.want, tc.opts)
		}
	}
}

// targetsEqual compares two targets by what they print, which names their
// kind and profile bytes.
func targetsEqual(a, b pdf.ColorTarget) bool {
	return fmt.Sprintf("%+v", a) == fmt.Sprintf("%+v", b)
}

// unconvertibleImagePDF draws an RGB image whose samples do not decode, which
// a colour conversion cannot convert and refuses.
func unconvertibleImagePDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Im0 5 0 R >> >> >>",
		"<< /Length 16 >>\nstream\n1 0 0 rg /Im0 Do\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode /Length 9 >>\nstream\nnot flate\nendstream",
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

// TestConvertColors_ConvertsToCMYK pins a conversion end to end: exit 0, the
// report on stdout, and an output that paints in DeviceCMYK alone.
func TestConvertColors_ConvertsToCMYK(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"convert-colors", "--target", "cmyk", "-o", out, src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d; stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "to DeviceCMYK") || !strings.Contains(stdout.String(), "Output intent: added") {
		t.Errorf("stdout = %q", stdout.String())
	}
	data, err := os.ReadFile(out) //nolint:gosec // out is the test's own temp file
	if err != nil {
		t.Fatal(err)
	}
	r, err := pdf.Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	uses, err := r.ExtractColorSpaces(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range uses {
		if u.Family != "DeviceCMYK" {
			t.Errorf("the output paints in %s", u.Family)
		}
	}
}

// TestConvertColors_RefusalWritesNothing pins that a refused conversion
// exits with an error, says why, and leaves no output file, and that the
// encryption warning comes before the work, as for redact.
func TestConvertColors_RefusalWritesNothing(t *testing.T) {
	src := encryptTempPDF(t, writeTempPDF(t, unconvertibleImagePDF()), "s3cret")
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"convert-colors", "-p", "s3cret", "--target", "cmyk", "-o", out, src}, &stdout, &stderr)
	if rc != exitError {
		t.Fatalf("exit %d, want %d; stderr: %s", rc, exitError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "colour conversion refused") {
		t.Errorf("stderr does not say the conversion was refused: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), encryptionWarning) {
		t.Errorf("no encryption warning; stderr: %q", stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused conversion left %s behind (stat: %v)", out, err)
	}
}

// TestConvertColors_JPEGQuality pins --jpeg-quality end to end: a JPEG
// converted under it comes out as DCTDecode, and a quality out of range is
// refused with the reason and writes nothing.
func TestConvertColors_JPEGQuality(t *testing.T) {
	src := writeTempPDF(t, jpegImagePDF(t))
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"convert-colors", "--target", "gray", "--jpeg-quality", "80", "-o", out, src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d; stderr: %s", rc, stderr.String())
	}
	data, err := os.ReadFile(out) //nolint:gosec // out is the test's own temp file
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("/DCTDecode")) {
		t.Errorf("the converted image is not a JPEG; stdout: %q", stdout.String())
	}

	bad := filepath.Join(t.TempDir(), "bad.pdf")
	stderr.Reset()
	if rc := run(context.Background(), []string{"convert-colors", "--target", "gray", "--jpeg-quality", "101", "-o", bad, src}, &stdout, &stderr); rc != exitError {
		t.Fatalf("exit %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr.String(), "JPEG quality 101") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Errorf("a refused conversion left %s behind (stat: %v)", bad, err)
	}
}

// jpegImagePDF draws one RGB JPEG of colour gradients.
func jpegImagePDF(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: uint8(x + y), A: 255})
		}
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 64 64] /Contents 4 0 R /Resources << /XObject << /Im0 5 0 R >> >> >>",
		"<< /Length 28 >>\nstream\nq 64 0 0 64 0 0 cm /Im0 Do Q\nendstream",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 64 /Height 64 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", jpg.Len(), jpg.String()),
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

// TestConvertColors_Usage pins the usage line of a call missing its output.
func TestConvertColors_Usage(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"convert-colors", "in.pdf"}, &stdout, &stderr); rc != exitError {
		t.Fatalf("exit %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr.String(), "Usage: litho convert-colors") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// TestConvertColors_OutputIntentConflict pins --replace-output-intents end to
// end: without it a CMYK target under a PDF/X intent over another CMYK
// profile exits 1 naming the printing condition and writes nothing, and with
// it the intent is pointed at the target and reported.
func TestConvertColors_OutputIntentConflict(t *testing.T) {
	profile, err := os.ReadFile("../../pkg/internal/graphics/icc/testdata/v4-cmyk-lut.icc")
	if err != nil {
		t.Fatal(err)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (FOGRA39) /DestOutputProfile 5 0 R >>] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		"<< /Length 22 >>\nstream\n1 0 0 rg 0 0 9 9 re f\nendstream",
		fmt.Sprintf("<< /N 4 /Length %d >>\nstream\n%s\nendstream", len(profile), profile),
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
	src := writeTempPDF(t, b.Bytes())

	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"convert-colors", "--target", "cmyk", "-o", out, src}, &stdout, &stderr); rc != exitError {
		t.Fatalf("exit %d, want %d; stderr: %s", rc, exitError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "FOGRA39") {
		t.Errorf("stderr does not name the printing condition: %q", stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused conversion left %s behind (stat: %v)", out, err)
	}

	stdout.Reset()
	stderr.Reset()
	if rc := run(context.Background(), []string{"convert-colors", "--target", "cmyk", "--replace-output-intents", "-o", out, src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d; stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "pointed at the target profile: FOGRA39") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

// TestConvertColorsReport_Overprint pins how the report states what the
// conversion did to overprinting paintings, in text and in JSON.
func TestConvertColorsReport_Overprint(t *testing.T) {
	t.Parallel()
	report := &pdf.ColorReport{Target: "DeviceCMYK", OverprintPreserved: 3, OverprintStates: 1, OverprintChanged: 2}
	var text, js, errs bytes.Buffer
	if code := printConvertColorsReport(&text, &errs, "text", "in.pdf", report); code != exitOK {
		t.Fatalf("text: exit %d, %s", code, errs.String())
	}
	for _, want := range []string{"Kept 3 overprinting painting(s) as they print, writing 1 graphics state(s)", "Warning: 2 overprinting painting(s) print otherwise"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text report %q lacks %q", text.String(), want)
		}
	}
	if code := printConvertColorsReport(&js, &errs, "json", "in.pdf", report); code != exitOK {
		t.Fatalf("json: exit %d, %s", code, errs.String())
	}
	for _, want := range []string{`"overprintPreserved": 3`, `"overprintStates": 1`, `"overprintChanged": 2`} {
		if !strings.Contains(js.String(), want) {
			t.Errorf("JSON report %s lacks %s", js.String(), want)
		}
	}
}

// TestConvertColors_ReportsTheProfiles pins that the report names where the
// target profile came from, in both formats: the built-in one by default,
// the --cmyk-profile file as the caller's.
func TestConvertColors_ReportsTheProfiles(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	profile := filepath.Join("..", "..", "pkg", "internal", "graphics", "icc", "profiles", "CGATS001Compat-v2-micro.icc")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "builtin"},
		{[]string{"--cmyk-profile", profile}, "caller"},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		var stdout, stderr bytes.Buffer
		args := append([]string{"convert-colors", "--target", "cmyk", "-o", out}, tc.args...)
		if rc := run(context.Background(), append(args, src), &stdout, &stderr); rc != exitOK {
			t.Fatalf("exit %d; stderr: %s", rc, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Target profile: "+tc.want+"\n") {
			t.Errorf("%v: stdout = %q", tc.args, stdout.String())
		}

		stdout.Reset()
		if rc := run(context.Background(), append(append(args, "-f", "json"), src), &stdout, &stderr); rc != exitOK {
			t.Fatalf("exit %d; stderr: %s", rc, stderr.String())
		}
		var result convertColorsResult
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.TargetProfile != tc.want {
			t.Errorf("%v: targetProfile = %q, want %q", tc.args, result.TargetProfile, tc.want)
		}
	}
}
