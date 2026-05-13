package compliance

// Hand-maintained rules not present in the VeraPDF validation profiles.
// These are litho-specific additions based on ISO 19005-4 requirements.

//nolint:gochecknoglobals // package-level rule definitions are the core design.
var (
	Rule_19005_4_6_7_3_t07 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.3",
		TestNumber:    7,
		Object:        "CIDSystemInfo",
		Description:   "If a document information dictionary is present in a conforming file, the value of each entry present in the document information dictionary shall be consistent with the corresponding XMP metadata entry",
	}

	Rule_19005_4_6_7_3_t08 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.3",
		TestNumber:    8,
		Object:        "CIDSystemInfo",
		Description:   "If a document information dictionary entry is present and non-empty, the corresponding XMP metadata property shall also be present",
	}

	// Hybrid-invoice rules covering the PDF/A-3 carrier requirements
	// shared by Factur-X 1.0+ and every ZUGFeRD generation. Each rule
	// is variant-agnostic; the offending filename, relationship, or
	// namespace URI surfaces in the Violation Context.

	Rule_invoice_AFArrayPresent = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    1,
		Object:        "Catalog",
		Description:   "The document catalog shall declare at least one filespec via the /AF array",
	}

	Rule_invoice_FileSpecPresent = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    2,
		Object:        "Filespec",
		Description:   "The /AF array shall reference a filespec whose name matches the variant filename",
	}

	Rule_invoice_AFRelationship = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    3,
		Object:        "Filespec",
		Description:   "The invoice filespec shall carry the /AFRelationship value mandated by the variant",
	}

	Rule_invoice_StreamSubtype = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    4,
		Object:        "EmbeddedFile",
		Description:   "The embedded invoice stream shall declare /Subtype /application#2Fxml",
	}

	Rule_invoice_XMPNamespace = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    5,
		Object:        "MainXMPPackage",
		Description:   "The XMP packet shall declare the variant namespace URI with the canonical prefix",
	}

	Rule_invoice_XMPDocumentType = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    6,
		Object:        "MainXMPPackage",
		Description:   "The XMP packet shall expose a non-empty DocumentType property in the variant namespace",
	}

	Rule_invoice_XMPDocumentFileName = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    7,
		Object:        "MainXMPPackage",
		Description:   "The XMP packet shall expose a DocumentFileName property in the variant namespace",
	}

	Rule_invoice_XMPVersion = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    8,
		Object:        "MainXMPPackage",
		Description:   "The XMP packet shall expose a Version property matching the variant expected value",
	}

	Rule_invoice_XMPConformanceLevel = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    9,
		Object:        "MainXMPPackage",
		Description:   "The XMP ConformanceLevel shall be one of the values allowed by the variant",
	}

	Rule_invoice_XMPExtensionSchema = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    10,
		Object:        "MainXMPPackage",
		Description:   "The XMP packet shall declare the variant namespace under pdfaExtension:schemas with its four properties",
	}

	Rule_invoice_FileNameCrossCheck = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    11,
		Object:        "Filespec",
		Description:   "The XMP DocumentFileName shall match the filespec /F (or /UF) entry",
	}

	Rule_invoice_NoConflictingVariant = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    12,
		Object:        "Catalog",
		Description:   "The document shall not embed filenames from a conflicting hybrid-invoice variant",
	}

	Rule_invoice_PDFA3Base = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE",
		TestNumber:    13,
		Object:        "CosDocument",
		Description:   "A hybrid invoice shall conform to PDF/A-3 as its archival base",
	}
)
