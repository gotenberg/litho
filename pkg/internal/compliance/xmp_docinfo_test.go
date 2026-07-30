package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestXMPParseForSync_PreservesSimpleWhitespace pins the raw capture of
// depth-1 simple property values: XMP preserves element text verbatim,
// and the docinfo synchronization check compares the un-trimmed value
// against the Info dict, so a pretty-printed whitespace-only pdf:Keywords
// is NOT equivalent to an empty /Keywords (ISO 19005-1 §6.7.3 t5, verified
// live on PDF_UA-1__7.1-t01-pass-b retargeted to PDF/A-1b).
func TestXMPParseForSync_PreservesSimpleWhitespace(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdf="http://ns.adobe.com/pdf/1.3/">
      <pdf:Keywords>
         </pdf:Keywords>
      <pdf:Producer>litho</pdf:Producer>
    </rdf:Description>`)

	res := xmpParseForSync(data)
	if res == nil {
		t.Fatal("xmpParseForSync returned nil")
	}
	kw, ok := res.values["pdf:Keywords"]
	if !ok {
		t.Fatal("pdf:Keywords not captured")
	}
	if kw == "" || kw != "\n         " {
		t.Errorf("pdf:Keywords = %q, want raw whitespace preserved", kw)
	}
	if res.values["pdf:Producer"] != "litho" {
		t.Errorf("pdf:Producer = %q, want %q", res.values["pdf:Producer"], "litho")
	}
}

// TestXMPParseForSync_PreservesWhitespaceOnlyAltValue pins that a
// whitespace-only rdf:li inside an rdf:Alt container (a title of " ") is
// captured verbatim, not dropped. Dropping it misreads dc:title as empty and
// fires a spurious Info/XMP mismatch under §6.7.3 that veraPDF does not raise
// (verified live on pdfjs bug1142033 @PDF/A-1b, where /Title=" " matches a
// space-valued dc:title). Structural pretty-print whitespace between the
// container elements is still ignored because it sits outside any rdf:li.
func TestXMPParseForSync_PreservesWhitespaceOnlyAltValue(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:dc="http://purl.org/dc/elements/1.1/">
      <dc:title>
        <rdf:Alt>
          <rdf:li xml:lang="x-default"> </rdf:li>
        </rdf:Alt>
      </dc:title>
    </rdf:Description>`)

	res := xmpParseForSync(data)
	if res == nil {
		t.Fatal("xmpParseForSync returned nil")
	}
	v, ok := res.values["dc:title"]
	if !ok {
		t.Fatal("dc:title not captured (whitespace-only li was dropped)")
	}
	if v != " " {
		t.Errorf("dc:title = %q, want a single space", v)
	}
}

// TestXMPCheckDocInfoSync_NoPacket covers a document information dictionary in
// a file carrying no XMP packet at all. Every §6.7.3 test reads
// "Key == null || Key == XMPKey", so an Info entry that is present cannot match
// an absent XMP property and the rule fires. veraPDF reports it; litho used to
// return before reaching the check, because the surrounding packet tests have
// nothing to examine without a packet.
func TestXMPCheckDocInfoSync_NoPacket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog >>",
		"<< /Title (DocTitle) /Creator (DocCreator) /Producer (DocProducer) >>",
	)
	doc.Index.Trailer["Info"] = ast.PdfIndirectRef{ObjectNum: 2}

	var results []Violation
	xmpCheckDocInfoSync(ctx, doc, nil, 1, &results)

	got := map[Rule]bool{}
	for _, v := range results {
		got[v.Rule] = true
	}
	for _, want := range []Rule{
		Rule_19005_1_6_7_3_t02, // Title against dc:title
		Rule_19005_1_6_7_3_t06, // Creator against xmp:CreatorTool
		Rule_19005_1_6_7_3_t07, // Producer against pdf:Producer
	} {
		if !got[want] {
			t.Errorf("missing violation for %v, got %d violations", want, len(results))
		}
	}

	// An Info dictionary with no entries has nothing to disagree with.
	docEmpty, _ := objsDocPDF(t, "<< /Type /Catalog >>", "<< >>")
	docEmpty.Index.Trailer["Info"] = ast.PdfIndirectRef{ObjectNum: 2}
	var none []Violation
	xmpCheckDocInfoSync(ctx, docEmpty, nil, 1, &none)
	if len(none) != 0 {
		t.Errorf("empty Info dict reported %d violations, want 0", len(none))
	}
}

// TestXMPCheckDocInfoSync_EmptyInfoEntry covers an Info entry that is present
// but empty. Every §6.7.3 test reads "Key == null || Key == XMPKey" and an empty
// string is not null, so the comparison turns on whether the XMP property is
// DECLARED, not on its value: an empty /Title against a declared-but-empty
// dc:title passes, against an undeclared one fails. Values alone cannot tell
// those apart, because a property carrying no character data leaves no entry in
// the value map, which is why xmpSyncResult tracks presence separately.
func TestXMPCheckDocInfoSync_EmptyInfoEntry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	title := func(inner string) []byte {
		return xmpPacket(`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
			inner + `</rdf:Description>`)
	}

	for _, tc := range []struct {
		name     string
		xmp      []byte
		wantFire bool
	}{
		{
			name:     "no packet at all",
			xmp:      nil,
			wantFire: true,
		},
		{
			name:     "packet declares no dc:title",
			xmp:      title(`<dc:description><rdf:Alt><rdf:li xml:lang="x-default">d</rdf:li></rdf:Alt></dc:description>`),
			wantFire: true,
		},
		{
			name:     "dc:title declared and empty",
			xmp:      title(`<dc:title><rdf:Alt><rdf:li xml:lang="x-default"></rdf:li></rdf:Alt></dc:title>`),
			wantFire: false,
		},
		{
			name:     "dc:title declared with a value",
			xmp:      title(`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">Real</rdf:li></rdf:Alt></dc:title>`),
			wantFire: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, "<< /Type /Catalog >>", "<< /Title () >>")
			doc.Index.Trailer["Info"] = ast.PdfIndirectRef{ObjectNum: 2}

			var results []Violation
			xmpCheckDocInfoSync(ctx, doc, tc.xmp, 1, &results)

			fired := false
			for _, v := range results {
				if v.Rule == Rule_19005_1_6_7_3_t02 {
					fired = true
				}
			}
			if fired != tc.wantFire {
				t.Errorf("Title violation fired = %v, want %v (%d violations)", fired, tc.wantFire, len(results))
			}
		})
	}
}

// TestXMPDateInstant covers §6.7.3 date equivalence across timezones. The tests
// ask whether the Info entry and its XMP analogue are equivalent, not whether
// they are spelled alike, so two zoned dates naming the same instant compare
// equal. Measured against veraPDF: "D:20220406201541+02'00'" is equivalent to
// both "2022-04-06T13:15:41-05:00" and "2022-04-06T18:15:41Z", and not to
// "2022-04-06T14:15:41-05:00", which is an hour later.
func TestXMPDateInstant(t *testing.T) {
	t.Parallel()

	same := []string{
		"2022-04-06T20:15:41+02:00",
		"2022-04-06T13:15:41-05:00",
		"2022-04-06T18:15:41Z",
	}
	var want string
	for i, s := range same {
		got, ok := xmpDateInstant(s)
		if !ok {
			t.Fatalf("xmpDateInstant(%q) failed to parse", s)
		}
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Errorf("xmpDateInstant(%q) = %q, want %q", s, got, want)
		}
	}

	if got, _ := xmpDateInstant("2022-04-06T14:15:41-05:00"); got == want {
		t.Errorf("an instant an hour apart must not normalize to %q", want)
	}

	// A value with no timezone is not an instant, so the caller keeps its own
	// zone-agnostic comparison rather than inventing UTC.
	if _, ok := xmpDateInstant("2022-04-06T20:15:41"); ok {
		t.Error("a zoneless datetime must not parse as an instant")
	}
	if _, ok := xmpDateInstant("not a date"); ok {
		t.Error("unparseable input must report false")
	}
}

// TestXMPParseForSync_ContainerTypedProperties covers the XMP type of the three
// §6.7.3 analogues that are RDF containers: dc:title and dc:description are
// language alternatives and dc:creator is an ordered array. veraPDF resolves
// them through that container, so a value written as an attribute or as a plain
// element does not answer the lookup and must not be recorded. Measured: an
// attribute-form or simple-element dc:title reports t2 even when its text equals
// /Title, while an rdf:Alt passes whether or not its entry is x-default.
func TestXMPParseForSync_ContainerTypedProperties(t *testing.T) {
	t.Parallel()

	const dc = `xmlns:dc="http://purl.org/dc/elements/1.1/"`
	for _, tc := range []struct {
		name     string
		desc     string
		wantHave bool
	}{
		{
			name:     "attribute form is not a container",
			desc:     `<rdf:Description rdf:about="" ` + dc + ` dc:title="V"/>`,
			wantHave: false,
		},
		{
			name:     "plain element is not a container",
			desc:     `<rdf:Description rdf:about="" ` + dc + `><dc:title>V</dc:title></rdf:Description>`,
			wantHave: false,
		},
		{
			name:     "rdf:Alt with x-default counts",
			desc:     `<rdf:Description rdf:about="" ` + dc + `><dc:title><rdf:Alt><rdf:li xml:lang="x-default">V</rdf:li></rdf:Alt></dc:title></rdf:Description>`,
			wantHave: true,
		},
		{
			name:     "rdf:Alt without x-default still counts",
			desc:     `<rdf:Description rdf:about="" ` + dc + `><dc:title><rdf:Alt><rdf:li xml:lang="en">V</rdf:li></rdf:Alt></dc:title></rdf:Description>`,
			wantHave: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := xmpParseForSync(xmpPacket(tc.desc))
			if got == nil {
				t.Fatal("xmpParseForSync returned nil")
			}
			if got.present["dc:title"] != tc.wantHave {
				t.Errorf("dc:title present = %v, want %v (values=%v)", got.present["dc:title"], tc.wantHave, got.values)
			}
			if _, ok := got.values["dc:title"]; ok != tc.wantHave {
				t.Errorf("dc:title value recorded = %v, want %v", ok, tc.wantHave)
			}
		})
	}

	// A simple property is not container-typed: attribute form is its normal
	// spelling and must keep working.
	got := xmpParseForSync(xmpPacket(`<rdf:Description rdf:about="" xmlns:pdf="http://ns.adobe.com/pdf/1.3/" pdf:Producer="P"/>`))
	if got.values["pdf:Producer"] != "P" {
		t.Errorf("pdf:Producer = %q, want %q", got.values["pdf:Producer"], "P")
	}
}
