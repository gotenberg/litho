package build

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("extprop_2b", extprop2b) }

// extprop2b builds extprop_2b.pdf: the container an extension schema's properties live in.
//
// ISO 19005-2, 6.6.2.3.3 test 5 reads
//
//	isPropertyValidSeq == true && (propertyPrefix == null || propertyPrefix == "pdfaSchema")
//
// so it judges the CONTAINER holding a schema's property list rather than any
// property inside it. `extschema_2b.pdf` covers the tests that judge the schema's
// own fields and its individual properties; this one covers the container, which
// those never reach because a well-formed `rdf:Seq` is what carries them.
//
// | Construct                                      | Rule            |
// | ---------------------------------------------- | --------------- |
// | a `pdfaSchema:property` holding an `rdf:Bag`   | `6.6.2.3.3/5`   |
//
// Three schemas sit side by side and only the first offends. The second holds the
// same single property in a proper `rdf:Seq`, and the third holds two properties
// in a `rdf:Seq`, because a container check that merely counted entries would pass
// a one-entry list and fail a longer one. Both are controls: every property inside
// all three schemas is complete and well formed, so nothing in the file can answer
// test 5 except the container itself.
//
// The properties are deliberately identical across the three schemas. If a check
// condemned the schema for something a property carries, all three would fire and
// the file would prove nothing about the container.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func extprop2b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 50 700 Td (extension schema property) Tj ET\n")),
	))
	pdf.AddPage(page)

	// prop is one complete pdfaProperty entry, so no property-level test can
	// fire.
	prop := func(name, description string) string {
		return `<rdf:li rdf:parseType="Resource">` +
			fmt.Sprintf("<pdfaProperty:name>%s</pdfaProperty:name>", name) +
			"<pdfaProperty:valueType>Text</pdfaProperty:valueType>" +
			"<pdfaProperty:category>external</pdfaProperty:category>" +
			fmt.Sprintf("<pdfaProperty:description>%s</pdfaProperty:description>", description) +
			"</rdf:li>"
	}

	// schema is one schema whose property list rides in the named RDF
	// container.
	schema := func(name, uri, prefix string, props []string, container string) string {
		return `<rdf:li rdf:parseType="Resource">` +
			fmt.Sprintf("<pdfaSchema:schema>%s</pdfaSchema:schema>", name) +
			fmt.Sprintf("<pdfaSchema:namespaceURI>%s</pdfaSchema:namespaceURI>", uri) +
			fmt.Sprintf("<pdfaSchema:prefix>%s</pdfaSchema:prefix>", prefix) +
			fmt.Sprintf("<pdfaSchema:property><rdf:%s>", container) +
			strings.Join(props, "") +
			fmt.Sprintf("</rdf:%s></pdfaSchema:property>", container) +
			"</rdf:li>"
	}

	// The defect: an rdf:Bag where the clause requires an rdf:Seq. The property
	// inside it is complete, so the container is the only thing left to answer for.
	sBag := schema(
		"Litho Bag Schema",
		"http://example.org/litho/bag#",
		"lbag",
		[]string{prop("bagOne", "a property in the wrong container")},
		"Bag",
	)

	// The control: the same single property in the container the clause requires.
	sSeq := schema(
		"Litho Seq Schema",
		"http://example.org/litho/seq#",
		"lseq",
		[]string{prop("seqOne", "a property in the right container")},
		"Seq",
	)

	// The second control carries two properties, so a container check that accepted
	// a one-entry list and rejected a longer one is visible here rather than hidden.
	sSeqTwo := schema(
		"Litho Pair Schema",
		"http://example.org/litho/pair#",
		"lpair",
		[]string{
			prop("pairOne", "first property of the pair"),
			prop("pairTwo", "second property of the pair"),
		},
		"Seq",
	)

	extension := "   <pdfaExtension:schemas><rdf:Bag>" +
		sBag +
		sSeq +
		sSeqTwo +
		"</rdf:Bag></pdfaExtension:schemas>"

	attachXMP(
		pdf,
		bytes.ReplaceAll(xmpPacket(xmpOpts{Part: 2, Conformance: "B", Extra: extension}),
			[]byte(`xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"`),
			[]byte(`xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"`+"\n"+
				`    xmlns:pdfaExtension="http://www.aiim.org/pdfa/ns/extension/"`+"\n"+
				`    xmlns:pdfaSchema="http://www.aiim.org/pdfa/ns/schema#"`+"\n"+
				`    xmlns:pdfaProperty="http://www.aiim.org/pdfa/ns/property#"`),
		),
	)

	// Deliberately no /OutputIntents, an independent defect that keeps a schema
	// verdict visibly separate from it.
	return save(pdf, "1.7")
}
