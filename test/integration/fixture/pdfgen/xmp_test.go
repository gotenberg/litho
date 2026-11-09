package pdfgen

import (
	"bytes"
	"testing"
)

// TestPikepdfXMP holds PikepdfXMP to what pikepdf 9.11 (lxml) wrote back for
// each packet on save: formatting of element-only content, comments outside
// the root, CDATA, character references and stripped processing
// instructions.
func TestPikepdfXMP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, want string
	}{
		{
			"nows",
			"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\"><rdf:Description rdf:about=\"\" xmlns:dc=\"http://purl.org/dc/elements/1.1/\"><dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">a &amp; b &lt; c &gt; d \"q\"</rdf:li></rdf:Alt></dc:title><dc:format/></rdf:Description></rdf:RDF></x:xmpmeta>",
			"<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n  <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n    <rdf:Description xmlns:dc=\"http://purl.org/dc/elements/1.1/\" rdf:about=\"\">\n      <dc:title>\n        <rdf:Alt>\n          <rdf:li xml:lang=\"x-default\">a &amp; b &lt; c &gt; d \"q\"</rdf:li>\n        </rdf:Alt>\n      </dc:title>\n      <dc:format/>\n    </rdf:Description>\n  </rdf:RDF>\n</x:xmpmeta>\n\n<?xpacket end=\"w\"?>\n",
		},
		{
			"mixed",
			"<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n<!-- top comment -->\n<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n  <rdf:Description rdf:about=\"\" a:b=\"x&quot;y\" xmlns:a=\"urn:a\"><a:c><![CDATA[<raw> & stuff]]></a:c><a:d></a:d><!--c--><?pi inside?>\n<a:e attr='sq'>caf\xc3\xa9 &#233; &#13;</a:e></rdf:Description>\n </rdf:RDF>\n</x:xmpmeta>\n<!-- trailing -->\n<?xpacket end=\"w\"?>",
			"<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n<!-- top comment -->\n<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n  <rdf:Description xmlns:a=\"urn:a\" rdf:about=\"\" a:b=\"x&quot;y\"><a:c>&lt;raw&gt; &amp; stuff</a:c><a:d/><!--c-->\n<a:e attr=\"sq\">caf\xc3\xa9 \xc3\xa9 &#13;</a:e></rdf:Description>\n </rdf:RDF>\n</x:xmpmeta>\n<!-- trailing -->\n\n<?xpacket end=\"w\"?>\n",
		},
		{
			"empty",
			"   ",
			"<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n<x:xmpmeta xmlns:x=\"adobe:ns:meta/\" x:xmptk=\"pikepdf\">\n <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n </rdf:RDF>\n</x:xmpmeta>\n\n<?xpacket end=\"w\"?>\n",
		},
		{
			"nested",
			"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\"><rdf:Description rdf:about=\"\"><!--c--><x:y>t</x:y></rdf:Description>\n</rdf:RDF></x:xmpmeta>",
			"<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n  <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\"><rdf:Description rdf:about=\"\"><!--c--><x:y>t</x:y></rdf:Description>\n</rdf:RDF>\n</x:xmpmeta>\n\n<?xpacket end=\"w\"?>\n",
		},
	}
	for _, c := range cases {
		got, err := PikepdfXMP([]byte(c.in))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !bytes.Equal(got, []byte(c.want)) {
			t.Errorf("%s:\ngot  %q\nwant %q", c.name, got, c.want)
		}
	}
}
