package xmp

import "strings"

// ControlChar reports whether r is a C0 control character, or DEL, that
// XML 1.0, 2.2 admits in no form other than tab, line feed and carriage
// return, so that no XMP value holds it. It mirrors the predicate veraPDF's
// XMP parser repairs a packet by.
func ControlChar(r rune) bool {
	return (r <= 0x1F || r == 0x7F) && r != '\t' && r != '\n' && r != '\r'
}

// Escape returns s as XML character data or an attribute value of either
// delimiter: the markup characters and both quotes become entity
// references, a carriage return a character reference, and a [ControlChar]
// is dropped.
func Escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	// Both quote characters are escaped because a value can land in an
	// attribute, and producers write XMP attributes with either delimiter.
	s = strings.ReplaceAll(s, "'", "&apos;")
	// A literal carriage return cannot survive a round trip: XML 1.0, 2.11
	// requires a parser to normalise CR and CRLF to a single LF before the
	// application sees the value. A numeric character reference is exempt
	// from that normalisation and parses back as CR.
	s = strings.ReplaceAll(s, "\r", "&#xD;")
	// XML 1.0, 2.2 admits no other C0 control character, raw or
	// referenced, so writing one would cost the packet its
	// well-formedness.
	return strings.Map(func(r rune) rune {
		if ControlChar(r) {
			return -1
		}
		return r
	}, s)
}

// Info is the document information a packet carries, each "" to leave the
// property out.
type Info struct {
	// Title is dc:title, the analogue of the Info dictionary's /Title.
	Title string
	// Author is dc:creator, the analogue of /Author.
	Author string
	// Subject is dc:description, the analogue of /Subject.
	Subject string
	// Keywords is pdf:Keywords, the analogue of /Keywords.
	Keywords string
	// Creator is xmp:CreatorTool, the analogue of /Creator.
	Creator string
	// Producer is pdf:Producer, the analogue of /Producer.
	Producer string
	// Lang is dc:language, a BCP 47 tag.
	Lang string
}

// Packet returns the XMP packet of a document whose information is info:
// one rdf:Description with the Dublin Core, XMP basic and Adobe PDF
// properties ISO 32000-2, 14.3.2 names as the
// analogues of the Info dictionary's entries, in a read-only packet
// (ISO 16684-1, 7.3). The output depends on info alone.
func Packet(info Info) []byte {
	var sb strings.Builder
	sb.WriteString("<?xpacket begin=\"\xEF\xBB\xBF\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n")
	sb.WriteString("<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n")
	sb.WriteString("<rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n")
	sb.WriteString("<rdf:Description rdf:about=\"\"\n")
	sb.WriteString("  xmlns:dc=\"http://purl.org/dc/elements/1.1/\"\n")
	sb.WriteString("  xmlns:xmp=\"http://ns.adobe.com/xap/1.0/\"\n")
	sb.WriteString("  xmlns:pdf=\"http://ns.adobe.com/pdf/1.3/\">\n")
	if info.Title != "" {
		sb.WriteString("  <dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">" + Escape(info.Title) + "</rdf:li></rdf:Alt></dc:title>\n")
	}
	if info.Author != "" {
		sb.WriteString("  <dc:creator><rdf:Seq><rdf:li>" + Escape(info.Author) + "</rdf:li></rdf:Seq></dc:creator>\n")
	}
	if info.Subject != "" {
		sb.WriteString("  <dc:description><rdf:Alt><rdf:li xml:lang=\"x-default\">" + Escape(info.Subject) + "</rdf:li></rdf:Alt></dc:description>\n")
	}
	if info.Lang != "" {
		sb.WriteString("  <dc:language><rdf:Bag><rdf:li>" + Escape(info.Lang) + "</rdf:li></rdf:Bag></dc:language>\n")
	}
	if info.Creator != "" {
		sb.WriteString("  <xmp:CreatorTool>" + Escape(info.Creator) + "</xmp:CreatorTool>\n")
	}
	if info.Producer != "" {
		sb.WriteString("  <pdf:Producer>" + Escape(info.Producer) + "</pdf:Producer>\n")
	}
	if info.Keywords != "" {
		sb.WriteString("  <pdf:Keywords>" + Escape(info.Keywords) + "</pdf:Keywords>\n")
	}
	sb.WriteString("</rdf:Description>\n")
	sb.WriteString("</rdf:RDF>\n")
	sb.WriteString("</x:xmpmeta>\n")
	sb.WriteString("<?xpacket end=\"r\"?>")
	return []byte(sb.String())
}
