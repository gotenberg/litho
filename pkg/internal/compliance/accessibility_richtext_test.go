package compliance

import (
	"context"
	"testing"
)

// TestUA2MarkupAnnotRichText pins ISO 14289-2 §8.9.2.3 t2, which compares the
// text of a markup annotation's /RC with its /Contents.
//
// The comparison is exact on both sides. veraPDF parses /RC as an XML document
// and concatenates its text nodes, so markup and attributes drop out while
// every space survives; the whitespace cases below are the pins that keep a
// well-meaning TrimSpace out of the implementation.
func TestUA2MarkupAnnotRichText(t *testing.T) {
	ctx := context.Background()

	// Build a UA-2 document holding one Square markup annotation. An empty rc
	// leaves /RC out entirely, which takes the annotation out of the rule.
	build := func(contents, rc string) []Violation {
		annot := "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /Contents (" + contents + ")"
		if rc != "" {
			annot += " /RC (" + rc + ")"
		}
		annot += " >>"
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
			annot,
		)
		return checkPDFUA2AnnotationConstraints(ctx, doc, cat, "PDF/UA-2")
	}

	fires := func(t *testing.T, name, contents, rc string, want bool) {
		t.Helper()
		got := hasRule(build(contents, rc), Rule_14289_2_8_9_2_3_t02)
		if got != want {
			t.Errorf("%s: fired=%v, want %v", name, got, want)
		}
	}

	fires(t, "no /RC at all", "the plain text", "", false)
	fires(t, "matching text", "the plain text", "<p>the plain text</p>", false)
	fires(t, "differing text", "the plain text", "<p>something else</p>", true)

	// Markup and attributes contribute nothing; nested elements concatenate
	// with no separator.
	fires(t, "nested elements", "the plain text", "<p>the <b>plain</b> text</p>", false)
	fires(t, "split elements", "the plain text", "<p>the plain</p><p> text</p>", true)
	fires(t, "attribute text", "the plain text",
		"<p title='ignored'>the plain text</p>", false)

	// Whitespace is significant on both sides.
	fires(t, "leading whitespace kept", "the plain text",
		"<body>  <p>the plain text</p>  </body>", true)
	fires(t, "inner whitespace kept", "a b", "<p>a  b</p>", true)

	// A payload that is not one well-formed XML document yields no text, which
	// cannot equal /Contents, so the rule fires.
	fires(t, "plain text, not XML", "the plain text", "the plain text", true)
	fires(t, "two root elements", "ab", "<p>a</p><p>b</p>", true)
	fires(t, "unclosed element", "a", "<p>a", true)
}

// TestRichTextContent pins the text extraction itself against the document
// model veraPDF walks: comments are dropped, processing instruction data
// counts, and the XML declaration does not.
func TestRichTextContent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "simple", in: "<p>hello</p>", want: "hello"},
		{name: "declaration ignored", in: "<?xml version='1.0'?><p>hello</p>", want: "hello"},
		{name: "comment dropped", in: "<body><!--hidden--><p>hello</p></body>", want: "hello"},
		{name: "processing instruction counts", in: "<body><?foo hello?></body>", want: "hello"},
		{name: "entity resolved", in: "<p>a &amp; b</p>", want: "a & b"},
		{name: "cdata counts", in: "<p><![CDATA[hello]]></p>", want: "hello"},
		{name: "whitespace preserved", in: "<body>  <p>a  b</p> </body>", want: "  a  b "},
		{name: "empty", in: "", wantErr: true},
		{name: "no element", in: "bare text", wantErr: true},
		{name: "two roots", in: "<a>1</a><b>2</b>", wantErr: true},
		{name: "unclosed", in: "<a>1", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := richTextContent([]byte(tc.in))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("richTextContent(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("richTextContent(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("richTextContent(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
