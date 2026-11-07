package ooxml

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

func TestParse_AlternateContent(t *testing.T) {
	t.Parallel()
	const doc = `<w:r xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" xmlns:x="urn:unknown">` +
		`<mc:AlternateContent><mc:Choice Requires="x"><w:t>unknown</w:t></mc:Choice><mc:Choice Requires="wps"><w:t>shape</w:t></mc:Choice><mc:Fallback><w:t>fallback</w:t></mc:Fallback></mc:AlternateContent></w:r>`
	for _, tc := range []struct {
		name       string
		understood []string
		want       string
	}{
		{"choice", []string{NSWPS}, "shape"},
		{"fallback", nil, "fallback"},
	} {
		root, err := Parse(context.Background(), []byte(doc), tc.understood)
		if err != nil {
			t.Fatal(err)
		}
		if len(root.Children) != 1 || root.Children[0].Text != tc.want {
			t.Errorf("%s: children %+v, want one w:t %q", tc.name, root.Children, tc.want)
		}
	}
}

func TestParse_Strict(t *testing.T) {
	t.Parallel()
	root, err := Parse(context.Background(), []byte(`<w:document xmlns:w="http://purl.oclc.org/ooxml/wordprocessingml/main"><w:body w:val="1"/></w:document>`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !root.Is(NSW, "document") || root.Child(NSW, "body") == nil {
		t.Fatalf("strict namespace not mapped: %s", root.Space)
	}
	if v, ok := root.Child(NSW, "body").Attr("val"); !ok || v != "1" {
		t.Errorf("Attr(val) = %q, %v", v, ok)
	}
}

func TestParse_Refuses(t *testing.T) {
	t.Parallel()
	deep := strings.Repeat("<a>", MaxDepth+2) + strings.Repeat("</a>", MaxDepth+2)
	for _, tc := range []struct {
		name, doc string
		want      error
	}{
		{"doctype", `<!DOCTYPE x [<!ENTITY e SYSTEM "file:///etc/passwd">]><x>&e;</x>`, office.ErrMalformed},
		{"unknown entity", `<x>&e;</x>`, office.ErrMalformed},
		{"mismatched", `<x><y></x></y>`, office.ErrMalformed},
		{"unclosed", `<x><y>`, office.ErrMalformed},
		{"empty", ``, office.ErrMalformed},
		{"deep", deep, office.ErrInputTooLarge},
	} {
		if _, err := Parse(context.Background(), []byte(tc.doc), nil); !errors.Is(err, tc.want) {
			t.Errorf("%s: Parse = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestParse_Text(t *testing.T) {
	t.Parallel()
	root, err := Parse(context.Background(), []byte("<p>\n  <t xml:space=\"preserve\">  a &amp; b </t>\n  <t> </t></p>"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if root.Text != "" || len(root.Children) != 2 || root.Children[0].Text != "  a & b " || root.Children[1].Text != " " {
		t.Errorf("text = %q, %+v", root.Text, root.Children)
	}
}
