package semantics

import "testing"

func TestParseXMP(t *testing.T) {
	tests := []struct {
		name string
		xml  string
		want map[string]string
	}{
		{
			name: "full Dublin Core and PDF properties",
			xml: `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""
        xmlns:dc="http://purl.org/dc/elements/1.1/"
        xmlns:pdf="http://ns.adobe.com/pdf/1.3/"
        xmlns:xmp="http://ns.adobe.com/xap/1.0/">
      <dc:title>
        <rdf:Alt>
          <rdf:li xml:lang="x-default">My Document Title</rdf:li>
        </rdf:Alt>
      </dc:title>
      <dc:creator>
        <rdf:Seq>
          <rdf:li>Alice</rdf:li>
          <rdf:li>Bob</rdf:li>
        </rdf:Seq>
      </dc:creator>
      <dc:subject>
        <rdf:Bag>
          <rdf:li>PDF</rdf:li>
          <rdf:li>metadata</rdf:li>
        </rdf:Bag>
      </dc:subject>
      <dc:rights>
        <rdf:Alt>
          <rdf:li xml:lang="x-default">Copyright 2025</rdf:li>
        </rdf:Alt>
      </dc:rights>
      <pdf:Producer>Test Producer 1.0</pdf:Producer>
      <pdf:Keywords>test, xmp</pdf:Keywords>
      <xmp:CreatorTool>TestApp</xmp:CreatorTool>
      <xmp:CreateDate>2025-01-15T10:30:00Z</xmp:CreateDate>
      <xmp:ModifyDate>2025-06-20T14:00:00Z</xmp:ModifyDate>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`,
			want: map[string]string{
				"Title":        "My Document Title",
				"Author":       "Alice, Bob",
				"Subject":      "PDF, metadata",
				"Rights":       "Copyright 2025",
				"Producer":     "Test Producer 1.0",
				"Keywords":     "test, xmp",
				"Creator":      "TestApp",
				"CreationDate": "2025-01-15T10:30:00Z",
				"ModDate":      "2025-06-20T14:00:00Z",
			},
		},
		{
			name: "partial metadata",
			xml: `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""
        xmlns:dc="http://purl.org/dc/elements/1.1/"
        xmlns:pdf="http://ns.adobe.com/pdf/1.3/">
      <dc:title>
        <rdf:Alt>
          <rdf:li xml:lang="x-default">Only Title</rdf:li>
        </rdf:Alt>
      </dc:title>
      <pdf:Producer>Minimal Producer</pdf:Producer>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>`,
			want: map[string]string{
				"Title":    "Only Title",
				"Producer": "Minimal Producer",
			},
		},
		{
			name: "invalid XML returns nil",
			xml:  `<not valid xml`,
			want: nil,
		},
		{
			name: "empty XMP returns empty map",
			xml: `<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""/>
  </rdf:RDF>
</x:xmpmeta>`,
			want: map[string]string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseXMP([]byte(tc.xml))
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d keys, got %d: %v", len(tc.want), len(got), got)
			}
			for k, wantV := range tc.want {
				if gotV, ok := got[k]; !ok {
					t.Errorf("missing key %q", k)
				} else if gotV != wantV {
					t.Errorf("key %q: want %q, got %q", k, wantV, gotV)
				}
			}
		})
	}
}
