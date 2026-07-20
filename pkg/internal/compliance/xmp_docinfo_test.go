package compliance

import "testing"

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
