package ooxml

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

func TestOpen_Errors(t *testing.T) {
	t.Parallel()
	noTypes := minimalPackage()
	delete(noTypes, "[Content_Types].xml")
	for _, tc := range []struct {
		name string
		data []byte
		want error
	}{
		{"compound file", append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 512)...), office.ErrEncrypted},
		{"not a zip", []byte("hello"), office.ErrMalformed},
		{"no content types", buildZip(t, noTypes), office.ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Open(t.Context(), tc.data); !errors.Is(err, tc.want) {
				t.Fatalf("Open = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPackage_Relationships(t *testing.T) {
	t.Parallel()
	p, err := Open(t.Context(), buildZip(t, minimalPackage()))
	if err != nil {
		t.Fatal(err)
	}
	main, err := p.MainPart(t.Context())
	if err != nil || main != "word/document.xml" {
		t.Fatalf("MainPart = %q, %v", main, err)
	}
	if ct := p.ContentType("/WORD/document.xml"); ct != "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml" {
		t.Errorf("ContentType = %q", ct)
	}
	if ct := p.ContentType("docProps/core.xml"); ct != "application/xml" {
		t.Errorf("default ContentType = %q", ct)
	}
	img, ok := p.RelationshipByID(t.Context(), main, "rId1")
	if !ok || img.Target != "word/media/a.png" || img.External {
		t.Errorf("image relationship = %+v, %v", img, ok)
	}
	link, ok := p.RelationshipByID(t.Context(), main, "rId2")
	if !ok || link.Target != "https://example.com/" || !link.External {
		t.Errorf("hyperlink relationship = %+v, %v", link, ok)
	}
	if _, err := p.Read("word/missing.xml"); !IsMissing(err) {
		t.Errorf("Read missing = %v", err)
	}
	cp, err := p.CoreProperties(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cp.Title != "A title" || cp.Creator != "Ann" || cp.Keywords != "k1, k2" {
		t.Errorf("CoreProperties = %+v", cp)
	}
}

func TestResolveTarget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ source, target, want string }{
		{"word/document.xml", "media/image1.png", "word/media/image1.png"},
		{"word/document.xml", "../customXml/item1.xml", "customXml/item1.xml"},
		{"word/document.xml", "/word/styles.xml", "word/styles.xml"},
		{"", "word/document.xml", "word/document.xml"},
		{"word/document.xml", "../../../etc/passwd", "etc/passwd"},
	} {
		if got := ResolveTarget(tc.source, tc.target); got != tc.want {
			t.Errorf("ResolveTarget(%q, %q) = %q, want %q", tc.source, tc.target, got, tc.want)
		}
	}
}

// TestPackage_PartsStopOnCancel checks that the content types and a
// relationships part stop parsing on a cancelled context, within the
// first elements of a part of any length.
func TestPackage_PartsStopOnCancel(t *testing.T) {
	t.Parallel()
	const n = 50_000
	var types, rels strings.Builder
	types.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	rels.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := range n {
		fmt.Fprintf(&types, `<Default Extension="x%d" ContentType="application/xml"/>`, i)
		fmt.Fprintf(&rels, `<Relationship Id="r%d" Type="%s" Target="media/%d.png"/>`, i, RelImage, i)
	}
	types.WriteString(`</Types>`)
	rels.WriteString(`</Relationships>`)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	big := minimalPackage()
	big["[Content_Types].xml"] = types.String()
	if _, err := Open(cancelled, buildZip(t, big)); !errors.Is(err, context.Canceled) {
		t.Errorf("Open on a cancelled context = %v, want context.Canceled", err)
	}

	pkg := minimalPackage()
	pkg["word/_rels/document.xml.rels"] = rels.String()
	p, err := Open(t.Context(), buildZip(t, pkg))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Relationships(cancelled, "word/document.xml"); !errors.Is(err, context.Canceled) {
		t.Errorf("Relationships on a cancelled context = %v, want context.Canceled", err)
	}
	if got := p.RelatedPart(cancelled, "word/document.xml", RelImage); got != "" {
		t.Errorf("RelatedPart on a cancelled context = %q, want none", got)
	}
}
