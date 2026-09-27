package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// untaggedTextPDF builds a single untagged page with a heading and a body line,
// so autotag has real text to synthesize a structure tree from.
func untaggedTextPDF() []byte {
	return singlePagePDF("", contentStream(
		"BT /F1 24 Tf 72 700 Td (Document Heading) Tj ET\n"+
			"BT /F1 12 Tf 72 678 Td (A body line of text for the paragraph.) Tj ET\n"))
}

// contentStream is the body of an unfiltered stream object holding content.
func contentStream(content string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
}

// singlePagePDF builds a one-page document with a classic xref table.
// catalogExtra is appended to the catalog dictionary, and contents is the body
// of the object the page's /Contents names.
func singlePagePDF(catalogExtra, contents string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R " + catalogExtra + ">>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		contents,
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xrefStart := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objs)+1)
	buf.WriteString("0000000000 65535 f\r\n")
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xrefStart)
	return buf.Bytes()
}

// TestAutotag_TagsUntaggedPDF runs the autotag command on an untagged text page
// and confirms it produces a tagged output: a non-empty PDF carrying the
// synthesized /StructTreeRoot and the /MarkInfo marker.
func TestAutotag_TagsUntaggedPDF(t *testing.T) {
	path := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "tagged.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"autotag", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit code: got %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected a non-empty tagged output")
	}
	got := string(data)
	if !bytes.Contains(data, []byte("/StructTreeRoot")) {
		t.Errorf("output is missing /StructTreeRoot; autotag did not synthesize a tree:\n%s", got)
	}
	if !bytes.Contains(data, []byte("/MarkInfo")) {
		t.Errorf("output is missing /MarkInfo; document not marked as tagged")
	}
}

// TestAutotag_MissingOutput reports a usage error when -o is absent.
func TestAutotag_MissingOutput(t *testing.T) {
	path := writeTempPDF(t, untaggedTextPDF())
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"autotag", path}, &stdout, &stderr)
	if rc != exitError {
		t.Errorf("exit code: got %d, want %d", rc, exitError)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("Usage: litho autotag")) {
		t.Errorf("expected autotag usage on stderr, got: %q", stderr.String())
	}
}

// autotagSkipCase is a document AutoTag leaves alone and the reason the
// notice gives for it.
type autotagSkipCase struct {
	desc   string
	pdf    []byte
	reason string
}

// autotagSkipCases holds one document per reason [pdf.AutoTagReport.Skipped]
// can carry.
func autotagSkipCases() []autotagSkipCase {
	text := "BT /F1 12 Tf 72 700 Td (A line of text.) Tj ET"
	return []autotagSkipCase{
		{
			// ISO 32000-1, 14.7.1: /MarkInfo /Marked true declares a tagged
			// document, so AutoTag keeps the producer's structure.
			desc:   "already tagged",
			pdf:    singlePagePDF("/MarkInfo << /Marked true >> ", contentStream(text)),
			reason: "the document is already tagged",
		},
		{
			// ISO 32000-1, 14.6: marking the text again would nest the BDC
			// sequence the page already opens.
			desc:   "marked content",
			pdf:    singlePagePDF("", contentStream("/P << /MCID 0 >> BDC "+text+" EMC")),
			reason: "page 1: the page content is already marked",
		},
		{
			// ISO 32000-1, 7.7.3.3, Table 30: /Contents is a stream or an
			// array of streams. A dictionary is neither, so the page's content
			// cannot be assembled.
			desc:   "unreadable content",
			pdf:    singlePagePDF("", "<< /Kind /NotAStream >>"),
			reason: "page 1: the page content cannot be read",
		},
		{
			desc:   "nothing to tag",
			pdf:    singlePagePDF("", contentStream("72 72 100 100 re f")),
			reason: "no page draws content to tag",
		},
	}
}

// TestAutotag_ReportsWhyItAddedNoTree pins the notice for a skipped AutoTag.
//
// A skip fails nothing and the output is written, so before the notice the
// command exited 0 in silence on every document below: a caller asking for a
// tagged PDF got the input back with no word that it was not tagged.
func TestAutotag_ReportsWhyItAddedNoTree(t *testing.T) {
	for _, tc := range autotagSkipCases() {
		t.Run(tc.desc, func(t *testing.T) {
			src := writeTempPDF(t, tc.pdf)
			out := filepath.Join(t.TempDir(), "out.pdf")

			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), []string{"autotag", "-o", out, src}, &stdout, &stderr); rc != exitOK {
				t.Fatalf("exit %d, want %d: a skip fails nothing. stderr: %s", rc, exitOK, stderr.String())
			}
			want := autotagSkipNotice + ": " + tc.reason
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing: the notice belongs on stderr", stdout.String())
			}
			if _, err := os.Stat(out); err != nil {
				t.Errorf("the output was not written: %v", err)
			}
		})
	}
}

// TestPipe_AutotagStageReportsWhyItAddedNoTree is the pipe half of
// [TestAutotag_ReportsWhyItAddedNoTree]: the stage must report what the
// standalone command reports.
//
// The split rows put a restructuring stage after autotag. The Writer then
// applies the edit when the split materializes it, not at the final write, so
// they check the report is read after the edit ran, whichever stage ran it.
func TestPipe_AutotagStageReportsWhyItAddedNoTree(t *testing.T) {
	for _, tc := range autotagSkipCases() {
		for _, tail := range [][]string{nil, {"split", "-pages", "1"}} {
			name := tc.desc
			if tail != nil {
				name += ", then split"
			}
			t.Run(name, func(t *testing.T) {
				src := writeTempPDF(t, tc.pdf)
				out := filepath.Join(t.TempDir(), "out.pdf")

				args := append([]string{"pipe", src, "autotag"}, tail...)
				args = append(args, "-o", out)
				var stdout, stderr bytes.Buffer
				if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
					t.Fatalf("exit %d, want %d: a skip fails nothing. stderr: %s", rc, exitOK, stderr.String())
				}
				want := autotagSkipNotice + ": " + tc.reason
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
				}
				if stdout.Len() != 0 {
					t.Errorf("stdout = %q, want nothing: pipe prints nothing but the document", stdout.String())
				}
			})
		}
	}
}

// TestAutotag_NoNoticeWhenItTags is the control: a document AutoTag tags draws
// no notice on either surface. Without it, a notice printed unconditionally
// would pass both tests above.
func TestAutotag_NoNoticeWhenItTags(t *testing.T) {
	for _, tc := range []struct {
		desc string
		args func(src, out string) []string
	}{
		{"standalone", func(src, out string) []string { return []string{"autotag", "-o", out, src} }},
		{"pipe", func(src, out string) []string { return []string{"pipe", src, "autotag", "-o", out} }},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			src := writeTempPDF(t, untaggedTextPDF())
			out := filepath.Join(t.TempDir(), "out.pdf")

			var stdout, stderr bytes.Buffer
			if rc := run(context.Background(), tc.args(src, out), &stdout, &stderr); rc != exitOK {
				t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
			}
			if strings.Contains(stderr.String(), autotagSkipNotice) {
				t.Errorf("stderr = %q, want no skip notice for a document AutoTag tagged", stderr.String())
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("read output: %v", err)
			}
			if !bytes.Contains(data, []byte("/StructTreeRoot")) {
				t.Error("the output carries no /StructTreeRoot, so AutoTag did not tag and this control proves nothing")
			}
		})
	}
}
