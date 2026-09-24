package compliance

// Hand-maintained rules that cmd/gen-rules does not generate: the
// litho-specific hybrid-invoice, CII and CII schema families, and the two
// WTPDF 1.0 declaration rules, whose profile the generator does not read.
//
// It also defines the parts 2 and 3 spellings of a requirement veraPDF
// enumerates only for part 4. Those two are never emitted, on purpose. See
// [Rule_19005_2_6_2_2_t03]: wiring them up would report violations the
// standard permits.

//nolint:gochecknoglobals // package-level rule definitions are the core design.
var (
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

	// Lenient-profile findings: consumer-path tolerances that the spec
	// does not sanction. Under strict validation (the default) they
	// surface as warnings, not fatals, so a document a real-world
	// producer emits still passes while the divergence is reported.
	// WithFacturXLenient suppresses them.

	Rule_invoice_AFRelationshipNonSpec = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE_LENIENT",
		TestNumber:    1,
		Object:        "Filespec",
		Description:   "The invoice filespec /AFRelationship uses a tolerated non-spec value; the spec mandates the variant value",
	}

	Rule_invoice_StreamSubtypeNonSpec = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE_LENIENT",
		TestNumber:    2,
		Object:        "EmbeddedFile",
		Description:   "The embedded invoice stream /Subtype is application/xml; the spec mandates text/xml",
	}

	Rule_invoice_VersionNonSpec = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "INVOICE_LENIENT",
		TestNumber:    3,
		Object:        "MainXMPPackage",
		Description:   "The XMP Version uses a tolerated non-spec value; the spec fixes the variant value",
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

	// CII structural rules. Cover the spine of a Factur-X 1.0+ /
	// ZUGFeRD 2.1+ Cross-Industry Invoice without pretending to be a
	// full UN/CEFACT XSD validator: the goal is to catch the realistic
	// failure modes (wrong root, missing required section, unreadable
	// payload) so litho rejects bad attachments before downstream
	// tools see them. Schema-deep type/value checks belong to the
	// per-profile XSD pass under [Rule_cii_xsd_Structure] and the
	// Schematron pass.

	Rule_cii_StreamReadable = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII",
		TestNumber:    1,
		Object:        "EmbeddedFile",
		Description:   "The embedded invoice XML stream shall resolve and read without error",
	}

	Rule_cii_WellFormed = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII",
		TestNumber:    2,
		Object:        "CII",
		Description:   "The embedded invoice payload shall be well-formed XML",
	}

	Rule_cii_RootElement = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII",
		TestNumber:    3,
		Object:        "CII",
		Description:   "The XML root shall be CrossIndustryInvoice in the UN/CEFACT CrossIndustryInvoice:100 namespace",
	}

	Rule_cii_DocumentContext = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII",
		TestNumber:    4,
		Object:        "CII",
		Description:   "CrossIndustryInvoice shall contain an ExchangedDocumentContext child",
	}

	Rule_cii_ExchangedDocument = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII",
		TestNumber:    5,
		Object:        "CII",
		Description:   "ExchangedDocument shall be present with non-empty ID and IssueDateTime",
	}

	Rule_cii_TradeTransaction = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII",
		TestNumber:    6,
		Object:        "CII",
		Description:   "CrossIndustryInvoice shall contain a SupplyChainTradeTransaction child",
	}

	Rule_cii_SellerTradeParty = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII",
		TestNumber:    7,
		Object:        "CII",
		Description:   "ApplicableHeaderTradeAgreement shall identify a SellerTradeParty with a non-empty Name",
	}

	Rule_cii_InvoiceCurrency = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII",
		TestNumber:    8,
		Object:        "CII",
		Description:   "ApplicableHeaderTradeSettlement shall carry a non-empty InvoiceCurrencyCode",
	}

	Rule_cii_GrandTotalAmount = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII",
		TestNumber:    9,
		Object:        "CII",
		Description:   "SpecifiedTradeSettlementHeaderMonetarySummation shall expose a non-empty GrandTotalAmount",
	}

	// CII XSD rules. Each rule below maps a category from the pure-Go
	// XSD engine ([pkg/internal/xsd]) onto a Violation surface so the
	// existing compliance scoreboard can aggregate them. Tests that
	// load a CII schema set into [pdf.Reader] turn structural,
	// cardinality, lexical, facet, and attribute failures the engine
	// finds into violations under the CII_XSD clause.

	Rule_cii_xsd_Root = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII_XSD",
		TestNumber:    1,
		Object:        "CII",
		Description:   "The embedded invoice document element shall be declared in the loaded XSD schema set",
	}

	Rule_cii_xsd_Structure = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII_XSD",
		TestNumber:    2,
		Object:        "CII",
		Description:   "Element ordering and parent-child structure shall match the XSD content model",
	}

	Rule_cii_xsd_Cardinality = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII_XSD",
		TestNumber:    3,
		Object:        "CII",
		Description:   "Each element occurrence count shall fall within the XSD minOccurs/maxOccurs bounds",
	}

	Rule_cii_xsd_Lexical = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII_XSD",
		TestNumber:    4,
		Object:        "CII",
		Description:   "Leaf-element text shall lie within the XSD built-in type's lexical space",
	}

	Rule_cii_xsd_Facet = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII_XSD",
		TestNumber:    5,
		Object:        "CII",
		Description:   "Leaf-element values shall satisfy every facet declared by the XSD simple type",
	}

	Rule_cii_xsd_Attribute = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII_XSD",
		TestNumber:    6,
		Object:        "CII",
		Description:   "Element attributes shall meet the XSD declaration's required/fixed/type constraints",
	}

	// CII Schematron rules. The Schematron engine fires when a
	// business-rule assertion sourced from EN 16931 / XRechnung
	// (or any user-supplied Schematron set) trips on the embedded
	// invoice. Two rules let the scoreboard tally by severity:
	// fatal-class flags map to Fatal, everything else to Warning.

	Rule_cii_sch_Fatal = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII_SCH",
		TestNumber:    1,
		Object:        "CII",
		Description:   "Schematron assertion with fatal severity fired on the embedded invoice",
	}

	Rule_cii_sch_Warning = Rule{
		Specification: "HYBRID_INVOICE",
		Clause:        "CII_SCH",
		TestNumber:    2,
		Object:        "CII",
		Description:   "Schematron assertion with warning or info severity fired on the embedded invoice",
	}

	// Rule_19005_2_6_2_2_t03 and Rule_19005_3_6_2_2_t03 are the Parts 2/3
	// spellings of the undefined-named-resource test, defined but never
	// emitted, and deliberately so.
	//
	// ISO 19005-4 §6.2.2 adds test 3: every named resource an operator
	// references shall be defined in the resource dictionary. Parts 2 and 3
	// state §6.2.2 with tests 1 and 2 alone, and ISO 19005-1 carries no
	// resource-completeness rule at all, since an undefined colour space name
	// conforms to PDF/A-1b. Emitting these under parts 1 to 3 would reject
	// documents those parts permit, so [missingResourceRule] gates the test to
	// part 4 and returns the zero Rule for every other part.
	//
	// They stay defined rather than deleted so the divergence stays visible.
	// Nothing may emit them while veraPDF enumerates the test for part 4 only.
	Rule_19005_2_6_2_2_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.2",
		TestNumber:    3,
		Object:        "PDContentStream",
		Description:   "A content stream's named resources shall be defined by a resource dictionary, which shall enumerate the named resources needed by the operators in the content stream and the names by which they can be referred to",
	}

	Rule_19005_3_6_2_2_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.2",
		TestNumber:    3,
		Object:        "PDContentStream",
		Description:   "A content stream's named resources shall be defined by a resource dictionary, which shall enumerate the named resources needed by the operators in the content stream and the names by which they can be referred to",
	}

	// Rule_WTPDF_1_0_6_1_2_t01 is the one rule WTPDF 1.0 states of its own
	// for the reuse conformance level (WTPDF 1.0, 6.1.2): a PDF Declaration
	// that names the level. Everything else the level requires is ISO
	// 14289-2, so this rule carries the WTPDF_1_0 specification and the rest
	// of a WTPDF run reports ISO_14289_2 clauses.
	Rule_WTPDF_1_0_6_1_2_t01 = Rule{
		Specification: "WTPDF_1_0",
		Clause:        "6.1.2",
		TestNumber:    1,
		Object:        "MainXMPPackage",
		Description:   "File conforming to the WTPDF 1.0 conformance level for reuse shall include a PDF Declaration with the URI identifier \"http://pdfa.org/declarations/wtpdf#reuse1.0\" as its pdfd:conformsTo entry",
	}

	// Rule_WTPDF_1_0_6_1_3_t01 is the one rule WTPDF 1.0 states of its own
	// for the accessibility conformance level (WTPDF 1.0, 6.1.3): a PDF
	// Declaration that names the level. As with
	// [Rule_WTPDF_1_0_6_1_2_t01], everything else the level requires is
	// ISO 14289-2.
	Rule_WTPDF_1_0_6_1_3_t01 = Rule{
		Specification: "WTPDF_1_0",
		Clause:        "6.1.3",
		TestNumber:    1,
		Object:        "MainXMPPackage",
		Description:   "File conforming to the WTPDF 1.0 conformance level for accessibility shall include a PDF Declaration with the URI identifier \"http://pdfa.org/declarations/wtpdf#accessibility1.0\" as its pdfd:conformsTo entry",
	}
)
