package compliance

import "github.com/gotenberg/litho/pkg/models"

// FacturXEmbedding is how the Factur-X 1.0+ / ZUGFeRD 2.1+ specification
// embeds the invoice XML of one conformance level: the file name, the
// /AFRelationship a writer emits, and the values a validator accepts.
type FacturXEmbedding struct {
	// FileName is the embedded file name, mirrored into XMP
	// fx:DocumentFileName.
	FileName string
	// Relationship is the /AFRelationship litho writes by default.
	Relationship string
	// Relationships lists every /AFRelationship the specification
	// admits for the level.
	Relationships []string
	// Tolerated lists the /AFRelationship values outside the
	// specification that strict validation reports and lenient
	// validation accepts.
	Tolerated []string
	// ConflictNames lists the file names that must not sit beside the
	// invoice.
	ConflictNames []string
}

// FacturXEmbeddingFor returns the embedding the specification fixes for
// level. An empty or unknown level gets the BASIC, EN 16931 and EXTENDED
// embedding, whose rules are the widest: the conformance level check
// reports the level itself.
//
// Per Factur-X 1.07.2 / ZUGFeRD 2.3.2:
//
//   - XRECHNUNG embeds xrechnung.xml, never beside a factur-x.xml, and its
//     relationship is always Alternative (7.7).
//   - MINIMUM and BASIC WL carry less invoice data than their visual part,
//     so the relationship must be Data (6.2.2).
//   - BASIC, EN 16931 and EXTENDED admit Data, Source or Alternative
//     depending on how the PDF was made (5.3, 6.2.2). Germany admits
//     Alternative alone, and litho writes it because it is the one value
//     valid in both countries. A PDF does not state the country it
//     targets, so validation accepts all three.
func FacturXEmbeddingFor(level models.FacturXLevel) FacturXEmbedding {
	switch level {
	case models.FacturXXRechnung:
		return FacturXEmbedding{
			FileName:      "xrechnung.xml",
			Relationship:  "Alternative",
			Relationships: []string{"Alternative"},
			ConflictNames: []string{"factur-x.xml", "zugferd-invoice.xml", "ZUGFeRD-invoice.xml"},
		}
	case models.FacturXMinimum, models.FacturXBasicWL:
		return FacturXEmbedding{
			FileName:      "factur-x.xml",
			Relationship:  "Data",
			Relationships: []string{"Data"},
			// Producers carrying ZUGFeRD 2.0 conventions write Alternative at
			// every level, which 6.2.2 rules out for these two.
			Tolerated:     []string{"Alternative"},
			ConflictNames: []string{"zugferd-invoice.xml", "ZUGFeRD-invoice.xml"},
		}
	case models.FacturXBasic, models.FacturXEN16931, models.FacturXExtended:
		// Shares the embedding below with an empty or unknown level.
	}
	return FacturXEmbedding{
		FileName:      "factur-x.xml",
		Relationship:  "Alternative",
		Relationships: []string{"Data", "Source", "Alternative"},
		ConflictNames: []string{"zugferd-invoice.xml", "ZUGFeRD-invoice.xml"},
	}
}
