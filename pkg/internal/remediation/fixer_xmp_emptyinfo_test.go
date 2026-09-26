package remediation

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// emptyInfoPDF builds a one-page document whose trailer /Info holds info, and
// whose catalog carries packet as its /Metadata when packet is not empty.
func emptyInfoPDF(info, packet string) []byte {
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	if packet != "" {
		catalog = "<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R >>"
	}
	objs := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< " + info + " >>",
	}
	if packet != "" {
		objs = append(objs, fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(packet), packet))
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 4 0 R /ID [<00112233445566778899aabbccddeeff> <00112233445566778899aabbccddeeff>] >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return buf.Bytes()
}

// emptyInfoPacket carries a value in every analogue of the /Info entries the
// tests below leave empty.
const emptyInfoPacket = `<?xpacket begin="` + "\xEF\xBB\xBF" + `" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:pdf="http://ns.adobe.com/pdf/1.3/">
<pdfaid:part>1</pdfaid:part>
<pdfaid:conformance>B</pdfaid:conformance>
<dc:title><rdf:Alt><rdf:li xml:lang="x-default">Quarterly report</rdf:li></rdf:Alt></dc:title>
<dc:creator><rdf:Seq><rdf:li>Jane Doe</rdf:li></rdf:Seq></dc:creator>
<dc:description><rdf:Alt><rdf:li xml:lang="x-default">Figures</rdf:li></rdf:Alt></dc:description>
<xmp:CreatorTool>Writer</xmp:CreatorTool>
<pdf:Producer>Converter</pdf:Producer>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

// emptyInfoEntries are the /Info entries ISO 19005-1, 6.7.3 compares with an
// XMP analogue, apart from /Keywords and the dates, which other tests pin,
// each holding the value the format argument gives.
const emptyInfoEntries = "/Title (%[1]s) /Subject (%[1]s) /Author (%[1]s) /Creator (%[1]s) /Producer (%[1]s)"

// TestRemediatePDFA_EmptyInfoEntriesHoldAfterOneRound pins that one pass of
// the remediation engine leaves no 6.7.3 finding on a present /Info entry that
// is empty or holds only whitespace, whether the engine generates the packet
// or patches one.
//
// veraPDF 1.30 compares an entry with its analogue verbatim, measured on
// crafted PDF/A-1b files: an empty /Title passes against an empty dc:title
// entry only, and fails against an absent one, a whitespace one, an rdf:Alt
// holding no entry, and the "Untitled" a generated packet wrote; a /Title of
// one space passes against a dc:title of one space only. The same holds for
// /Subject, /Author, /Creator and /Producer against dc:description,
// dc:creator, xmp:CreatorTool and pdf:Producer. The engine synced none of
// them while empty, so a generated packet held "Untitled", "litho" and no
// dc:creator or dc:description, and a patched one kept the values the entries
// no longer matched.
func TestRemediatePDFA_EmptyInfoEntriesHoldAfterOneRound(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, value, packet string
	}{
		{name: "empty entries, no packet", value: ""},
		{name: "whitespace entries, no packet", value: " "},
		{name: "empty entries, a packet", value: "", packet: emptyInfoPacket},
		{name: "whitespace entries, a packet", value: " ", packet: emptyInfoPacket},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, emptyInfoPDF(fmt.Sprintf(emptyInfoEntries, tc.value), tc.packet))
			before, err := compliance.ValidatePDFA(ctx, doc, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			flagged := map[int]bool{}
			for _, v := range toViolations(before) {
				if v.Rule.Clause == "6.7.3" {
					flagged[v.Rule.TestNumber] = true
				}
			}
			for _, test := range []int{2, 3, 4, 6, 7} {
				if !flagged[test] {
					t.Fatalf("precondition: the source does not fail 6.7.3 test %d, so the test proves nothing; violations = %v", test, before)
				}
			}

			if _, err := RemediatePDFA(ctx, doc, alloc, models.PDFA1b, Options{}); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			out := rewriteAndReopen(t, doc, alloc)
			errs, err := compliance.ValidatePDFA(ctx, out, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA on the output: %v", err)
			}
			for _, v := range toViolations(errs) {
				if v.Rule.Clause == "6.7.3" {
					t.Errorf("after one round: %v", v)
				}
			}

			packet := catalogPacket(t, out)
			for _, key := range []string{"dc:title", "dc:creator", "dc:description", "xmp:CreatorTool", "pdf:Producer"} {
				if got, ok := analogueText(packet, key); !ok || got != tc.value {
					t.Errorf("%s = %q (present %v), want the /Info value %q", key, got, ok, tc.value)
				}
			}
		})
	}
}

// TestGenerateXMP_PlaceholdersStandInForAbsentEntriesOnly pins which /Info
// entries get a placeholder analogue in a generated packet: an absent one,
// which ISO 19005-1, 6.7.3 does not compare, and under PDF/UA an empty
// /Title, since an empty dc:title identifies no document (ISO 14289-1, 7.1).
func TestGenerateXMP_PlaceholdersStandInForAbsentEntriesOnly(t *testing.T) {
	t.Parallel()

	present := infoMetadata{Present: map[string]bool{"Title": true, "Author": true, "Subject": true, "Creator": true, "Producer": true}}
	for _, tc := range []struct {
		name   string
		uaPart int
		info   infoMetadata
		want   map[string]string
	}{
		{
			name: "absent entries",
			info: infoMetadata{Present: map[string]bool{}},
			want: map[string]string{"dc:title": "Untitled", "xmp:CreatorTool": "litho", "pdf:Producer": "litho"},
		},
		{
			name: "empty entries",
			info: present,
			want: map[string]string{"dc:title": "", "dc:creator": "", "dc:description": "", "xmp:CreatorTool": "", "pdf:Producer": ""},
		},
		{
			name:   "empty entries under PDF/UA",
			uaPart: 1,
			info:   present,
			want:   map[string]string{"dc:title": "Untitled", "dc:creator": "", "dc:description": "", "xmp:CreatorTool": "", "pdf:Producer": ""},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			part := 1
			if tc.uaPart > 0 {
				part = 0
			}
			packet := []byte(generateXMP(part, "B", tc.uaPart, tc.info))
			for key, want := range tc.want {
				got, ok := analogueText(packet, key)
				if !ok {
					t.Errorf("%s is absent, want %q:\n%s", key, want, packet)
					continue
				}
				if got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
			}
			for _, key := range []string{"dc:creator", "dc:description"} {
				if _, want := tc.want[key]; !want {
					if _, ok := analogueText(packet, key); ok {
						t.Errorf("%s written for an absent /Info entry:\n%s", key, packet)
					}
				}
			}
		})
	}
}

// TestSyncInfoIntoXMP_EmptyTitleKeepsTheUATitle pins the PDF/UA exception to
// the empty-entry sync: an empty /Title neither blanks the dc:title that
// identifies the document (ISO 14289-1, 7.1) nor replaces it, while outside
// PDF/UA it is synced as ISO 19005-1, 6.7.3 requires.
func TestSyncInfoIntoXMP_EmptyTitleKeepsTheUATitle(t *testing.T) {
	t.Parallel()

	info := infoMetadata{Present: map[string]bool{"Title": true}}
	for _, tc := range []struct {
		uaPart int
		want   string
	}{
		{uaPart: 0, want: ""},
		{uaPart: 1, want: "Quarterly report"},
	} {
		out, _ := syncInfoIntoXMP([]byte(emptyInfoPacket), info, tc.uaPart)
		if got, _ := analogueText(out, "dc:title"); got != tc.want {
			t.Errorf("uaPart %d: dc:title = %q, want %q", tc.uaPart, got, tc.want)
		}
	}
}

// rewriteAndReopen writes doc with the repairs queued in alloc as a full
// rewrite and returns the output reopened.
func rewriteAndReopen(t *testing.T, doc *document.Document, alloc *writer.ObjAllocator) *document.Document {
	t.Helper()

	ctx := context.Background()
	var out bytes.Buffer
	if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	return reread
}

// catalogPacket returns the decoded catalog XMP packet of doc.
func catalogPacket(t *testing.T, doc *document.Document) []byte {
	t.Helper()

	ctx := context.Background()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("resolving the catalog: %v", err)
	}
	stream, err := document.ResolveStream(ctx, doc, catalog.Dict["Metadata"])
	if err != nil {
		t.Fatalf("resolving the catalog /Metadata: %v", err)
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		t.Fatalf("decoding the catalog /Metadata: %v", err)
	}
	return data
}

// xmpTagPattern matches one XML tag, so stripping every match leaves the text
// an analogue holds.
var xmpTagPattern = regexp.MustCompile(`<[^>]*>`)

// analogueText returns the text of the XMP analogue key ("dc:title",
// "pdf:Producer") in packet, verbatim with its container markup stripped, and
// whether the packet carries the analogue at all. It reads the bytes itself
// rather than through xmpPropertyRawValue, which trims the whitespace the
// tests compare.
func analogueText(packet []byte, key string) (string, bool) {
	ns := map[string]string{"dc": dcNS, "xmp": xmpNS, "pdf": pdfNS}
	prefix, local, _ := strings.Cut(key, ":")
	loc := findXMPProperty(packet, ns[prefix], local)
	frag := packet[loc.Start:loc.End]
	switch loc.Form {
	case xmpPropertyAsElement:
		_, openEnd, closeStart, ok := elementTextRange(frag)
		if !ok || closeStart < openEnd {
			return "", true
		}
		return xmpTagPattern.ReplaceAllString(string(frag[openEnd:closeStart]), ""), true
	case xmpPropertyAsAttribute:
		start, end, _ := attrValueRange(frag)
		return string(frag[start:end]), true
	case xmpPropertyNotFound:
	}
	return "", false
}
