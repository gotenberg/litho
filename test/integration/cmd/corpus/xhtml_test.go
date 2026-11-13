package main

import (
	"context"
	"path/filepath"
	"testing"
)

// TestStripStyleCDATA pins what an XML parser and an HTML parser agree on:
// the markers of a section in a style element go, the rest of the file
// stays byte for byte, and a marker an HTML parser reads otherwise keeps
// the file as it is and reports false.
func TestStripStyleCDATA(t *testing.T) {
	t.Parallel()

	const head = `<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml"><head>`
	for _, tc := range []struct {
		name, in, want string
		ok             bool
	}{
		{"no marker", head + `<style>p { color: green }</style></head><body><p>a</p></body></html>`, "", true},
		{
			"a sheet in a section", head + "<style type=\"text/css\"><![CDATA[\n p > a { color: green }\n]]></style></head><body/></html>",
			head + "<style type=\"text/css\">\n p > a { color: green }\n</style></head><body/></html>", true,
		},
		{
			"markers in CSS comments", head + `<style>/*<![CDATA[*/ p { color: green } /*]]>*/</style></head></html>`,
			head + `<style>/**/ p { color: green } /**/</style></head></html>`, true,
		},
		{
			"two style elements", head + `<style><![CDATA[a{}]]></style><style><![CDATA[b{}]]>c{}</style></head></html>`,
			head + `<style>a{}</style><style>b{}c{}</style></head></html>`, true,
		},
		{"a marker in a comment", head + `</head><body><!-- <![CDATA[ ]]> --></body></html>`, "", true},
		{"a marker in an attribute value", head + `</head><body><p title="]]>">a</p></body></html>`, "", true},
		{"a section in a script", head + `<script><![CDATA[ x() ]]></script></head></html>`, "", false},
		{"a section in the title", head + `<title><![CDATA[ a ]]></title></head></html>`, "", false},
		{"a section in the body", head + `</head><body><p><![CDATA[a <b> c]]></p></body></html>`, "", false},
		{"a closing marker in the body", head + `</head><body><p>a ]]> b</p></body></html>`, "", false},
		{"a closing marker in CSS with no opener", head + `<style>p { color: green } ]]></style></head></html>`, "", false},
		{"a section that does not close", head + `<style><![CDATA[ p { color: green }</style></head></html>`, "", false},
	} {
		got, ok := stripStyleCDATA([]byte(tc.in))
		want := tc.want
		if want == "" {
			want = tc.in
		}
		if ok != tc.ok || string(got) != want {
			t.Errorf("%s: got %q, %v; want %q, %v", tc.name, got, ok, want, tc.ok)
		}
	}
}

// TestReftestFeature_StripsXHTMLCDATA pins that the reftest feature hands
// the converter an XHTML test with its style sheets' CDATA markers
// stripped, the bytes of the file without them, and an HTML file as it is.
func TestReftestFeature_StripsXHTMLCDATA(t *testing.T) {
	t.Parallel()

	const (
		marked = `<html><head><style><![CDATA[ p { color: green } ]]></style></head><body><p> blue </p></body></html>`
		plain  = `<html><head><style> p { color: green } </style></head><body><p> blue </p></body></html>`
	)
	var got []string
	record := func(ctx context.Context, src []byte, s htmlSettings) ([]byte, error) {
		got = append(got, string(src))
		return colourStub(60, 45)(ctx, src, s)
	}
	f, root := newTestReftest(t, record, map[string]string{
		"a/test.xht": marked, "a/ref.xht": plain, "a/page.html": marked,
	},
		&reftestEntry{ID: "a/test.xht", Refs: []reftestRef{{Rel: "match", Path: "a/ref.xht"}}},
		&reftestEntry{ID: "a/page.html", Refs: []reftestRef{{Rel: "match", Path: "a/ref.xht"}}},
	)
	for _, id := range []string{"a/test.xht", "a/page.html"} {
		if _, err := f.Run(context.Background(), filepath.Join(root, filepath.FromSlash(id)), id); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{plain, plain, marked, plain}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Errorf("converted %q, want %q", got, want)
	}
}

// TestXHTDeparture pins the two departures of an HTML parse from an XML
// parse the selection keeps out, and that the text of a style element, the
// title or a comment in the head is none.
func TestXHTDeparture(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"<html><head><title>t</title><style>p {}</style><!-- note --></head><body><p>a</p></body></html>": "",
		"<html><head><style>p {}</style>\n  /* a note */\n</head><body></body></html>":                    xhtHeadText,
		"<html><head></head><body><pre>\nline</pre></body></html>":                                        xhtPreBreak,
		"<html><head></head><body><pre>line\n</pre><listing>a</listing></body></html>":                    "",
	} {
		if got := xhtDeparture([]byte(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
