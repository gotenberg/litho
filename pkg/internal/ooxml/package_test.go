package ooxml

import (
	"context"
	"errors"
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
			if _, err := Open(tc.data); !errors.Is(err, tc.want) {
				t.Fatalf("Open = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPackage_Relationships(t *testing.T) {
	t.Parallel()
	p, err := Open(buildZip(t, minimalPackage()))
	if err != nil {
		t.Fatal(err)
	}
	main, err := p.MainPart()
	if err != nil || main != "word/document.xml" {
		t.Fatalf("MainPart = %q, %v", main, err)
	}
	if ct := p.ContentType("/WORD/document.xml"); ct != "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml" {
		t.Errorf("ContentType = %q", ct)
	}
	if ct := p.ContentType("docProps/core.xml"); ct != "application/xml" {
		t.Errorf("default ContentType = %q", ct)
	}
	img, ok := p.RelationshipByID(main, "rId1")
	if !ok || img.Target != "word/media/a.png" || img.External {
		t.Errorf("image relationship = %+v, %v", img, ok)
	}
	link, ok := p.RelationshipByID(main, "rId2")
	if !ok || link.Target != "https://example.com/" || !link.External {
		t.Errorf("hyperlink relationship = %+v, %v", link, ok)
	}
	if _, err := p.Read("word/missing.xml"); !IsMissing(err) {
		t.Errorf("Read missing = %v", err)
	}
	cp := p.CoreProperties(context.Background())
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
