package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// signedTwoPagePDF builds a two-page document whose first page presents
// "approval", a signed signature field that /Perms /DocMDP names, and whose
// catalog /Perms /UR3 names a usage rights signature no field holds.
func signedTwoPagePDF() []byte {
	const ap = "0 0 1 rg 0 0 200 50 re f"
	sig := "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached " +
		"/ByteRange [0 100 200 300] /Contents <30820001> >>"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /SigFlags 3 >> " +
			"/Perms << /DocMDP 6 0 R /UR3 8 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /V 6 0 R /Rect [50 700 250 750] /F 4 " +
			"/P 3 0 R /AP << /N 7 0 R >> >>",
		sig,
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 50] /Length %d >>\nstream\n%s\nendstream", len(ap), ap),
		sig,
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// signatureWarnings are the lines every command writing the output of
// [signedTwoPagePDF] without its signatures prints.
//
//nolint:gochecknoglobals // static expectation
var signatureWarnings = []string{
	`warning: the output does not carry certification signature "approval": ` + signatureDropReason,
	"warning: the output does not carry the usage rights signature: " + signatureDropReason,
}

// TestCommands_WarnWhenSignaturesAreDropped pins that each command whose
// output leaves the input's signatures out says so on stderr, once per
// signature, and still succeeds.
//
// A full rewrite, a split and a merge build a new file that the input's
// signatures do not cover (ISO 32000-1, 12.8.1), so litho draws each signed
// widget into its page and leaves the signature out. Every one of these
// commands used to exit 0 in silence, handing back an unsigned document from
// a signed one.
func TestCommands_WarnWhenSignaturesAreDropped(t *testing.T) {
	t.Parallel()
	src := writeTempPDF(t, signedTwoPagePDF())

	for _, tc := range []struct {
		name string
		args func(out string) []string
	}{
		{"delete", func(out string) []string { return []string{"delete", src, "--pages", "2", "-o", out} }},
		{"flatten-form", func(out string) []string { return []string{"flatten-form", src, "-o", out} }},
		{"encrypt", func(out string) []string { return []string{"encrypt", src, "-user-password", "pw", "-o", out} }},
		{"split", func(out string) []string { return []string{"split", src, "--pages", "1", "-o", out} }},
		{"merge", func(out string) []string { return []string{"merge", src, src, "-o", out} }},
		{"pipe", func(out string) []string { return []string{"pipe", src, "delete", "--pages", "2", "-o", out} }},
		{"pipe split", func(out string) []string {
			return []string{"pipe", src, "rotate", "-angle", "90", "split", "--pages", "1", "-o", out}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := filepath.Join(t.TempDir(), "out.pdf")
			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), tc.args(out), &stdout, &stderr); rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
			}
			var got []string
			for line := range strings.Lines(stderr.String()) {
				if strings.Contains(line, "does not carry") {
					got = append(got, strings.TrimSuffix(line, "\n"))
				}
			}
			if strings.Join(got, "\n") != strings.Join(signatureWarnings, "\n") {
				t.Errorf("signature warnings = %q, want %q", got, signatureWarnings)
			}
		})
	}
}

// TestRotate_KeepsSignaturesWithoutWarning pins that a command writing an
// incremental update, which keeps every signature, prints no signature
// warning.
func TestRotate_KeepsSignaturesWithoutWarning(t *testing.T) {
	t.Parallel()
	src := writeTempPDF(t, signedTwoPagePDF())
	out := filepath.Join(t.TempDir(), "out.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"rotate", src, "-angle", "90", "-o", out}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("rotate: exit %d, stderr: %s", rc, stderr.String())
	}
	if strings.Contains(stderr.String(), "does not carry") {
		t.Errorf("rotate warned of a dropped signature: %q", stderr.String())
	}
}

// TestDescribeSignature pins the name each kind of dropped signature goes by.
func TestDescribeSignature(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		sig  pdf.DroppedSignature
		want string
	}{
		{pdf.DroppedSignature{Field: "a.b"}, `signature "a.b"`},
		{pdf.DroppedSignature{Field: "cert", Perms: "DocMDP"}, `certification signature "cert"`},
		{pdf.DroppedSignature{Perms: "UR3"}, "the usage rights signature"},
		{pdf.DroppedSignature{Perms: "DocMDP"}, "the certification signature"},
		{pdf.DroppedSignature{}, "the signature of an unnamed field"},
	} {
		if got := describeSignature(tc.sig); got != tc.want {
			t.Errorf("describeSignature(%+v) = %q, want %q", tc.sig, got, tc.want)
		}
	}
}
