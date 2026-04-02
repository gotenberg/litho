package compliance

// Rule represents a single VeraPDF validation rule.
type Rule struct {
	// Specification is the ISO standard identifier (e.g., "ISO_19005_1").
	Specification string

	// Clause is the standard clause number (e.g., "6.1.2").
	Clause string

	// TestNumber identifies the specific test within a clause.
	TestNumber int

	// Object is the VeraPDF object type this rule applies to (e.g., "CosDocument").
	Object string

	// Description is the normative requirement text from the ISO standard.
	Description string
}

// --- All unique VeraPDF validation rules ---
//
// Total unique rules: 751
// Source: https://github.com/veraPDF/veraPDF-validation-profiles (v1.28.2)

//nolint:gochecknoglobals // package-level rule definitions are the core design.
var (
	// ISO 14289-1:2014 (PDF/UA-1)

	Rule_14289_1_5_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "5",
		TestNumber:    1,
		Object:        "MainXMPPackage",
		Description:   "The PDF/UA version and conformance level of a file shall be specified using the PDF/UA Identification extension schema",
	}

	Rule_14289_1_5_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "5",
		TestNumber:    2,
		Object:        "PDFUAIdentification",
		Description:   "The value of \"pdfuaid:part\" shall be the part number of the International Standard to which the file conforms",
	}

	Rule_14289_1_5_t03 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "5",
		TestNumber:    3,
		Object:        "PDFUAIdentification",
		Description:   "Property \"part\" of the PDF/UA Identification Schema shall have namespace prefix \"pdfuaid\"",
	}

	Rule_14289_1_5_t04 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "5",
		TestNumber:    4,
		Object:        "PDFUAIdentification",
		Description:   "Property \"amd\" of the PDF/UA Identification Schema shall have namespace prefix \"pdfuaid\"",
	}

	Rule_14289_1_5_t05 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "5",
		TestNumber:    5,
		Object:        "PDFUAIdentification",
		Description:   "Property \"corr\" of the PDF/UA Identification Schema shall have namespace prefix \"pdfuaid\"",
	}

	Rule_14289_1_6_1_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "6.1",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The file header shall consist of \"%PDF-1.n\" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h)",
	}

	Rule_14289_1_6_2_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "6.2",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The document catalog dictionary shall include a MarkInfo dictionary containing an entry, Marked, whose value shall be true",
	}

	Rule_14289_1_7_1_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    1,
		Object:        "SEMarkedContent",
		Description:   "Content marked as Artifact should not be present inside tagged content",
	}

	Rule_14289_1_7_1_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    2,
		Object:        "SEMarkedContent",
		Description:   "Tagged content should not be present inside content marked as Artifact",
	}

	Rule_14289_1_7_1_t03 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    3,
		Object:        "SESimpleContentItem",
		Description:   "Content shall be marked as Artifact or tagged as real content",
	}

	Rule_14289_1_7_1_t04 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    4,
		Object:        "CosDocument",
		Description:   "Files shall have a Suspects value of false(ISO 32000-1:2008, Table 321)",
	}

	Rule_14289_1_7_1_t05 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    5,
		Object:        "SENonStandard",
		Description:   "All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in ISO 32000-1:2008, 14.8.4, in the role map dictionary of the structure tree root. This mapping may be indirect; within the role map a non-standard type can map directly to another non-standard type, but eventually the mapping shall terminate at a standard type",
	}

	Rule_14289_1_7_1_t06 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    6,
		Object:        "PDStructElem",
		Description:   "A circular mapping shall not exist",
	}

	Rule_14289_1_7_1_t07 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    7,
		Object:        "PDStructElem",
		Description:   "Standard tags defined in ISO 32000-1:2008, 14.8.4, shall not be remapped",
	}

	Rule_14289_1_7_1_t08 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    8,
		Object:        "PDDocument",
		Description:   "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-1:2008, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML",
	}

	Rule_14289_1_7_1_t09 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    9,
		Object:        "MainXMPPackage",
		Description:   "The Metadata stream in the document's catalog dictionary shall contain a dc:title entry, where dc is the recommended prefix for the Dublin Core metadata schema as defined in the XMP specification, which clearly identifies the document",
	}

	Rule_14289_1_7_1_t10 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    10,
		Object:        "CosDocument",
		Description:   "The document catalog dictionary shall include a ViewerPreferences dictionary containing a DisplayDocTitle key, whose value shall be true",
	}

	Rule_14289_1_7_1_t11 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    11,
		Object:        "PDDocument",
		Description:   "The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog dictionary, as described in ISO 32000-1:2008, 14.7",
	}

	Rule_14289_1_7_1_t12 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.1",
		TestNumber:    12,
		Object:        "PDStructElem",
		Description:   "A structure element dictionary shall contain the P (parent) entry according to ISO 32000-1:2008, 14.7.2, Table 355",
	}

	Rule_14289_1_7_2_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    2,
		Object:        "PDOutline",
		Description:   "Natural language in the Outline entries shall be determined",
	}

	Rule_14289_1_7_2_t03 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    3,
		Object:        "SETable",
		Description:   "Table element may contain only TR, THead, TBody, TFoot and Caption elements",
	}

	Rule_14289_1_7_2_t04 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    4,
		Object:        "SETR",
		Description:   "TR element should be contained in Table, THead, TBody or TFoot element",
	}

	Rule_14289_1_7_2_t05 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    5,
		Object:        "SETHead",
		Description:   "THead element should be contained in Table element",
	}

	Rule_14289_1_7_2_t06 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    6,
		Object:        "SETBody",
		Description:   "TBody element should be contained in Table element",
	}

	Rule_14289_1_7_2_t07 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    7,
		Object:        "SETFoot",
		Description:   "TFoot element should be contained in Table element",
	}

	Rule_14289_1_7_2_t08 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    8,
		Object:        "SETH",
		Description:   "TH element should be contained in TR element",
	}

	Rule_14289_1_7_2_t09 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    9,
		Object:        "SETD",
		Description:   "TD element should be contained in TR element",
	}

	Rule_14289_1_7_2_t10 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    10,
		Object:        "SETR",
		Description:   "TR element may contain only TH and TD elements",
	}

	Rule_14289_1_7_2_t11 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    11,
		Object:        "SETable",
		Description:   "Table element should contain zero or one THead kid",
	}

	Rule_14289_1_7_2_t12 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    12,
		Object:        "SETable",
		Description:   "Table element should contain zero or one TFoot kid",
	}

	Rule_14289_1_7_2_t13 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    13,
		Object:        "SETable",
		Description:   "If Table element contains TFoot kid, Table element should contain one or more TBody kids",
	}

	Rule_14289_1_7_2_t14 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    14,
		Object:        "SETable",
		Description:   "If Table element contains THead kid, Table element should contain one or more TBody kids",
	}

	Rule_14289_1_7_2_t15 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    15,
		Object:        "SETableCell",
		Description:   "A table cell shall not have intersection with other cells",
	}

	Rule_14289_1_7_2_t16 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    16,
		Object:        "SETable",
		Description:   "Table element may contain a Caption element as its first or last kid",
	}

	Rule_14289_1_7_2_t17 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    17,
		Object:        "SELI",
		Description:   "LI element should be contained in L element",
	}

	Rule_14289_1_7_2_t18 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    18,
		Object:        "SELBody",
		Description:   "LBody element should be contained in LI element",
	}

	Rule_14289_1_7_2_t19 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    19,
		Object:        "SEL",
		Description:   "L element may contain only L, LI and Caption elements",
	}

	Rule_14289_1_7_2_t20 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    20,
		Object:        "SELI",
		Description:   "LI element may contain only Lbl and LBody elements",
	}

	Rule_14289_1_7_2_t21 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    21,
		Object:        "PDStructElem",
		Description:   "Natural language for text in ActualText attribute shall be determined",
	}

	Rule_14289_1_7_2_t22 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    22,
		Object:        "PDStructElem",
		Description:   "Natural language for text in Alt attribute shall be determined",
	}

	Rule_14289_1_7_2_t23 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    23,
		Object:        "PDStructElem",
		Description:   "Natural language for text in E attribute shall be determined",
	}

	Rule_14289_1_7_2_t24 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    24,
		Object:        "PDAnnot",
		Description:   "Natural language in the Contents entry for annotations shall be determined",
	}

	Rule_14289_1_7_2_t25 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    25,
		Object:        "PDFormField",
		Description:   "Natural language in the TU key for form fields shall be determined",
	}

	Rule_14289_1_7_2_t26 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    26,
		Object:        "SETOCI",
		Description:   "TOCI element should be contained in TOC element",
	}

	Rule_14289_1_7_2_t27 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    27,
		Object:        "SETOC",
		Description:   "TOC element may contain only TOC, TOCI and Caption elements",
	}

	Rule_14289_1_7_2_t28 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    28,
		Object:        "SETOC",
		Description:   "TOC element may contain a Caption element only as its first kid",
	}

	Rule_14289_1_7_2_t29 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    29,
		Object:        "CosLang",
		Description:   "If the Lang entry is present in the document's Catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as described in ISO 32000-1:2008, 14.9.2. A language identifier shall be a Language-Tag as defined in RFC 3066, Tags for the Identification of Languages",
	}

	Rule_14289_1_7_2_t30 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    30,
		Object:        "SEMarkedContent",
		Description:   "Natural language for text in ActualText attribute in Span Marked Content shall be determined",
	}

	Rule_14289_1_7_2_t31 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    31,
		Object:        "SEMarkedContent",
		Description:   "Natural language for text in Alt attribute in Span Marked Content shall be determined",
	}

	Rule_14289_1_7_2_t32 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    32,
		Object:        "SEMarkedContent",
		Description:   "Natural language for text in E attribute in Span Marked Content shall be determined",
	}

	Rule_14289_1_7_2_t33 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    33,
		Object:        "XMPLangAlt",
		Description:   "Natural language for document metadata shall be determined",
	}

	Rule_14289_1_7_2_t34 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    34,
		Object:        "SETextItem",
		Description:   "Natural language for text in page content shall be determined",
	}

	Rule_14289_1_7_2_t36 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    36,
		Object:        "SETHead",
		Description:   "THead element may contain only TR elements",
	}

	Rule_14289_1_7_2_t37 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    37,
		Object:        "SETBody",
		Description:   "TBody element may contain only TR elements",
	}

	Rule_14289_1_7_2_t38 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    38,
		Object:        "SETFoot",
		Description:   "TFoot element may contain only TR elements",
	}

	Rule_14289_1_7_2_t39 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    39,
		Object:        "SETable",
		Description:   "Table element may contain only one Caption element",
	}

	Rule_14289_1_7_2_t40 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    40,
		Object:        "SEL",
		Description:   "L element may contain a Caption element only as its first kid",
	}

	Rule_14289_1_7_2_t41 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    41,
		Object:        "SETable",
		Description:   "Table columns shall have the same number of rows (taking into account row spans)",
	}

	Rule_14289_1_7_2_t42 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    42,
		Object:        "SETable",
		Description:   "Table rows shall have the same number of columns (taking into account column spans)",
	}

	Rule_14289_1_7_2_t43 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.2",
		TestNumber:    43,
		Object:        "SETable",
		Description:   "Table rows shall have the same number of columns (taking into account column spans)",
	}

	Rule_14289_1_7_3_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.3",
		TestNumber:    1,
		Object:        "SEFigure",
		Description:   "Figure tags shall include an alternative representation or replacement text that represents the contents marked with the Figure tag as noted in ISO 32000-1:2008, 14.7.2, Table 323",
	}

	Rule_14289_1_7_4_2_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.4.2",
		TestNumber:    1,
		Object:        "SEHn",
		Description:   "For documents that are not strongly structured, as described in ISO 32000-1:2008, 14.8.4.3.5, heading tags shall be used as follows: (*) If any heading tags are used, H1 shall be the first. (*) A document may use more than one instance of any specific tag level. For example, a tag level may be repeated if document content requires it. (*) If document semantics require a descending sequence of headers, such a sequence shall proceed in strict numerical order and shall not skip an intervening heading level. (*) A document may increment its heading sequence without restarting at H1 if document semantics require it",
	}

	Rule_14289_1_7_4_4_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.4.4",
		TestNumber:    1,
		Object:        "PDStructElem",
		Description:   "Each node in the tag tree shall contain at most one child H tag",
	}

	Rule_14289_1_7_4_4_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.4.4",
		TestNumber:    2,
		Object:        "SEH",
		Description:   "All documents shall be either strongly or weakly structured, but not both",
	}

	Rule_14289_1_7_4_4_t03 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.4.4",
		TestNumber:    3,
		Object:        "SEHn",
		Description:   "All documents shall be either strongly or weakly structured, but not both",
	}

	Rule_14289_1_7_5_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.5",
		TestNumber:    1,
		Object:        "SETD",
		Description:   "If the table's structure is not determinable via Headers and IDs, then structure elements of type TH shall have a Scope attribute",
	}

	Rule_14289_1_7_5_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.5",
		TestNumber:    2,
		Object:        "SETD",
		Description:   "If the table's structure is not determinable via Headers and IDs, then structure elements of type TH shall have a Scope attribute",
	}

	Rule_14289_1_7_7_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.7",
		TestNumber:    1,
		Object:        "SEFormula",
		Description:   "All mathematical expressions shall be enclosed within a Formula tag as detailed in ISO 32000-1:2008, 14.8.4.5 and shall have Alt or ActualText attributes",
	}

	Rule_14289_1_7_9_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.9",
		TestNumber:    1,
		Object:        "SENote",
		Description:   "Note tag shall have ID entry",
	}

	Rule_14289_1_7_9_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.9",
		TestNumber:    2,
		Object:        "SENote",
		Description:   "Each Note tag shall have unique ID key",
	}

	Rule_14289_1_7_10_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.10",
		TestNumber:    1,
		Object:        "PDOCConfig",
		Description:   "Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key",
	}

	Rule_14289_1_7_10_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.10",
		TestNumber:    2,
		Object:        "PDOCConfig",
		Description:   "The AS key shall not appear in any optional content configuration dictionary",
	}

	Rule_14289_1_7_11_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.11",
		TestNumber:    1,
		Object:        "CosFileSpecification",
		Description:   "The file specification dictionary for an embedded file shall contain the non-empty F and UF keys",
	}

	Rule_14289_1_7_15_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.15",
		TestNumber:    1,
		Object:        "PDAcroForm",
		Description:   "Dynamic XFA forms shall not be used",
	}

	Rule_14289_1_7_16_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.16",
		TestNumber:    1,
		Object:        "PDEncryption",
		Description:   "An encrypted conforming file shall contain a P key in its encryption dictionary (ISO 32000-1:2008, 7.6.3.2, Table 21). The 10th bit position of the P key shall be true",
	}

	Rule_14289_1_7_18_1_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.1",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "An annotation, excluding annotations of subtype Widget, PrinterMark or Link, shall be nested within an Annot tag",
	}

	Rule_14289_1_7_18_1_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.1",
		TestNumber:    2,
		Object:        "PDAnnot",
		Description:   "An annotation (except Widget annotations or hidden annotations, or those having rectangle outside the crop-box) shall have either Contents key or an Alt entry in the enclosing structure element",
	}

	Rule_14289_1_7_18_1_t03 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.1",
		TestNumber:    3,
		Object:        "PDWidgetAnnot",
		Description:   "A form field shall have a TU key present or all its Widget annotations shall have alternative descriptions (in the form of an Alt entry in the enclosing structure elements)",
	}

	Rule_14289_1_7_18_2_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.2",
		TestNumber:    1,
		Object:        "PDTrapNetAnnot",
		Description:   "Annotations of subtype TrapNet shall not be permitted",
	}

	Rule_14289_1_7_18_3_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.3",
		TestNumber:    1,
		Object:        "PDPage",
		Description:   "Every page on which there is an annotation shall contain in its page dictionary the key Tabs, and its value shall be S",
	}

	Rule_14289_1_7_18_4_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.4",
		TestNumber:    1,
		Object:        "PDWidgetAnnot",
		Description:   "A Widget annotation shall be nested within a Form tag per ISO 32000-1:2008, 14.8.4.5, Table 340",
	}

	Rule_14289_1_7_18_4_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.4",
		TestNumber:    2,
		Object:        "SEForm",
		Description:   "If the Form element omits a Role attribute (Table 348), it shall have only one child: an object reference (14.7.4.3) identifying the widget annotation per ISO 32000-1:2008, 14.8.4.5, Table 340",
	}

	Rule_14289_1_7_18_5_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.5",
		TestNumber:    1,
		Object:        "PDLinkAnnot",
		Description:   "Links shall be tagged according to ISO 32000-1:2008, 14.8.4.4.2, Link Element",
	}

	Rule_14289_1_7_18_5_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.5",
		TestNumber:    2,
		Object:        "PDLinkAnnot",
		Description:   "Links shall contain an alternate description via their Contents key as described in ISO 32000-1:2008, 14.9.3",
	}

	Rule_14289_1_7_18_6_2_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.6.2",
		TestNumber:    1,
		Object:        "PDMediaClip",
		Description:   "In the media clip data dictionary, the optional CT key (ISO 32000-1:2008, 13.2.4.2, Table 274) is required",
	}

	Rule_14289_1_7_18_6_2_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.6.2",
		TestNumber:    2,
		Object:        "PDMediaClip",
		Description:   "In the media clip data dictionary, the optional Alt key (ISO 32000-1:2008, 13.2.4.2, Table 274) is required",
	}

	Rule_14289_1_7_18_8_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.18.8",
		TestNumber:    1,
		Object:        "PDPrinterMarkAnnot",
		Description:   "PrinterMark annotations, if present, shall be considered Incidental Artifacts, as if they are hidden page elements as defined in ISO 32000-1:2008, 14.8.2.2.3",
	}

	Rule_14289_1_7_20_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.20",
		TestNumber:    1,
		Object:        "PDXForm",
		Description:   "A conforming file shall not contain any reference XObjects",
	}

	Rule_14289_1_7_20_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.20",
		TestNumber:    2,
		Object:        "PDXForm",
		Description:   "The content of Form XObjects shall be incorporated into structure elements according to ISO 32000-1:2008, 14.7.2",
	}

	Rule_14289_1_7_21_3_1_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.3.1",
		TestNumber:    1,
		Object:        "PDType0Font",
		Description:   "For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary is Identity-H or Identity-V, any values of Registry, Ordering, and Supplement may be used in the CIDSystemInfo entry of the CIDFont. - Otherwise, the corresponding Registry and Ordering strings in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be less than or equal to the Supplement key in the CIDSystemInfo dictionary of the CMap",
	}

	Rule_14289_1_7_21_3_2_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.3.2",
		TestNumber:    1,
		Object:        "PDCIDFont",
		Description:   "ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2008, 9.7.4, Table 117",
	}

	Rule_14289_1_7_21_3_3_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.3.3",
		TestNumber:    1,
		Object:        "PDCMap",
		Description:   "All CMaps used within a PDF/UA file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5",
	}

	Rule_14289_1_7_21_3_3_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.3.3",
		TestNumber:    2,
		Object:        "CMapFile",
		Description:   "For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream",
	}

	Rule_14289_1_7_21_3_3_t03 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.3.3",
		TestNumber:    3,
		Object:        "PDReferencedCMap",
		Description:   "A CMap shall not reference any other CMap except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118",
	}

	Rule_14289_1_7_21_4_1_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.4.1",
		TestNumber:    1,
		Object:        "PDFont",
		Description:   "The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9",
	}

	Rule_14289_1_7_21_4_1_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.4.1",
		TestNumber:    2,
		Object:        "Glyph",
		Description:   "Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced solely in text rendering mode 3 is not rendered and is thus exempt from the requirements that impact the visual representation of the glyphs of a font. In all cases for TrueType fonts that are to be rendered, character codes shall be able to be mapped to glyphs according to ISO 32000-1:2008, 9.6.6.4 without the use of a non-standard mapping chosen by the conforming processor",
	}

	Rule_14289_1_7_21_4_2_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.4.2",
		TestNumber:    1,
		Object:        "PDType1Font",
		Description:   "If the FontDescriptor dictionary of an embedded Type 1 font contains a CharSet string, then it shall list the character names of all glyphs present in the font program, regardless of whether a glyph in the font is referenced or used by the PDF or not",
	}

	Rule_14289_1_7_21_4_2_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.4.2",
		TestNumber:    2,
		Object:        "PDCIDFont",
		Description:   "If the FontDescriptor dictionary of an embedded CID font contains a CIDSet stream, then it shall identify all CIDs which are present in the font program, regardless of whether a CID in the font is referenced or used by the PDF or not",
	}

	Rule_14289_1_7_21_5_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.5",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent",
	}

	Rule_14289_1_7_21_6_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.6",
		TestNumber:    1,
		Object:        "TrueTypeFontProgram",
		Description:   "For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain one or several non-symbolic cmap entries such that all necessary glyph lookups can be carried out",
	}

	Rule_14289_1_7_21_6_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.6",
		TestNumber:    2,
		Object:        "PDTrueTypeFont",
		Description:   "All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the Font dictionary. In addition, no non-symbolic TrueType font shall define a Differences array unless all of the glyph names in the Differences array are listed in the Adobe Glyph List and, if the font program is embedded, it shall contain at least the Microsoft Unicode (3,1 – Platform ID = 3, Encoding ID = 1) encoding in the 'cmap' table",
	}

	Rule_14289_1_7_21_6_t03 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.6",
		TestNumber:    3,
		Object:        "PDTrueTypeFont",
		Description:   "Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary",
	}

	Rule_14289_1_7_21_6_t04 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.6",
		TestNumber:    4,
		Object:        "TrueTypeFontProgram",
		Description:   "The 'cmap' table in the embedded font program for a symbolic TrueType font shall contain either exactly one encoding or it shall contain, at least, the Microsoft Symbol (3,0 - Platform ID=3, Encoding ID=0) encoding",
	}

	Rule_14289_1_7_21_7_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.7",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 14289-1, 7.21.7",
	}

	Rule_14289_1_7_21_7_t02 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.7",
		TestNumber:    2,
		Object:        "Glyph",
		Description:   "The Unicode values specified in the ToUnicode CMap shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE",
	}

	Rule_14289_1_7_21_8_t01 = Rule{
		Specification: "ISO_14289_1",
		Clause:        "7.21.8",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "A PDF/UA compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream",
	}

	// ISO 14289-2:2024 (PDF/UA-2)

	Rule_14289_2_5_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "5",
		TestNumber:    1,
		Object:        "MainXMPPackage",
		Description:   "The PDF/UA version of a file shall be specified in the value of the Metadata entry in the document catalog using the PDF/UA identification schema",
	}

	Rule_14289_2_5_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "5",
		TestNumber:    2,
		Object:        "PDFUAIdentification",
		Description:   "The value of \"pdfuaid:part\" shall be the part number of the International Standard to which the file conforms",
	}

	Rule_14289_2_5_t03 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "5",
		TestNumber:    3,
		Object:        "PDFUAIdentification",
		Description:   "Property \"part\" of the PDF/UA Identification Schema shall have namespace prefix \"pdfuaid\"",
	}

	Rule_14289_2_5_t04 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "5",
		TestNumber:    4,
		Object:        "PDFUAIdentification",
		Description:   "Property \"rev\" of the PDF/UA Identification Schema shall have namespace prefix \"pdfuaid\"",
	}

	Rule_14289_2_5_t05 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "5",
		TestNumber:    5,
		Object:        "PDFUAIdentification",
		Description:   "The value of \"pdfuaid:rev\" shall be \"2024\"",
	}

	Rule_14289_2_6_2_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "6.2",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The document catalog dictionary shall include a MarkInfo dictionary containing an entry, Marked, whose value shall be true",
	}

	Rule_14289_2_8_2_1_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.1",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog dictionary, as described in ISO 32000-2:2020, 14.7",
	}

	Rule_14289_2_8_2_1_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.1",
		TestNumber:    2,
		Object:        "PDStructElem",
		Description:   "A structure element dictionary shall contain the P (parent) entry according to ISO 32000-2:2020, 14.7.2, Table 323",
	}

	Rule_14289_2_8_2_2_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.2",
		TestNumber:    1,
		Object:        "SESimpleContentItem",
		Description:   "Content that is not considered real shall be an artifact",
	}

	Rule_14289_2_8_2_4_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.4",
		TestNumber:    1,
		Object:        "SENonStandard",
		Description:   "All structure elements shall belong to, or be role mapped to, at least one of the following namespaces specified in ISO 32000-2:2020, 14.8.6: — the PDF 1.7 namespace; — the PDF 2.0 namespace; — the MathML namespace",
	}

	Rule_14289_2_8_2_4_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.4",
		TestNumber:    2,
		Object:        "PDStructElem",
		Description:   "A circular mapping shall not exist",
	}

	Rule_14289_2_8_2_4_t03 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.4",
		TestNumber:    3,
		Object:        "PDStructElem",
		Description:   "Within a given explicitly provided namespace, structure types shall not be role mapped to other structure types in the same namespace",
	}

	Rule_14289_2_8_2_4_t04 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.4",
		TestNumber:    4,
		Object:        "SENonStandard",
		Description:   "All structure elements shall belong to, or be role mapped to, at least one of the following namespaces specified in ISO 32000-2:2020, 14.8.6: — the PDF 1.7 namespace; — the PDF 2.0 namespace; — the MathML namespace",
	}

	Rule_14289_2_8_2_5_2_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.2",
		TestNumber:    1,
		Object:        "PDStructTreeRoot",
		Description:   "The structure tree root shall contain a single Document structure element as its only child, as specified in ISO 32000-2:2020, Annex L and ISO/TS 32005",
	}

	Rule_14289_2_8_2_5_2_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.2",
		TestNumber:    2,
		Object:        "PDStructTreeRoot",
		Description:   "The structure tree root shall contain a single Document structure element as its only child. The namespace for that element shall be specified as the PDF 2.0 namespace",
	}

	Rule_14289_2_8_2_5_8_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.8",
		TestNumber:    1,
		Object:        "SETOCI",
		Description:   "Each TOCI in the table of contents shall identify the target of the reference using the Ref entry, either directly on the TOCI structure element itself or on one of its child structure elements",
	}

	Rule_14289_2_8_2_5_12_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.12",
		TestNumber:    1,
		Object:        "SEH",
		Description:   "Conforming files shall not use the H structure type",
	}

	Rule_14289_2_8_2_5_14_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.14",
		TestNumber:    1,
		Object:        "SENote",
		Description:   "The Note standard structure type shall not be present in conforming documents unless role mapped to a structure element in the PDF 2.0 namespace",
	}

	Rule_14289_2_8_2_5_14_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.14",
		TestNumber:    2,
		Object:        "SEFENote",
		Description:   "Real content that refers to footnotes or endnotes (real content enclosed in FENote structure elements) shall use the Ref entry on the referring structure element to reference the FENote. The corresponding FENote shall also use the Ref entry to identify all citations that reference it",
	}

	Rule_14289_2_8_2_5_14_t03 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.14",
		TestNumber:    3,
		Object:        "SEFENote",
		Description:   "Real content that refers to footnotes or endnotes (real content enclosed in FENote structure elements) shall use the Ref entry on the referring structure element to reference the FENote. The corresponding FENote shall also use the Ref entry to identify all citations that reference it",
	}

	Rule_14289_2_8_2_5_14_t04 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.14",
		TestNumber:    4,
		Object:        "SEFENote",
		Description:   "The value of the NoteType attribute of the FENote structure element shall be Footnote, Endnote or None",
	}

	Rule_14289_2_8_2_5_20_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.20",
		TestNumber:    1,
		Object:        "PDLinkAnnot",
		Description:   "A link annotation and its associated content shall be enclosed in either a Link or Reference structure element",
	}

	Rule_14289_2_8_2_5_20_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.20",
		TestNumber:    2,
		Object:        "PDLinkAnnot",
		Description:   "Link annotations that target different locations shall be in separate Link or Reference structure elements",
	}

	Rule_14289_2_8_2_5_23_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.23",
		TestNumber:    1,
		Object:        "SERuby",
		Description:   "A Ruby structure element shall contain a single RB structure element and a single RT structure element or a Ruby structure element shall consist of a four-element subsequence: RB, RP, RT, RP",
	}

	Rule_14289_2_8_2_5_24_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.24",
		TestNumber:    1,
		Object:        "SEWarichu",
		Description:   "Content typeset as warichu shall be tagged in a three-element sequence consisting of the structure elements WP, WT and WP, grouped inside a Warichu structure element",
	}

	Rule_14289_2_8_2_5_25_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.25",
		TestNumber:    1,
		Object:        "SEL",
		Description:   "If Lbl structure elements are present, the ListNumbering attribute shall be present on the respective L structure element; in such cases the value None shall not be used",
	}

	Rule_14289_2_8_2_5_25_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.25",
		TestNumber:    2,
		Object:        "SELI",
		Description:   "Any real content within an LI structure element that is not enclosed in an Lbl structure element shall be enclosed in an LBody structure element",
	}

	Rule_14289_2_8_2_5_26_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.26",
		TestNumber:    1,
		Object:        "SETableCell",
		Description:   "Tables shall be regular. A table cell shall not have intersection with other cells",
	}

	Rule_14289_2_8_2_5_26_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.26",
		TestNumber:    2,
		Object:        "SETable",
		Description:   "Tables shall be regular. Row groupings formed by THead, TBody and TFoot structure elements shall be regular. Table columns shall have the same number of rows (taking into account row spans) in table and each row groupings formed by THead, TBody and TFoot structure elements",
	}

	Rule_14289_2_8_2_5_26_t03 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.26",
		TestNumber:    3,
		Object:        "SETable",
		Description:   "Tables shall be regular. Table rows shall have the same number of columns (taking into account column spans)",
	}

	Rule_14289_2_8_2_5_26_t04 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.26",
		TestNumber:    4,
		Object:        "SETable",
		Description:   "Tables shall be regular. Table rows shall have the same number of columns (taking into account column spans)",
	}

	Rule_14289_2_8_2_5_26_t05 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.26",
		TestNumber:    5,
		Object:        "SETD",
		Description:   "When a table contains header cells, that table shall provide sufficient semantic information to allow accurate determination of which of its table header cells (structure element TH) pertain to other cells as specified in ISO 32000-2:2020, 14.8.5.7",
	}

	Rule_14289_2_8_2_5_26_t06 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.26",
		TestNumber:    6,
		Object:        "SETD",
		Description:   "When a table contains header cells, that table shall provide sufficient semantic information to allow accurate determination of which of its table header cells (structure element TH) pertain to other cells as specified in ISO 32000-2:2020, 14.8.5.7",
	}

	Rule_14289_2_8_2_5_27_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.27",
		TestNumber:    1,
		Object:        "PDStructElem",
		Description:   "When present, the Caption element shall be the first or the last child of the structure element",
	}

	Rule_14289_2_8_2_5_28_2_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.28.2",
		TestNumber:    1,
		Object:        "SEFigure",
		Description:   "A Figure structure element shall have at least one of the following properties: a) an alternate description (Alt property), as specified in ISO 32000-2:2020, 14.9.3; b) a replacement text (ActualText property) that represents the content enclosed by the Figure structure element",
	}

	Rule_14289_2_8_2_5_29_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.2.5.29",
		TestNumber:    1,
		Object:        "SEMathMLStructElem",
		Description:   "The math structure type shall occur only as a child of a Formula structure element",
	}

	Rule_14289_2_8_4_3_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.3",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "In all cases, where real content maps to Unicode PUA values, an ActualText or Alt entry shall be present",
	}

	Rule_14289_2_8_4_3_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.3",
		TestNumber:    2,
		Object:        "CosActualText",
		Description:   "The ActualText entry shall not contain any PUA values",
	}

	Rule_14289_2_8_4_3_t03 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.3",
		TestNumber:    3,
		Object:        "CosAlt",
		Description:   "The Alt entry shall not contain any PUA values",
	}

	Rule_14289_2_8_4_4_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.4",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "The default natural language for content and text strings shall be specified using the Lang entry, with a non-empty value, in the catalog dictionary",
	}

	Rule_14289_2_8_4_4_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.4",
		TestNumber:    2,
		Object:        "CosLang",
		Description:   "If the Lang entry is present in the document's Catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as described in ISO 32000-2:2020, 14.9.2. A language identifier shall be a Language-Tag as defined in RFC 3066, Tags for the Identification of Languages",
	}

	Rule_14289_2_8_4_5_3_1_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.3.1",
		TestNumber:    1,
		Object:        "PDType0Font",
		Description:   "For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary has a value of Identity-H or Identity-V, then any values for the Registry, Ordering, and Supplement keys may be used in the CIDSystemInfo dictionary of the CIDFont. - Otherwise the corresponding values of the Registry and Ordering keys in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be less than or equal to the value of the Supplement key in the CIDSystemInfo dictionary of the CMap",
	}

	Rule_14289_2_8_4_5_3_2_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.3.2",
		TestNumber:    1,
		Object:        "PDCIDFont",
		Description:   "All embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-2:2020, Table 115",
	}

	Rule_14289_2_8_4_5_4_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.4",
		TestNumber:    1,
		Object:        "PDCMap",
		Description:   "All CMaps used within a file that conforms to PDF/UA-2, except those listed in ISO 32000-2:2020, Table 116, shall be embedded in that file, as described in ISO 32000-2:2020, 9.7.5",
	}

	Rule_14289_2_8_4_5_4_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.4",
		TestNumber:    2,
		Object:        "CMapFile",
		Description:   "For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream",
	}

	Rule_14289_2_8_4_5_4_t03 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.4",
		TestNumber:    3,
		Object:        "PDReferencedCMap",
		Description:   "A CMap shall not reference any other CMap except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116",
	}

	Rule_14289_2_8_4_5_5_1_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.5.1",
		TestNumber:    1,
		Object:        "PDFont",
		Description:   "The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-2:2020, 9.9",
	}

	Rule_14289_2_8_4_5_5_1_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.5.1",
		TestNumber:    2,
		Object:        "Glyph",
		Description:   "Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced solely in text rendering mode 3 is not rendered and is thus exempt from the requirements that impact the visual representation of the glyphs of a font. In all cases for TrueType fonts that are to be rendered, character codes shall be able to be mapped to glyphs in accordance with ISO 32000-2:2020, 9.6.5, without the use of a non-standard mapping chosen by the conforming processor",
	}

	Rule_14289_2_8_4_5_6_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.6",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "For every font embedded in a conforming file and referenced for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent for every glyph",
	}

	Rule_14289_2_8_4_5_7_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.7",
		TestNumber:    1,
		Object:        "TrueTypeFontProgram",
		Description:   "For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain at least Microsoft Unicode (3,1 – Platform ID=3, Encoding ID=1), or Macintosh Roman (1,0 – Platform ID=1, Encoding ID=0) 'cmap' subtable",
	}

	Rule_14289_2_8_4_5_7_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.7",
		TestNumber:    2,
		Object:        "PDTrueTypeFont",
		Description:   "All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the Font dictionary. In addition, no non-symbolic TrueType font shall define a Differences array unless all of the glyph names in the Differences array are listed in the Adobe Glyph List and, if the font program is embedded, it shall contain at least the Microsoft Unicode (3,1 – Platform ID = 3, Encoding ID = 1) encoding in the 'cmap' table",
	}

	Rule_14289_2_8_4_5_7_t03 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.7",
		TestNumber:    3,
		Object:        "PDTrueTypeFont",
		Description:   "Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary",
	}

	Rule_14289_2_8_4_5_7_t04 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.7",
		TestNumber:    4,
		Object:        "TrueTypeFontProgram",
		Description:   "The 'cmap' subtable in the embedded font program shall either contain the Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0 – Platform ID=1, Encoding ID=1) encoding",
	}

	Rule_14289_2_8_4_5_8_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.8",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 14289-2, 8.4.5.8",
	}

	Rule_14289_2_8_4_5_8_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.8",
		TestNumber:    2,
		Object:        "Glyph",
		Description:   "If a ToUnicode CMap is present, the Unicode values it specifies shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE",
	}

	Rule_14289_2_8_4_5_9_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.4.5.9",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "A file in conformance with PDF/UA-2 shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream",
	}

	Rule_14289_2_8_6_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.6",
		TestNumber:    1,
		Object:        "CosTextString",
		Description:   "Text strings intended to be human readable shall not use the Unicode PUA",
	}

	Rule_14289_2_8_7_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.7",
		TestNumber:    1,
		Object:        "PDOCConfig",
		Description:   "All optional content configuration dictionaries in the document, including the default optional content configuration dictionary, shall contain a Name entry (see ISO 32000-2:2020, Table 96) whose value is a non-empty text string when: a) a document contains a Configs entry in the OCProperties entry of the catalog dictionary (see ISO 32000-2:2020, Table 29), and b) the Configs entry contains at least one optional content configuration dictionary",
	}

	Rule_14289_2_8_7_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.7",
		TestNumber:    2,
		Object:        "PDOCConfig",
		Description:   "The AS key shall not appear in any optional content configuration dictionary",
	}

	Rule_14289_2_8_8_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.8",
		TestNumber:    1,
		Object:        "PDDestination",
		Description:   "All destinations whose target lies within the current document shall be structure destinations",
	}

	Rule_14289_2_8_8_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.8",
		TestNumber:    2,
		Object:        "PDGoToAction",
		Description:   "All destinations whose target lies within the current document shall be structure destinations",
	}

	Rule_14289_2_8_9_2_2_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.2",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Annotations shall be artifacts, if the Invisible flag is set in accordance with ISO 32000-2:2020, Table 167",
	}

	Rule_14289_2_8_9_2_2_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.2",
		TestNumber:    2,
		Object:        "PDAnnot",
		Description:   "Annotations shall be artifacts if the NoView flag is set and the ToggleNoView flag is not set in accordance with ISO 32000-2:2020, Table 167",
	}

	Rule_14289_2_8_9_2_3_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.3",
		TestNumber:    1,
		Object:        "PDMarkupAnnot",
		Description:   "Markup annotations shall be enclosed within Annot structure elements",
	}

	Rule_14289_2_8_9_2_3_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.3",
		TestNumber:    2,
		Object:        "PDMarkupAnnot",
		Description:   "When both RC and Contents entries are present for markup annotation, they shall be textually equivalent",
	}

	Rule_14289_2_8_9_2_4_7_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.7",
		TestNumber:    1,
		Object:        "PDRubberStampAnnot",
		Description:   "If the Name entry is insufficient to describe the intent of the stamp annotation, a Contents entry describing the author’s intent shall be provided",
	}

	Rule_14289_2_8_9_2_4_8_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.8",
		TestNumber:    1,
		Object:        "PDInkAnnot",
		Description:   "A Contents entry describing the author’s intent shall be provided for Ink annotation",
	}

	Rule_14289_2_8_9_2_4_9_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.9",
		TestNumber:    1,
		Object:        "PDPopupAnnot",
		Description:   "Popup annotations shall not be present in the structure tree",
	}

	Rule_14289_2_8_9_2_4_10_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.10",
		TestNumber:    1,
		Object:        "PDFileAttachmentAnnot",
		Description:   "When a file attachment annotation references a file specification dictionary, the file specification dictionary shall include an AFRelationship entry",
	}

	Rule_14289_2_8_9_2_4_11_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.11",
		TestNumber:    1,
		Object:        "PDSoundAnnot",
		Description:   "Sound annotation cannot be used when conforming to PDF/UA-2",
	}

	Rule_14289_2_8_9_2_4_11_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.11",
		TestNumber:    2,
		Object:        "PDMovieAnnot",
		Description:   "Movie annotation cannot be used when conforming to PDF/UA-2",
	}

	Rule_14289_2_8_9_2_4_12_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.12",
		TestNumber:    1,
		Object:        "PDScreenAnnot",
		Description:   "Screen annotations shall include a Contents entry",
	}

	Rule_14289_2_8_9_2_4_13_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.13",
		TestNumber:    1,
		Object:        "PDWidgetAnnot",
		Description:   "A widget annotation of zero height and width shall be an artifact",
	}

	Rule_14289_2_8_9_2_4_14_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.14",
		TestNumber:    1,
		Object:        "PDPrinterMarkAnnot",
		Description:   "A printer's mark annotation shall be an artifact",
	}

	Rule_14289_2_8_9_2_4_15_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.15",
		TestNumber:    1,
		Object:        "PDTrapNetAnnot",
		Description:   "Trap network annotations shall not be used in files conforming to PDF/UA-2",
	}

	Rule_14289_2_8_9_2_4_16_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.16",
		TestNumber:    1,
		Object:        "PDWatermarkAnnot",
		Description:   "When used as real content, Watermark annotations shall be enclosed within Annot structure elements",
	}

	Rule_14289_2_8_9_2_4_19_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.19",
		TestNumber:    1,
		Object:        "PD3DAnnot",
		Description:   "3D annotation shall include alternate description in respective Contents entry",
	}

	Rule_14289_2_8_9_2_4_19_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.2.4.19",
		TestNumber:    2,
		Object:        "PDRichMediaAnnot",
		Description:   "RichMedia annotation shall include alternate description in respective Contents entry",
	}

	Rule_14289_2_8_9_3_3_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.3.3",
		TestNumber:    1,
		Object:        "PDPage",
		Description:   "Every page that includes an annotation shall contain a Tabs entry in its page dictionary in accordance with ISO 32000-2:2020, Table 31, and its value shall be A, W or S",
	}

	Rule_14289_2_8_9_4_2_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.9.4.2",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Where an annotation has a Contents entry and the directly enclosing structure element has an Alt entry, the values of Alt and Contents shall be identical",
	}

	Rule_14289_2_8_10_1_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.10.1",
		TestNumber:    1,
		Object:        "PDWidgetAnnot",
		Description:   "Each widget annotation shall be enclosed by a Form structure element unless the widget annotation is an artifact",
	}

	Rule_14289_2_8_10_1_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.10.1",
		TestNumber:    2,
		Object:        "SEForm",
		Description:   "A Form structure element shall contain at most one widget annotation",
	}

	Rule_14289_2_8_10_1_t03 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.10.1",
		TestNumber:    3,
		Object:        "PDAcroForm",
		Description:   "XFA forms shall not be present",
	}

	Rule_14289_2_8_10_2_3_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.10.2.3",
		TestNumber:    1,
		Object:        "PDWidgetAnnot",
		Description:   "If a label for a widget annotation is not present, a Contents entry shall be provided to supply description and context for the widget",
	}

	Rule_14289_2_8_10_2_3_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.10.2.3",
		TestNumber:    2,
		Object:        "PDWidgetAnnot",
		Description:   "If an additional action (AA) entry is present in a widget annotation dictionary, the respective widget's Contents entry shall be present",
	}

	Rule_14289_2_8_10_3_3_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.10.3.3",
		TestNumber:    1,
		Object:        "PDTextField",
		Description:   "For text fields, when an RV entry is present a V entry shall also be present, and they shall be textually equivalent",
	}

	Rule_14289_2_8_10_3_5_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.10.3.5",
		TestNumber:    1,
		Object:        "SEGraphicContentItem",
		Description:   "If a portion of the appearance of a signature is represented by a graphic, alternative text shall be provided for that graphic",
	}

	Rule_14289_2_8_11_1_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.11.1",
		TestNumber:    1,
		Object:        "MainXMPPackage",
		Description:   "The Metadata stream as specified in ISO 32000-2:2020, 14.3 in the document catalog dictionary shall contain a dc:title entry",
	}

	Rule_14289_2_8_11_1_t02 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.11.1",
		TestNumber:    2,
		Object:        "PDDocument",
		Description:   "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-2:2020, 14.3. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML",
	}

	Rule_14289_2_8_11_2_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.11.2",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The ViewerPreferences dictionary of the document catalog dictionary shall be present and shall contain at least the DisplayDocTitle key with a value of true, as described in ISO 32000-2:2020, Table 147",
	}

	Rule_14289_2_8_14_1_t01 = Rule{
		Specification: "ISO_14289_2",
		Clause:        "8.14.1",
		TestNumber:    1,
		Object:        "CosFileSpecification",
		Description:   "The Desc entry shall be present on all file specification dictionaries present in the EmbeddedFiles name tree of a conforming document",
	}

	// ISO 19005-1:2005 (PDF/A-1)

	Rule_19005_1_6_1_2_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.2",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The % character of the file header shall occur at byte offset 0 of the file. The first line of a PDF file is a header identifying the version of the PDF specification to which the file conforms",
	}

	Rule_19005_1_6_1_2_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.2",
		TestNumber:    2,
		Object:        "CosDocument",
		Description:   "The file header line shall be immediately followed by a comment consisting of a % character followed by at least four characters, each of whose encoded byte values shall have a decimal value greater than 127",
	}

	Rule_19005_1_6_1_3_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.3",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The file trailer dictionary shall contain the ID keyword. The file trailer referred to is either the last trailer dictionary in a PDF file, as described in PDF Reference 3.4.4 and 3.4.5, or the first page trailer in a linearized PDF file, as described in PDF Reference F.2",
	}

	Rule_19005_1_6_1_3_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.3",
		TestNumber:    2,
		Object:        "CosTrailer",
		Description:   "The keyword Encrypt shall not be used in the trailer dictionary",
	}

	Rule_19005_1_6_1_3_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.3",
		TestNumber:    3,
		Object:        "CosDocument",
		Description:   "No data shall follow the last end-of-file marker except a single optional end-of-line marker",
	}

	Rule_19005_1_6_1_3_t04 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.3",
		TestNumber:    4,
		Object:        "CosDocument",
		Description:   "In a linearized PDF, if the ID keyword is present in both the first page trailer dictionary and the last trailer dictionary, the value to both instances of the ID keyword shall be identical",
	}

	Rule_19005_1_6_1_4_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.4",
		TestNumber:    1,
		Object:        "CosXRef",
		Description:   "In a cross reference subsection header the starting object number and the range shall be separated by a single SPACE character (20h)",
	}

	Rule_19005_1_6_1_4_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.4",
		TestNumber:    2,
		Object:        "CosXRef",
		Description:   "The xref keyword and the cross reference subsection header shall be separated by a single EOL marker",
	}

	Rule_19005_1_6_1_4_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.4",
		TestNumber:    3,
		Object:        "PDDocument",
		Description:   "Xref streams shall not be used",
	}

	Rule_19005_1_6_1_6_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.6",
		TestNumber:    1,
		Object:        "CosString",
		Description:   "Hexadecimal strings shall contain an even number of non-white-space characters",
	}

	Rule_19005_1_6_1_6_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.6",
		TestNumber:    2,
		Object:        "CosString",
		Description:   "All non-white-space characters in hexadecimal strings shall be in the range 0 to 9, A to F or a to f",
	}

	Rule_19005_1_6_1_7_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.7",
		TestNumber:    1,
		Object:        "CosStream",
		Description:   "The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED character after the stream keyword and preceding the EOL marker before the endstream keyword",
	}

	Rule_19005_1_6_1_7_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.7",
		TestNumber:    2,
		Object:        "CosStream",
		Description:   "The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED character. The endstream keyword shall be preceded by an EOL marker",
	}

	Rule_19005_1_6_1_7_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.7",
		TestNumber:    3,
		Object:        "CosStream",
		Description:   "A stream object dictionary shall not contain the F, FFilter, or FDecodeParms keys",
	}

	Rule_19005_1_6_1_8_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.8",
		TestNumber:    1,
		Object:        "CosIndirect",
		Description:   "The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded by an EOL marker. The obj and endobj keywords shall each be followed by an EOL marker",
	}

	Rule_19005_1_6_1_10_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.10",
		TestNumber:    1,
		Object:        "CosFilter",
		Description:   "The LZWDecode filter shall not be permitted",
	}

	Rule_19005_1_6_1_10_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.10",
		TestNumber:    2,
		Object:        "CosIIFilter",
		Description:   "The LZWDecode filter shall not be permitted",
	}

	Rule_19005_1_6_1_11_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.11",
		TestNumber:    1,
		Object:        "CosFileSpecification",
		Description:   "A file specification dictionary, as defined in PDF 3.10.2, shall not contain the EF key",
	}

	Rule_19005_1_6_1_11_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.11",
		TestNumber:    2,
		Object:        "CosDocument",
		Description:   "A file's name dictionary, as defined in PDF Reference 3.6.3, shall not contain the EmbeddedFiles key",
	}

	Rule_19005_1_6_1_12_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.12",
		TestNumber:    1,
		Object:        "CosInteger",
		Description:   "Largest Integer value is 2,147,483,647. Smallest integer value is -2,147,483,648",
	}

	Rule_19005_1_6_1_12_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.12",
		TestNumber:    2,
		Object:        "CosReal",
		Description:   "Absolute real value must be less than or equal to 32767.0",
	}

	Rule_19005_1_6_1_12_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.12",
		TestNumber:    3,
		Object:        "CosString",
		Description:   "Maximum length of a string (in bytes) is 65535",
	}

	Rule_19005_1_6_1_12_t04 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.12",
		TestNumber:    4,
		Object:        "CosName",
		Description:   "Maximum length of a name (in bytes) is 127",
	}

	Rule_19005_1_6_1_12_t05 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.12",
		TestNumber:    5,
		Object:        "CosArray",
		Description:   "Maximum capacity of an array (in elements) is 8191",
	}

	Rule_19005_1_6_1_12_t06 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.12",
		TestNumber:    6,
		Object:        "CosDict",
		Description:   "Maximum capacity of a dictionary (in entries) is 4095",
	}

	Rule_19005_1_6_1_12_t07 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.12",
		TestNumber:    7,
		Object:        "CosDocument",
		Description:   "Maximum number of indirect objects in a PDF file is 8,388,607",
	}

	Rule_19005_1_6_1_12_t08 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.12",
		TestNumber:    8,
		Object:        "Op_q_gsave",
		Description:   "Maximum depth of graphics state nesting by q and Q operators is 28",
	}

	Rule_19005_1_6_1_12_t09 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.12",
		TestNumber:    9,
		Object:        "PDDeviceN",
		Description:   "Maximum number of DeviceN components is 8",
	}

	Rule_19005_1_6_1_12_t10 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.12",
		TestNumber:    10,
		Object:        "CMapFile",
		Description:   "Maximum value of a CID (character identifier) is 65,535",
	}

	Rule_19005_1_6_1_13_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.1.13",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The document catalog dictionary shall not contain a key with the name OCProperties",
	}

	Rule_19005_1_6_2_2_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.2",
		TestNumber:    1,
		Object:        "ICCOutputProfile",
		Description:   "A PDF/A-1 OutputIntent is an OutputIntent dictionary, as defined by PDF Reference 9.10.4, that is included in the file's OutputIntents array and has GTS_PDFA1 as the value of its S key and a valid ICC profile stream as the value its DestOutputProfile key",
	}

	Rule_19005_1_6_2_2_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.2",
		TestNumber:    2,
		Object:        "OutputIntents",
		Description:   "If a file's OutputIntents array contains more than one entry, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream",
	}

	Rule_19005_1_6_2_3_2_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.3.2",
		TestNumber:    1,
		Object:        "ICCInputProfile",
		Description:   "All ICCBased colour spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5",
	}

	Rule_19005_1_6_2_3_2_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.3.2",
		TestNumber:    2,
		Object:        "ICCProfile",
		Description:   "All ICCBased colour spaces shall be embedded as ICC profile streams as described in PDF Reference 4.5. The number of color components in the color space described by the ICC profile data must match the number of components actually in the ICC profile. As of PDF 1.4, N must be 1, 3, or 4",
	}

	Rule_19005_1_6_2_3_3_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.3.3",
		TestNumber:    1,
		Object:        "PDDeviceRGB",
		Description:   "DeviceRGB may be used only if the file has a PDF/A-1 OutputIntent that uses an RGB colour space",
	}

	Rule_19005_1_6_2_3_3_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.3.3",
		TestNumber:    2,
		Object:        "PDDeviceCMYK",
		Description:   "DeviceCMYK may be used only if the file has a PDF/A-1 OutputIntent that uses a CMYK colour space",
	}

	Rule_19005_1_6_2_3_3_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.3.3",
		TestNumber:    3,
		Object:        "PDDeviceGray",
		Description:   "If an uncalibrated colour space is used in a file then that file shall contain a PDF/A-1 OutputIntent, as defined in 6.2.2",
	}

	Rule_19005_1_6_2_4_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.4",
		TestNumber:    1,
		Object:        "PDXImage",
		Description:   "An Image dictionary shall not contain the Alternates key",
	}

	Rule_19005_1_6_2_4_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.4",
		TestNumber:    2,
		Object:        "PDXObject",
		Description:   "An XObject dictionary (Image or Form) shall not contain the OPI key",
	}

	Rule_19005_1_6_2_4_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.4",
		TestNumber:    3,
		Object:        "PDXImage",
		Description:   "If an Image dictionary contains the Interpolate key, its value shall be false",
	}

	Rule_19005_1_6_2_4_t04 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.4",
		TestNumber:    4,
		Object:        "PDXImage",
		Description:   "If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4 or 8",
	}

	Rule_19005_1_6_2_4_t05 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.4",
		TestNumber:    5,
		Object:        "PDMaskImage",
		Description:   "If an image mask dictionary contains the BitsPerComponent key, its value shall be 1",
	}

	Rule_19005_1_6_2_5_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.5",
		TestNumber:    1,
		Object:        "PDXForm",
		Description:   "A form XObject dictionary shall not contain the Subtype2 key with a value of PS or the PS key",
	}

	Rule_19005_1_6_2_6_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.6",
		TestNumber:    1,
		Object:        "PDXForm",
		Description:   "A conforming file shall not contain any reference XObjects",
	}

	Rule_19005_1_6_2_7_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.7",
		TestNumber:    1,
		Object:        "PDXObject",
		Description:   "A conforming file shall not contain any PostScript XObjects",
	}

	Rule_19005_1_6_2_8_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.8",
		TestNumber:    1,
		Object:        "PDExtGState",
		Description:   "An ExtGState dictionary shall not contain the TR key",
	}

	Rule_19005_1_6_2_8_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.8",
		TestNumber:    2,
		Object:        "PDExtGState",
		Description:   "An ExtGState dictionary shall not contain the TR2 key with a value other than Default",
	}

	Rule_19005_1_6_2_9_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.9",
		TestNumber:    1,
		Object:        "CosRenderingIntent",
		Description:   "Where a rendering intent is specified, its value shall be one of the four values defined in PDF Reference RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation",
	}

	Rule_19005_1_6_2_10_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.2.10",
		TestNumber:    1,
		Object:        "Op_Undefined",
		Description:   "A content stream shall not contain any operators not defined in PDF Reference even if such operators are bracketed by the BX/EX compatibility operators",
	}

	Rule_19005_1_6_3_2_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.2",
		TestNumber:    1,
		Object:        "PDFont",
		Description:   "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary",
	}

	Rule_19005_1_6_3_2_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.2",
		TestNumber:    2,
		Object:        "PDFont",
		Description:   "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Subtype - name - (Required) The type of font; must be \"Type1\" for Type 1 fonts, \"MMType1\" for multiple master fonts, \"TrueType\" for TrueType fonts \"Type3\" for Type 3 fonts, \"Type0\" for Type 0 fonts and \"CIDFontType0\" or \"CIDFontType2\" for CID fonts",
	}

	Rule_19005_1_6_3_2_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.2",
		TestNumber:    3,
		Object:        "PDFont",
		Description:   "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. BaseFont - name - (Required) The PostScript name of the font",
	}

	Rule_19005_1_6_3_2_t04 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.2",
		TestNumber:    4,
		Object:        "PDSimpleFont",
		Description:   "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array",
	}

	Rule_19005_1_6_3_2_t05 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.2",
		TestNumber:    5,
		Object:        "PDSimpleFont",
		Description:   "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array",
	}

	Rule_19005_1_6_3_2_t06 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.2",
		TestNumber:    6,
		Object:        "PDSimpleFont",
		Description:   "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths",
	}

	Rule_19005_1_6_3_2_t07 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.2",
		TestNumber:    7,
		Object:        "PDFont",
		Description:   "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.4 are Type1C - Type 1–equivalent font program represented in the Compact Font Format (CFF) and CIDFontType0C - Type 0 CIDFont program represented in the Compact Font Format (CFF)",
	}

	Rule_19005_1_6_3_3_1_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.3.1",
		TestNumber:    1,
		Object:        "PDType0Font",
		Description:   "For any given composite (Type 0) font referenced within a conforming file, the CIDSystemInfo entries of its CIDFont and CMap dictionaries shall be compatible. In other words, the Registry and Ordering strings of the CIDSystemInfo dictionaries for that font shall be identical, unless the value of the Encoding key in the font dictionary is Identity-H or Identity-V",
	}

	Rule_19005_1_6_3_3_2_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.3.2",
		TestNumber:    1,
		Object:        "PDCIDFont",
		Description:   "For all Type 2 CIDFonts that are used for rendering, the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in PDF Reference Table 5.13",
	}

	Rule_19005_1_6_3_3_3_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.3.3",
		TestNumber:    1,
		Object:        "PDCMap",
		Description:   "All CMaps used within a conforming file, except Identity-H and Identity-V, shall be embedded in that file as described in PDF Reference 5.6.4",
	}

	Rule_19005_1_6_3_3_3_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.3.3",
		TestNumber:    2,
		Object:        "CMapFile",
		Description:   "For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream",
	}

	Rule_19005_1_6_3_4_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.4",
		TestNumber:    1,
		Object:        "PDFont",
		Description:   "The font programs for all fonts used within a conforming file shall be embedded within that file, as defined in PDF Reference 5.8, except when the fonts are used exclusively with text rendering mode 3",
	}

	Rule_19005_1_6_3_5_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.5",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "Embedded font programs shall define all font glyphs referenced for rendering with conforming file",
	}

	Rule_19005_1_6_3_5_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.5",
		TestNumber:    2,
		Object:        "PDType1Font",
		Description:   "For all Type 1 font subsets referenced within a conforming file, the font descriptor dictionary shall include a CharSet string listing the character names defined in the font subset, as described in PDF Reference Table 5.18",
	}

	Rule_19005_1_6_3_5_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.5",
		TestNumber:    3,
		Object:        "PDCIDFont",
		Description:   "For all CIDFont subsets referenced within a conforming file, the font descriptor dictionary shall include a CIDSet stream identifying which CIDs are present in the embedded CIDFont file, as described in PDF Reference Table 5.20",
	}

	Rule_19005_1_6_3_6_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.6",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent",
	}

	Rule_19005_1_6_3_7_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.7",
		TestNumber:    1,
		Object:        "PDTrueTypeFont",
		Description:   "All non-symbolic TrueType fonts shall specify MacRomanEncoding or WinAnsiEncoding, either as the value of the Encoding entry in the font dictionary or as the value of the BaseEncoding entry in the dictionary that is the value of the Encoding entry in the font dictionary. If the value of the Encoding entry is a dictionary, it shall not contain a Differences entry",
	}

	Rule_19005_1_6_3_7_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.7",
		TestNumber:    2,
		Object:        "PDTrueTypeFont",
		Description:   "All symbolic TrueType fonts shall not specify an Encoding entry in the font dictionary",
	}

	Rule_19005_1_6_3_7_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.7",
		TestNumber:    3,
		Object:        "TrueTypeFontProgram",
		Description:   "Font programs' \"cmap\" tables for all symbolic TrueType fonts shall contain exactly one encoding",
	}

	Rule_19005_1_6_3_8_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.3.8",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "The font dictionary shall include a ToUnicode entry whose value is a CMap stream object that maps character codes to Unicode values, as described in PDF Reference 5.9, unless the font meets any of the following three conditions: (*) fonts that use the predefined encodings MacRomanEncoding, MacExpertEncoding or WinAnsiEncoding, or that use the predefined Identity-H or Identity-V CMaps; (*) Type 1 fonts whose character names are taken from the Adobe standard Latin character set or the set of named characters in the Symbol font, as defined in PDF Reference Appendix D; (*) Type 0 fonts whose descendant CIDFont uses the Adobe-GB1, Adobe-CNS1, Adobe-Japan1 or Adobe-Korea1 character collections",
	}

	Rule_19005_1_6_4_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.4",
		TestNumber:    1,
		Object:        "PDExtGState",
		Description:   "If an SMask key appears in an ExtGState dictionary, its value shall be None",
	}

	Rule_19005_1_6_4_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.4",
		TestNumber:    2,
		Object:        "PDXObject",
		Description:   "An XObject dictionary shall not contain the SMask key",
	}

	Rule_19005_1_6_4_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.4",
		TestNumber:    3,
		Object:        "PDGroup",
		Description:   "A Group object with an S key with a value of Transparency shall not be included in a form XObject. A Group object with an S key with a value of Transparency shall not be included in a page dictionary",
	}

	Rule_19005_1_6_4_t04 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.4",
		TestNumber:    4,
		Object:        "PDExtGState",
		Description:   "If a BM key is present in an ExtGState object, its value shall be Normal or Compatible",
	}

	Rule_19005_1_6_4_t05 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.4",
		TestNumber:    5,
		Object:        "PDExtGState",
		Description:   "If a CA key is present in an ExtGState object, its value shall be 1.0",
	}

	Rule_19005_1_6_4_t06 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.4",
		TestNumber:    6,
		Object:        "PDExtGState",
		Description:   "If a ca key is present in an ExtGState object, its value shall be 1.0",
	}

	Rule_19005_1_6_5_2_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.5.2",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Annotation types not defined in PDF Reference shall not be permitted. Additionally, the FileAttachment, Sound and Movie types shall not be permitted",
	}

	Rule_19005_1_6_5_3_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.5.3",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "An annotation dictionary shall not contain the CA key with a value other than 1.0",
	}

	Rule_19005_1_6_5_3_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.5.3",
		TestNumber:    2,
		Object:        "PDAnnot",
		Description:   "An annotation dictionary shall contain the F key. The F key’s Print flag bit shall be set to 1 and its Hidden, Invisible and NoView flag bits shall be set to 0",
	}

	Rule_19005_1_6_5_3_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.5.3",
		TestNumber:    3,
		Object:        "PDAnnot",
		Description:   "An annotation dictionary shall not contain the C array or the IC array unless the colour space of the DestOutputProfile in the PDF/A-1 OutputIntent dictionary, defined in 6.2.2, is RGB",
	}

	Rule_19005_1_6_5_3_t04 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.5.3",
		TestNumber:    4,
		Object:        "PDAnnot",
		Description:   "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key",
	}

	Rule_19005_1_6_5_3_t05 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.5.3",
		TestNumber:    5,
		Object:        "PDAnnot",
		Description:   "If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary",
	}

	Rule_19005_1_6_5_3_t06 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.5.3",
		TestNumber:    6,
		Object:        "PDAnnot",
		Description:   "If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream",
	}

	Rule_19005_1_6_6_1_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.6.1",
		TestNumber:    1,
		Object:        "PDAction",
		Description:   "The Launch, Sound, Movie, ResetForm, ImportData and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted. The Hide action shall not be permitted (Corrigendum 2)",
	}

	Rule_19005_1_6_6_1_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.6.1",
		TestNumber:    2,
		Object:        "PDNamedAction",
		Description:   "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted",
	}

	Rule_19005_1_6_6_1_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.6.1",
		TestNumber:    3,
		Object:        "PDWidgetAnnot",
		Description:   "Interactive form fields shall not perform actions of any type",
	}

	Rule_19005_1_6_6_2_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.6.2",
		TestNumber:    1,
		Object:        "PDWidgetAnnot",
		Description:   "A Widget annotation dictionary shall not include an AA entry for an additional-actions dictionary",
	}

	Rule_19005_1_6_6_2_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.6.2",
		TestNumber:    2,
		Object:        "PDFormField",
		Description:   "A Field dictionary shall not include an AA entry for an additional-actions dictionary",
	}

	Rule_19005_1_6_6_2_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.6.2",
		TestNumber:    3,
		Object:        "PDDocument",
		Description:   "The document catalog dictionary shall not include an AA entry for an additional-actions dictionary",
	}

	Rule_19005_1_6_7_2_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.2",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "The document catalog dictionary of a conforming file shall contain the Metadata key. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML",
	}

	Rule_19005_1_6_7_2_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.2",
		TestNumber:    2,
		Object:        "PDMetadata",
		Description:   "The Metadata object stream dictionary in the document’s catalog shall not contain the Filter key",
	}

	Rule_19005_1_6_7_3_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.3",
		TestNumber:    1,
		Object:        "CosInfo",
		Description:   "The value of CreationDate entry from the document information dictionary, if present, and its analogous XMP property \"xmp:CreateDate\" shall be equivalent",
	}

	Rule_19005_1_6_7_3_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.3",
		TestNumber:    2,
		Object:        "CosInfo",
		Description:   "The value of Title entry from the document information dictionary, if present, and its analogous XMP property \"dc:title['x-default']\" shall be equivalent",
	}

	Rule_19005_1_6_7_3_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.3",
		TestNumber:    3,
		Object:        "CosInfo",
		Description:   "The value of Author entry from the document information dictionary, if present, and its analogous XMP property \"dc:creator\" shall be equivalent. dc:creator shall contain exactly one entry",
	}

	Rule_19005_1_6_7_3_t04 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.3",
		TestNumber:    4,
		Object:        "CosInfo",
		Description:   "The value of Subject entry from the document information dictionary, if present, and its analogous XMP property \"dc:description['x-default']\" shall be equivalent",
	}

	Rule_19005_1_6_7_3_t05 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.3",
		TestNumber:    5,
		Object:        "CosInfo",
		Description:   "The value of Keywords entry from the document information dictionary, if present, and its analogous XMP property \"pdf:Keywords\" shall be equivalent",
	}

	Rule_19005_1_6_7_3_t06 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.3",
		TestNumber:    6,
		Object:        "CosInfo",
		Description:   "The value of Creator entry from the document information dictionary, if present, and its analogous XMP property \"xmp:CreatorTool\" shall be equivalent",
	}

	Rule_19005_1_6_7_3_t07 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.3",
		TestNumber:    7,
		Object:        "CosInfo",
		Description:   "The value of Producer entry from the document information dictionary, if present, and its analogous XMP property \"pdf:Producer\" shall be equivalent",
	}

	Rule_19005_1_6_7_3_t08 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.3",
		TestNumber:    8,
		Object:        "CosInfo",
		Description:   "The value of ModDate entry from the document information dictionary, if present, and its analogous XMP property \"xmp:ModifyDate\" shall be equivalent",
	}

	Rule_19005_1_6_7_5_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.5",
		TestNumber:    1,
		Object:        "XMPPackage",
		Description:   "The bytes attribute shall not be used in the header of an XMP packet",
	}

	Rule_19005_1_6_7_5_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.5",
		TestNumber:    2,
		Object:        "XMPPackage",
		Description:   "The encoding attribute shall not be used in the header of an XMP packet",
	}

	Rule_19005_1_6_7_8_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    1,
		Object:        "ExtensionSchemaObject",
		Description:   "Extension schema descriptions shall be specified using the PDF/A extension schema description schema defined in this clause",
	}

	Rule_19005_1_6_7_8_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    2,
		Object:        "ExtensionSchemasContainer",
		Description:   "The extension schema container schema uses the namespace URI \"http://www.aiim.org/pdfa/ns/extension/\". The required schema namespace prefix is pdfaExtension. pdfaExtension:schemas - Bag Schema - Description of extension schemas",
	}

	Rule_19005_1_6_7_8_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    3,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_1_6_7_8_t04 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    4,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_1_6_7_8_t05 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    5,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_1_6_7_8_t06 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    6,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_1_6_7_8_t07 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    7,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_1_6_7_8_t08 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    8,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'",
	}

	Rule_19005_1_6_7_8_t09 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    9,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined",
	}

	Rule_19005_1_6_7_8_t10 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    10,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty'",
	}

	Rule_19005_1_6_7_8_t11 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    11,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'",
	}

	Rule_19005_1_6_7_8_t12 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    12,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'",
	}

	Rule_19005_1_6_7_8_t13 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    13,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType'",
	}

	Rule_19005_1_6_7_8_t14 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    14,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'",
	}

	Rule_19005_1_6_7_8_t15 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    15,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'",
	}

	Rule_19005_1_6_7_8_t16 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    16,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType'",
	}

	Rule_19005_1_6_7_8_t17 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    17,
		Object:        "ExtensionSchemaField",
		Description:   "Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'",
	}

	Rule_19005_1_6_7_8_t18 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    18,
		Object:        "ExtensionSchemaField",
		Description:   "Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined",
	}

	Rule_19005_1_6_7_8_t19 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.8",
		TestNumber:    19,
		Object:        "ExtensionSchemaField",
		Description:   "Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'",
	}

	Rule_19005_1_6_7_9_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.9",
		TestNumber:    1,
		Object:        "XMPPackage",
		Description:   "The metadata stream shall conform to XMP Specification and well formed PDFAExtension Schema for all extensions",
	}

	Rule_19005_1_6_7_9_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.9",
		TestNumber:    2,
		Object:        "XMPProperty",
		Description:   "Properties specified in XMP form shall use either the predefined schemas defined in XMP Specification, or extension schemas that comply with XMP Specification",
	}

	Rule_19005_1_6_7_9_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.9",
		TestNumber:    3,
		Object:        "XMPProperty",
		Description:   "Properties specified in XMP form shall use either the predefined schemas defined in XMP Specification, or extension schemas that comply with XMP Specification",
	}

	Rule_19005_1_6_7_11_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.11",
		TestNumber:    1,
		Object:        "MainXMPPackage",
		Description:   "The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema",
	}

	Rule_19005_1_6_7_11_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.11",
		TestNumber:    2,
		Object:        "PDFAIdentification",
		Description:   "The value of \"pdfaid:part\" shall be the part number of ISO 19005 to which the file conforms",
	}

	Rule_19005_1_6_7_11_t03 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.11",
		TestNumber:    3,
		Object:        "PDFAIdentification",
		Description:   "A Level A conforming file shall specify the value of \"pdfaid:conformance\" as A",
	}

	Rule_19005_1_6_7_11_t04 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.11",
		TestNumber:    4,
		Object:        "PDFAIdentification",
		Description:   "Property \"part\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_1_6_7_11_t05 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.11",
		TestNumber:    5,
		Object:        "PDFAIdentification",
		Description:   "Property \"conformance\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_1_6_7_11_t06 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.7.11",
		TestNumber:    6,
		Object:        "PDFAIdentification",
		Description:   "Property \"amd\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_1_6_8_2_2_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.8.2.2",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The document catalog dictionary shall include a MarkInfo dictionary with a Marked entry in it, whose value shall be true",
	}

	Rule_19005_1_6_8_3_3_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.8.3.3",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document catalog dictionary, as described in PDF Reference 9.6",
	}

	Rule_19005_1_6_8_3_4_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.8.3.4",
		TestNumber:    1,
		Object:        "SENonStandard",
		Description:   "All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in PDF Reference 9.7.4, in the role map dictionary of the structure tree root",
	}

	Rule_19005_1_6_8_3_4_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.8.3.4",
		TestNumber:    2,
		Object:        "SENonStandard",
		Description:   "A circular mapping shall not exist",
	}

	Rule_19005_1_6_8_4_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.8.4",
		TestNumber:    1,
		Object:        "CosLang",
		Description:   "If the Lang entry is present in the document catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as defined by RFC 1766, Tags for the Identification of Languages, as described in PDF Reference 9.8.1",
	}

	Rule_19005_1_6_9_t01 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.9",
		TestNumber:    1,
		Object:        "PDAcroForm",
		Description:   "The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false",
	}

	Rule_19005_1_6_9_t02 = Rule{
		Specification: "ISO_19005_1",
		Clause:        "6.9",
		TestNumber:    2,
		Object:        "PDWidgetAnnot",
		Description:   "Every form field shall have an appearance dictionary associated with the field's data",
	}

	// ISO 19005-2:2011 (PDF/A-2)

	Rule_19005_2_6_1_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.2",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The file header shall begin at byte zero and shall consist of \"%PDF-1.n\" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h)",
	}

	Rule_19005_2_6_1_2_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.2",
		TestNumber:    2,
		Object:        "CosDocument",
		Description:   "The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127",
	}

	Rule_19005_2_6_1_3_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.3",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The file trailer dictionary shall contain the ID keyword whose value shall be File Identifiers as defined in ISO 32000-1:2008, 14.4",
	}

	Rule_19005_2_6_1_3_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.3",
		TestNumber:    2,
		Object:        "CosTrailer",
		Description:   "The keyword Encrypt shall not be used in the trailer dictionary",
	}

	Rule_19005_2_6_1_3_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.3",
		TestNumber:    3,
		Object:        "CosDocument",
		Description:   "No data can follow the last end-of-file marker except a single optional end-of-line marker as described in ISO 32000-1:2008, 7.5.5",
	}

	Rule_19005_2_6_1_4_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.4",
		TestNumber:    2,
		Object:        "CosXRef",
		Description:   "The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker",
	}

	Rule_19005_2_6_1_6_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.6",
		TestNumber:    1,
		Object:        "CosString",
		Description:   "Hexadecimal strings shall contain an even number of non-white-space characters",
	}

	Rule_19005_2_6_1_6_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.6",
		TestNumber:    2,
		Object:        "CosString",
		Description:   "A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f)",
	}

	Rule_19005_2_6_1_7_1_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.7.1",
		TestNumber:    1,
		Object:        "CosStream",
		Description:   "The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword",
	}

	Rule_19005_2_6_1_7_1_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.7.1",
		TestNumber:    2,
		Object:        "CosStream",
		Description:   "The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED (0Ah) character. The endstream keyword shall be preceded by an EOL marker",
	}

	Rule_19005_2_6_1_7_1_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.7.1",
		TestNumber:    3,
		Object:        "CosStream",
		Description:   "A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys",
	}

	Rule_19005_2_6_1_7_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.7.2",
		TestNumber:    1,
		Object:        "CosFilter",
		Description:   "All standard stream filters listed in ISO 32000-1:2008, 7.4, Table 6 may be used, with the exception of LZWDecode. In addition, the Crypt filter shall not be used unless the value of the Name key in the decode parameters dictionary is Identity. Filters that are not listed in ISO 32000-1:2008, 7.4, Table 6 shall not be used",
	}

	Rule_19005_2_6_1_8_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.8",
		TestNumber:    1,
		Object:        "CosUnicodeName",
		Description:   "Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences",
	}

	Rule_19005_2_6_1_9_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.9",
		TestNumber:    1,
		Object:        "CosIndirect",
		Description:   "The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded by an EOL marker. The obj and endobj keywords shall each be followed by an EOL marker",
	}

	Rule_19005_2_6_1_10_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.10",
		TestNumber:    1,
		Object:        "CosIIFilter",
		Description:   "The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-1:2008, Table 6, or an array containing any such value",
	}

	Rule_19005_2_6_1_12_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.12",
		TestNumber:    1,
		Object:        "PDPerms",
		Description:   "No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-1:2008, 12.8.4, Table 258)",
	}

	Rule_19005_2_6_1_12_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.12",
		TestNumber:    2,
		Object:        "PDSigRef",
		Description:   "If DocMDP is present, then the Signature References dictionary (ISO 32000-1:2008, 12.8.1, Table 253) shall not contain the keys DigestLocation, DigestMethod, and DigestValue",
	}

	Rule_19005_2_6_1_13_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.13",
		TestNumber:    1,
		Object:        "CosInteger",
		Description:   "A conforming file shall not contain any integer greater than 2147483647. A conforming file shall not contain any integer less than -2147483648",
	}

	Rule_19005_2_6_1_13_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.13",
		TestNumber:    2,
		Object:        "CosReal",
		Description:   "A conforming file shall not contain any real number outside the range of +/-3.403 x 10^38",
	}

	Rule_19005_2_6_1_13_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.13",
		TestNumber:    3,
		Object:        "CosString",
		Description:   "A conforming file shall not contain any string longer than 32767 bytes",
	}

	Rule_19005_2_6_1_13_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.13",
		TestNumber:    4,
		Object:        "CosName",
		Description:   "A conforming file shall not contain any name longer than 127 bytes",
	}

	Rule_19005_2_6_1_13_t05 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.13",
		TestNumber:    5,
		Object:        "CosReal",
		Description:   "A conforming file shall not contain any real number closer to zero than +/-1.175 x 10^(-38)",
	}

	Rule_19005_2_6_1_13_t07 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.13",
		TestNumber:    7,
		Object:        "CosDocument",
		Description:   "A conforming file shall not contain more than 8388607 indirect objects",
	}

	Rule_19005_2_6_1_13_t08 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.13",
		TestNumber:    8,
		Object:        "Op_q_gsave",
		Description:   "A conforming file shall not nest q/Q pairs by more than 28 nesting levels",
	}

	Rule_19005_2_6_1_13_t09 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.13",
		TestNumber:    9,
		Object:        "PDDeviceN",
		Description:   "A conforming file shall not contain a DeviceN colour space with more than 32 colourants",
	}

	Rule_19005_2_6_1_13_t10 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.13",
		TestNumber:    10,
		Object:        "CMapFile",
		Description:   "A conforming file shall not contain a CID value greater than 65535",
	}

	Rule_19005_2_6_1_13_t11 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.1.13",
		TestNumber:    11,
		Object:        "CosBBox",
		Description:   "The size of any of the page boundaries described in ISO 32000-1:2008, 14.11.2 shall not be less than 3 units in either direction, nor shall it be greater than 14 400 units in either direction",
	}

	Rule_19005_2_6_2_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.2",
		TestNumber:    1,
		Object:        "Op_Undefined",
		Description:   "Content streams shall not contain any operators not defined in ISO 32000-1 even if such operators are bracketed by the BX/EX compatibility operators",
	}

	Rule_19005_2_6_2_2_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.2",
		TestNumber:    2,
		Object:        "PDContentStream",
		Description:   "A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-1:2008, 7.8.3",
	}

	Rule_19005_2_6_2_3_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.3",
		TestNumber:    1,
		Object:        "ICCOutputProfile",
		Description:   "The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = \"prtr\") or a monitor profile (Device Class = \"mntr\"). The profiles shall have a colour space of either \"GRAY\", \"RGB\", or \"CMYK\"",
	}

	Rule_19005_2_6_2_3_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.3",
		TestNumber:    2,
		Object:        "OutputIntents",
		Description:   "If a file's OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X-4 or PDF/E-1, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream",
	}

	Rule_19005_2_6_2_3_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.3",
		TestNumber:    3,
		Object:        "PDOutputIntent",
		Description:   "In addition, the DestOutputProfileRef key, as defined in ISO 15930-7:2010, Annex A, shall not be present in any PDF/X OutputIntent",
	}

	Rule_19005_2_6_2_4_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.4.2",
		TestNumber:    1,
		Object:        "ICCInputProfile",
		Description:   "The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1",
	}

	Rule_19005_2_6_2_4_2_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.4.2",
		TestNumber:    2,
		Object:        "PDICCBasedCMYK",
		Description:   "Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and overprinting for fill is set to true, or both",
	}

	Rule_19005_2_6_2_4_3_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.4.3",
		TestNumber:    2,
		Object:        "PDDeviceRGB",
		Description:   "DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used, or if the file has a PDF/A OutputIntent that contains an RGB destination profile",
	}

	Rule_19005_2_6_2_4_3_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.4.3",
		TestNumber:    3,
		Object:        "PDDeviceCMYK",
		Description:   "DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set or if a DeviceN-based DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or the file has a PDF/A OutputIntent that contains a CMYK destination profile",
	}

	Rule_19005_2_6_2_4_3_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.4.3",
		TestNumber:    4,
		Object:        "PDDeviceGray",
		Description:   "DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is present",
	}

	Rule_19005_2_6_2_4_4_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.4.4",
		TestNumber:    1,
		Object:        "PDDeviceN",
		Description:   "For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present",
	}

	Rule_19005_2_6_2_4_4_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.4.4",
		TestNumber:    2,
		Object:        "PDSeparation",
		Description:   "All Separation arrays within a single PDF/A-2 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, rather than the computational result of the use of those PDF objects. Compression and whether or not an object is direct or indirect shall be ignored",
	}

	Rule_19005_2_6_2_5_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.5",
		TestNumber:    1,
		Object:        "PDExtGState",
		Description:   "An ExtGState dictionary shall not contain the TR key",
	}

	Rule_19005_2_6_2_5_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.5",
		TestNumber:    2,
		Object:        "PDExtGState",
		Description:   "An ExtGState dictionary shall not contain the TR2 key with a value other than Default",
	}

	Rule_19005_2_6_2_5_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.5",
		TestNumber:    3,
		Object:        "PDExtGState",
		Description:   "An ExtGState dictionary shall not contain the HTP key",
	}

	Rule_19005_2_6_2_5_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.5",
		TestNumber:    4,
		Object:        "PDHalftone",
		Description:   "All halftones in a conforming PDF/A-2 file shall have the value 1 or 5 for the HalftoneType key",
	}

	Rule_19005_2_6_2_5_t05 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.5",
		TestNumber:    5,
		Object:        "PDHalftone",
		Description:   "Halftones in a conforming PDF/A-2 file shall not contain a HalftoneName key",
	}

	Rule_19005_2_6_2_5_t06 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.5",
		TestNumber:    6,
		Object:        "PDHalftone",
		Description:   "The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-1",
	}

	Rule_19005_2_6_2_6_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.6",
		TestNumber:    1,
		Object:        "CosRenderingIntent",
		Description:   "Where a rendering intent is specified, its value shall be one of the four values defined in ISO 32000-1:2008, Table 70: RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation",
	}

	Rule_19005_2_6_2_8_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.8",
		TestNumber:    1,
		Object:        "PDXImage",
		Description:   "An Image dictionary shall not contain the Alternates key",
	}

	Rule_19005_2_6_2_8_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.8",
		TestNumber:    2,
		Object:        "PDXImage",
		Description:   "An Image dictionary shall not contain the OPI key",
	}

	Rule_19005_2_6_2_8_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.8",
		TestNumber:    3,
		Object:        "PDXImage",
		Description:   "If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key shall have a value of false",
	}

	Rule_19005_2_6_2_8_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.8",
		TestNumber:    4,
		Object:        "PDXImage",
		Description:   "If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16",
	}

	Rule_19005_2_6_2_8_t05 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.8",
		TestNumber:    5,
		Object:        "PDMaskImage",
		Description:   "If an image mask dictionary contains the BitsPerComponent key, its value shall be 1",
	}

	Rule_19005_2_6_2_8_3_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.8.3",
		TestNumber:    1,
		Object:        "JPEG2000",
		Description:   "The number of colour channels in the JPEG2000 data shall be 1, 3 or 4",
	}

	Rule_19005_2_6_2_8_3_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.8.3",
		TestNumber:    2,
		Object:        "JPEG2000",
		Description:   "If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field",
	}

	Rule_19005_2_6_2_8_3_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.8.3",
		TestNumber:    3,
		Object:        "JPEG2000",
		Description:   "The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming reader shall use only that colour space and shall ignore all other colour space specifications",
	}

	Rule_19005_2_6_2_8_3_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.8.3",
		TestNumber:    4,
		Object:        "JPEG2000",
		Description:   "JPEG2000 enumerated colour space 19 (CIEJab) shall not be used",
	}

	Rule_19005_2_6_2_8_3_t05 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.8.3",
		TestNumber:    5,
		Object:        "JPEG2000",
		Description:   "The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth",
	}

	Rule_19005_2_6_2_9_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.9",
		TestNumber:    1,
		Object:        "PDXForm",
		Description:   "A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key",
	}

	Rule_19005_2_6_2_9_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.9",
		TestNumber:    2,
		Object:        "PDXForm",
		Description:   "A conforming file shall not contain any reference XObjects",
	}

	Rule_19005_2_6_2_9_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.9",
		TestNumber:    3,
		Object:        "PDXObject",
		Description:   "A conforming file shall not contain any PostScript XObjects",
	}

	Rule_19005_2_6_2_10_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.10",
		TestNumber:    1,
		Object:        "CosBM",
		Description:   "Only blend modes that are specified in ISO 32000-1:2008 shall be used for the value of the BM key in an extended graphic state dictionary",
	}

	Rule_19005_2_6_2_10_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.10",
		TestNumber:    2,
		Object:        "PDPage",
		Description:   "If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space",
	}

	Rule_19005_2_6_2_11_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.2",
		TestNumber:    1,
		Object:        "PDFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary",
	}

	Rule_19005_2_6_2_11_2_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.2",
		TestNumber:    2,
		Object:        "PDFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Required) The type of font; must be \"Type1\" for Type 1 fonts, \"MMType1\" for multiple master fonts, \"TrueType\" for TrueType fonts \"Type3\" for Type 3 fonts, \"Type0\" for Type 0 fonts and \"CIDFontType0\" or \"CIDFontType2\" for CID fonts",
	}

	Rule_19005_2_6_2_11_2_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.2",
		TestNumber:    3,
		Object:        "PDFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (Required) The PostScript name of the font",
	}

	Rule_19005_2_6_2_11_2_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.2",
		TestNumber:    4,
		Object:        "PDSimpleFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array",
	}

	Rule_19005_2_6_2_11_2_t05 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.2",
		TestNumber:    5,
		Object:        "PDSimpleFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array",
	}

	Rule_19005_2_6_2_11_2_t06 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.2",
		TestNumber:    6,
		Object:        "PDSimpleFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths",
	}

	Rule_19005_2_6_2_11_2_t07 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.2",
		TestNumber:    7,
		Object:        "PDFont",
		Description:   "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.7 are Type1C - Type 1–equivalent font program represented in the Compact Font Format (CFF), CIDFontType0C - Type 0 CIDFont program represented in the Compact Font Format (CFF) and OpenType - OpenType® font program, as described in the OpenType Specification v.1.4",
	}

	Rule_19005_2_6_2_11_3_1_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.3.1",
		TestNumber:    1,
		Object:        "PDType0Font",
		Description:   "For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary is Identity-H or Identity-V, any values of Registry, Ordering, and Supplement may be used in the CIDSystemInfo entry of the CIDFont. - Otherwise, the corresponding Registry and Ordering strings in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be less than or equal to the Supplement key in the CIDSystemInfo dictionary of the CMap",
	}

	Rule_19005_2_6_2_11_3_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.3.2",
		TestNumber:    1,
		Object:        "PDCIDFont",
		Description:   "ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2008, 9.7.4, Table 117",
	}

	Rule_19005_2_6_2_11_3_3_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.3.3",
		TestNumber:    1,
		Object:        "PDCMap",
		Description:   "All CMaps used within a PDF/A-2 file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5",
	}

	Rule_19005_2_6_2_11_3_3_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.3.3",
		TestNumber:    2,
		Object:        "CMapFile",
		Description:   "For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream",
	}

	Rule_19005_2_6_2_11_3_3_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.3.3",
		TestNumber:    3,
		Object:        "PDReferencedCMap",
		Description:   "A CMap shall not reference any other CMap except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118",
	}

	Rule_19005_2_6_2_11_4_1_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.4.1",
		TestNumber:    1,
		Object:        "PDFont",
		Description:   "The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9",
	}

	Rule_19005_2_6_2_11_4_1_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.4.1",
		TestNumber:    2,
		Object:        "Glyph",
		Description:   "Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType fonts that are to be rendered, character codes shall be able to be mapped to glyphs according to ISO 32000-1:2008, 9.6.6.4 without the use of a non-standard mapping chosen by the conforming processor",
	}

	Rule_19005_2_6_2_11_4_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.4.2",
		TestNumber:    1,
		Object:        "PDType1Font",
		Description:   "If the FontDescriptor dictionary of an embedded Type 1 font contains a CharSet string, then it shall list the character names of all glyphs present in the font program, regardless of whether a glyph in the font is referenced or used by the PDF or not",
	}

	Rule_19005_2_6_2_11_4_2_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.4.2",
		TestNumber:    2,
		Object:        "PDCIDFont",
		Description:   "If the FontDescriptor dictionary of an embedded CID font contains a CIDSet stream, then it shall identify all CIDs which are present in the font program, regardless of whether a CID in the font is referenced or used by the PDF or not",
	}

	Rule_19005_2_6_2_11_5_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.5",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent",
	}

	Rule_19005_2_6_2_11_6_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.6",
		TestNumber:    1,
		Object:        "TrueTypeFontProgram",
		Description:   "For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain one or several non-symbolic cmap entries such that all necessary glyph lookups can be carried out",
	}

	Rule_19005_2_6_2_11_6_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.6",
		TestNumber:    2,
		Object:        "PDTrueTypeFont",
		Description:   "All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary that is the value of the Encoding key in the Font dictionary. In addition, all non-symbolic TrueType fonts shall not define a Differences array unless all of the glyph names in the Differences array are listed in the Adobe Glyph List and, if the font program is embedded, it shall contain at least the Microsoft Unicode (3,1 – Platform ID = 3, Encoding ID = 1) encoding in the 'cmap' table",
	}

	Rule_19005_2_6_2_11_6_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.6",
		TestNumber:    3,
		Object:        "PDTrueTypeFont",
		Description:   "Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary",
	}

	Rule_19005_2_6_2_11_6_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.6",
		TestNumber:    4,
		Object:        "TrueTypeFontProgram",
		Description:   "The 'cmap' table in the embedded font program for a symbolic TrueType font shall contain either exactly one encoding or it shall contain, at least, the Microsoft Symbol (3,0 - Platform ID=3, Encoding ID=0) encoding",
	}

	Rule_19005_2_6_2_11_7_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.7.2",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 19005-2, 6.2.11.7.2",
	}

	Rule_19005_2_6_2_11_7_2_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.7.2",
		TestNumber:    2,
		Object:        "Glyph",
		Description:   "The Unicode values specified in the ToUnicode CMap shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE",
	}

	Rule_19005_2_6_2_11_7_3_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.7.3",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "For any character, regardless of its rendering mode, that is mapped to a code or codes in the Unicode Private Use Area (PUA), an ActualText entry as described in ISO 32000-1:2008, 14.9.4 shall be present for this character or a sequence of characters of which such a character is a part",
	}

	Rule_19005_2_6_2_11_8_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.2.11.8",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "A PDF/A-2 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream",
	}

	Rule_19005_2_6_3_1_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.3.1",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Annotation types not defined in ISO 32000-1 shall not be permitted. Additionally, the 3D, Sound, Screen and Movie types shall not be permitted",
	}

	Rule_19005_2_6_3_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.3.2",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key",
	}

	Rule_19005_2_6_3_2_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.3.2",
		TestNumber:    2,
		Object:        "PDAnnot",
		Description:   "If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0",
	}

	Rule_19005_2_6_3_3_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.3.3",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: - annotations where the value of the Rect key consists of an array where value 1 is equal to value 3 and value 2 is equal to value 4; - annotations whose Subtype value is Popup or Link",
	}

	Rule_19005_2_6_3_3_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.3.3",
		TestNumber:    2,
		Object:        "PDAnnot",
		Description:   "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key",
	}

	Rule_19005_2_6_3_3_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.3.3",
		TestNumber:    3,
		Object:        "PDAnnot",
		Description:   "If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary",
	}

	Rule_19005_2_6_3_3_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.3.3",
		TestNumber:    4,
		Object:        "PDAnnot",
		Description:   "If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream",
	}

	Rule_19005_2_6_4_1_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.4.1",
		TestNumber:    1,
		Object:        "PDWidgetAnnot",
		Description:   "A Widget annotation dictionary shall not contain the A or AA keys",
	}

	Rule_19005_2_6_4_1_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.4.1",
		TestNumber:    2,
		Object:        "PDFormField",
		Description:   "A Field dictionary shall not contain the A or AA keys",
	}

	Rule_19005_2_6_4_1_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.4.1",
		TestNumber:    3,
		Object:        "PDAcroForm",
		Description:   "The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false",
	}

	Rule_19005_2_6_4_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.4.2",
		TestNumber:    1,
		Object:        "PDAcroForm",
		Description:   "The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-2 file, if present, shall not contain the XFA key",
	}

	Rule_19005_2_6_4_2_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.4.2",
		TestNumber:    2,
		Object:        "CosDocument",
		Description:   "A document's Catalog shall not contain the NeedsRendering key",
	}

	Rule_19005_2_6_4_3_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.4.3",
		TestNumber:    1,
		Object:        "PDSignature",
		Description:   "When computing the digest for the file, it shall be computed over the entire file, including the signature dictionary but excluding the PDF Signature itself",
	}

	Rule_19005_2_6_4_3_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.4.3",
		TestNumber:    2,
		Object:        "PKCSDataObject",
		Description:   "The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, it shall include the signer's X.509 signing certificate",
	}

	Rule_19005_2_6_4_3_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.4.3",
		TestNumber:    3,
		Object:        "PKCSDataObject",
		Description:   "The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, there shall only be a single signer (e.g. a single \"SignerInfo\" structure) in the PDF Signature",
	}

	Rule_19005_2_6_5_1_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.5.1",
		TestNumber:    1,
		Object:        "PDAction",
		Description:   "The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted",
	}

	Rule_19005_2_6_5_1_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.5.1",
		TestNumber:    2,
		Object:        "PDNamedAction",
		Description:   "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted",
	}

	Rule_19005_2_6_5_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.5.2",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "The document's Catalog shall not include an AA entry for an additional-actions dictionary",
	}

	Rule_19005_2_6_5_2_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.5.2",
		TestNumber:    2,
		Object:        "PDPage",
		Description:   "The Page dictionary shall not include an AA entry for an additional-actions dictionary",
	}

	Rule_19005_2_6_6_2_1_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.1",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-1:2008, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML",
	}

	Rule_19005_2_6_6_2_1_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.1",
		TestNumber:    2,
		Object:        "XMPPackage",
		Description:   "The bytes attribute shall not be used in the header of an XMP packet",
	}

	Rule_19005_2_6_6_2_1_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.1",
		TestNumber:    3,
		Object:        "XMPPackage",
		Description:   "The encoding attribute shall not be used in the header of an XMP packet",
	}

	Rule_19005_2_6_6_2_1_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.1",
		TestNumber:    4,
		Object:        "XMPPackage",
		Description:   "All metadata streams present in the PDF shall conform to the XMP Specification. All content of all XMP packets shall be well-formed, as defined by Extensible Markup Language (XML) 1.0 (Third Edition), 2.1, and the RDF/XML Syntax Specification (Revised)",
	}

	Rule_19005_2_6_6_2_1_t05 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.1",
		TestNumber:    5,
		Object:        "XMPPackage",
		Description:   "All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8",
	}

	Rule_19005_2_6_6_2_3_1_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.1",
		TestNumber:    1,
		Object:        "XMPProperty",
		Description:   "All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2",
	}

	Rule_19005_2_6_6_2_3_1_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.1",
		TestNumber:    2,
		Object:        "XMPProperty",
		Description:   "All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2",
	}

	Rule_19005_2_6_6_2_3_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.2",
		TestNumber:    1,
		Object:        "ExtensionSchemaObject",
		Description:   "Extension schemas shall be specified using the PDF/A extension schema container schema defined in 6.6.2.3.3. All fields described in each of the tables in 6.6.2.3.3 shall be present in any extension schema container schema",
	}

	Rule_19005_2_6_6_2_3_3_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    1,
		Object:        "ExtensionSchemasContainer",
		Description:   "The extension schema container schema uses the namespace URI \"http://www.aiim.org/pdfa/ns/extension/\". The required schema namespace prefix is pdfaExtension. pdfaExtension:schemas - Bag Schema - Description of extension schemas",
	}

	Rule_19005_2_6_6_2_3_3_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    2,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_2_6_6_2_3_3_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    3,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_2_6_6_2_3_3_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    4,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_2_6_6_2_3_3_t05 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    5,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_2_6_6_2_3_3_t06 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    6,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_2_6_6_2_3_3_t07 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    7,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'",
	}

	Rule_19005_2_6_6_2_3_3_t08 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    8,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined",
	}

	Rule_19005_2_6_6_2_3_3_t09 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    9,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty'",
	}

	Rule_19005_2_6_6_2_3_3_t10 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    10,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'",
	}

	Rule_19005_2_6_6_2_3_3_t11 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    11,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'",
	}

	Rule_19005_2_6_6_2_3_3_t12 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    12,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType'",
	}

	Rule_19005_2_6_6_2_3_3_t13 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    13,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'",
	}

	Rule_19005_2_6_6_2_3_3_t14 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    14,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'",
	}

	Rule_19005_2_6_6_2_3_3_t15 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    15,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType'",
	}

	Rule_19005_2_6_6_2_3_3_t16 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    16,
		Object:        "ExtensionSchemaField",
		Description:   "Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'",
	}

	Rule_19005_2_6_6_2_3_3_t17 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    17,
		Object:        "ExtensionSchemaField",
		Description:   "Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined",
	}

	Rule_19005_2_6_6_2_3_3_t18 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.2.3.3",
		TestNumber:    18,
		Object:        "ExtensionSchemaField",
		Description:   "Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'",
	}

	Rule_19005_2_6_6_4_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.4",
		TestNumber:    1,
		Object:        "MainXMPPackage",
		Description:   "The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema",
	}

	Rule_19005_2_6_6_4_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.4",
		TestNumber:    2,
		Object:        "PDFAIdentification",
		Description:   "The value of \"pdfaid:part\" shall be the part number of ISO 19005 to which the file conforms",
	}

	Rule_19005_2_6_6_4_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.4",
		TestNumber:    3,
		Object:        "PDFAIdentification",
		Description:   "A Level A conforming file shall specify the value of \"pdfaid:conformance\" as A. A Level B conforming file shall specify the value of \"pdfaid:conformance\" as B. A Level U conforming file shall specify the value of \"pdfaid:conformance\" as U",
	}

	Rule_19005_2_6_6_4_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.4",
		TestNumber:    4,
		Object:        "PDFAIdentification",
		Description:   "Property \"part\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_2_6_6_4_t05 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.4",
		TestNumber:    5,
		Object:        "PDFAIdentification",
		Description:   "Property \"conformance\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_2_6_6_4_t06 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.4",
		TestNumber:    6,
		Object:        "PDFAIdentification",
		Description:   "Property \"amd\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_2_6_6_4_t07 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.6.4",
		TestNumber:    7,
		Object:        "PDFAIdentification",
		Description:   "Property \"corr\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_2_6_7_2_2_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.7.2.2",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The document catalog dictionary shall include a MarkInfo dictionary containing an entry, Marked, whose value shall be true",
	}

	Rule_19005_2_6_7_3_3_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.7.3.3",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document's Catalog dictionary, as described in ISO 32000-1:2008, 14.7",
	}

	Rule_19005_2_6_7_3_4_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.7.3.4",
		TestNumber:    1,
		Object:        "SENonStandard",
		Description:   "All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in ISO 32000-1:2008, 14.8.4, in the role map dictionary of the structure tree root",
	}

	Rule_19005_2_6_7_3_4_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.7.3.4",
		TestNumber:    2,
		Object:        "SENonStandard",
		Description:   "A circular mapping shall not exist",
	}

	Rule_19005_2_6_7_3_4_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.7.3.4",
		TestNumber:    3,
		Object:        "SENonStandard",
		Description:   "Standard tags shall not be remapped to a non-standard type",
	}

	Rule_19005_2_6_7_4_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.7.4",
		TestNumber:    1,
		Object:        "CosLang",
		Description:   "If the Lang entry is present in the document's Catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as described in ISO 32000-1:2008, 14.9.2. A language identifier shall either be the empty text string, to indicate that the language is unknown, or a Language-Tag as defined in RFC 3066, Tags for the Identification of Languages",
	}

	Rule_19005_2_6_8_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.8",
		TestNumber:    2,
		Object:        "CosFileSpecification",
		Description:   "The file specification dictionary for an embedded file shall contain the F and UF keys",
	}

	Rule_19005_2_6_8_t05 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.8",
		TestNumber:    5,
		Object:        "EmbeddedFile",
		Description:   "A file specification dictionary, as defined in ISO 32000-1:2008, 7.11.3, may contain the EF key, provided that the embedded file is compliant with either ISO 19005-1 or this part of ISO 19005",
	}

	Rule_19005_2_6_9_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.9",
		TestNumber:    1,
		Object:        "PDOCConfig",
		Description:   "Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key",
	}

	Rule_19005_2_6_9_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.9",
		TestNumber:    2,
		Object:        "PDOCConfig",
		Description:   "Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-2 file",
	}

	Rule_19005_2_6_9_t03 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.9",
		TestNumber:    3,
		Object:        "PDOCConfig",
		Description:   "If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file",
	}

	Rule_19005_2_6_9_t04 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.9",
		TestNumber:    4,
		Object:        "PDOCConfig",
		Description:   "The AS key shall not appear in any optional content configuration dictionary",
	}

	Rule_19005_2_6_10_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.10",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "There shall be no AlternatePresentations entry in the document's name dictionary",
	}

	Rule_19005_2_6_10_t02 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.10",
		TestNumber:    2,
		Object:        "PDPage",
		Description:   "There shall be no PresSteps entry in any Page dictionary",
	}

	Rule_19005_2_6_11_t01 = Rule{
		Specification: "ISO_19005_2",
		Clause:        "6.11",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The document catalog shall not contain the Requirements key",
	}

	// ISO 19005-3:2012 (PDF/A-3)

	Rule_19005_3_6_1_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.2",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The file header shall begin at byte zero and shall consist of \"%PDF-1.n\" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 7 (37h)",
	}

	Rule_19005_3_6_1_2_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.2",
		TestNumber:    2,
		Object:        "CosDocument",
		Description:   "The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127",
	}

	Rule_19005_3_6_1_3_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.3",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The file trailer dictionary shall contain the ID keyword whose value shall be File Identifiers as defined in ISO 32000-1:2008, 14.4",
	}

	Rule_19005_3_6_1_3_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.3",
		TestNumber:    2,
		Object:        "CosTrailer",
		Description:   "The keyword Encrypt shall not be used in the trailer dictionary",
	}

	Rule_19005_3_6_1_3_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.3",
		TestNumber:    3,
		Object:        "CosDocument",
		Description:   "No data can follow the last end-of-file marker except a single optional end-of-line marker as described in ISO 32000-1:2008, 7.5.5",
	}

	Rule_19005_3_6_1_4_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.4",
		TestNumber:    2,
		Object:        "CosXRef",
		Description:   "The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker",
	}

	Rule_19005_3_6_1_6_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.6",
		TestNumber:    1,
		Object:        "CosString",
		Description:   "Hexadecimal strings shall contain an even number of non-white-space characters",
	}

	Rule_19005_3_6_1_6_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.6",
		TestNumber:    2,
		Object:        "CosString",
		Description:   "A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f)",
	}

	Rule_19005_3_6_1_7_1_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.7.1",
		TestNumber:    1,
		Object:        "CosStream",
		Description:   "The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword",
	}

	Rule_19005_3_6_1_7_1_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.7.1",
		TestNumber:    2,
		Object:        "CosStream",
		Description:   "The stream keyword shall be followed either by a CARRIAGE RETURN (0Dh) and LINE FEED (0Ah) character sequence or by a single LINE FEED (0Ah) character. The endstream keyword shall be preceded by an EOL marker",
	}

	Rule_19005_3_6_1_7_1_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.7.1",
		TestNumber:    3,
		Object:        "CosStream",
		Description:   "A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys",
	}

	Rule_19005_3_6_1_7_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.7.2",
		TestNumber:    1,
		Object:        "CosFilter",
		Description:   "All standard stream filters listed in ISO 32000-1:2008, 7.4, Table 6 may be used, with the exception of LZWDecode. In addition, the Crypt filter shall not be used unless the value of the Name key in the decode parameters dictionary is Identity. Filters that are not listed in ISO 32000-1:2008, 7.4, Table 6 shall not be used",
	}

	Rule_19005_3_6_1_8_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.8",
		TestNumber:    1,
		Object:        "CosUnicodeName",
		Description:   "Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences",
	}

	Rule_19005_3_6_1_9_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.9",
		TestNumber:    1,
		Object:        "CosIndirect",
		Description:   "The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded by an EOL marker. The obj and endobj keywords shall each be followed by an EOL marker",
	}

	Rule_19005_3_6_1_10_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.10",
		TestNumber:    1,
		Object:        "CosIIFilter",
		Description:   "The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-1:2008, Table 6, or an array containing any such value",
	}

	Rule_19005_3_6_1_12_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.12",
		TestNumber:    1,
		Object:        "PDPerms",
		Description:   "No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-1:2008, 12.8.4, Table 258)",
	}

	Rule_19005_3_6_1_12_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.12",
		TestNumber:    2,
		Object:        "PDSigRef",
		Description:   "If DocMDP is present, then the Signature References dictionary (ISO 32000-1:2008, 12.8.1, Table 253) shall not contain the keys DigestLocation, DigestMethod, and DigestValue",
	}

	Rule_19005_3_6_1_13_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.13",
		TestNumber:    1,
		Object:        "CosInteger",
		Description:   "A conforming file shall not contain any integer greater than 2147483647. A conforming file shall not contain any integer less than -2147483648",
	}

	Rule_19005_3_6_1_13_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.13",
		TestNumber:    2,
		Object:        "CosReal",
		Description:   "A conforming file shall not contain any real number outside the range of +/-3.403 x 10^38",
	}

	Rule_19005_3_6_1_13_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.13",
		TestNumber:    3,
		Object:        "CosString",
		Description:   "A conforming file shall not contain any string longer than 32767 bytes",
	}

	Rule_19005_3_6_1_13_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.13",
		TestNumber:    4,
		Object:        "CosName",
		Description:   "A conforming file shall not contain any name longer than 127 bytes",
	}

	Rule_19005_3_6_1_13_t05 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.13",
		TestNumber:    5,
		Object:        "CosReal",
		Description:   "A conforming file shall not contain any real number closer to zero than +/-1.175 x 10^(-38)",
	}

	Rule_19005_3_6_1_13_t07 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.13",
		TestNumber:    7,
		Object:        "CosDocument",
		Description:   "A conforming file shall not contain more than 8388607 indirect objects",
	}

	Rule_19005_3_6_1_13_t08 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.13",
		TestNumber:    8,
		Object:        "Op_q_gsave",
		Description:   "A conforming file shall not nest q/Q pairs by more than 28 nesting levels",
	}

	Rule_19005_3_6_1_13_t09 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.13",
		TestNumber:    9,
		Object:        "PDDeviceN",
		Description:   "A conforming file shall not contain a DeviceN colour space with more than 32 colourants",
	}

	Rule_19005_3_6_1_13_t10 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.13",
		TestNumber:    10,
		Object:        "CMapFile",
		Description:   "A conforming file shall not contain a CID value greater than 65535",
	}

	Rule_19005_3_6_1_13_t11 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.1.13",
		TestNumber:    11,
		Object:        "CosBBox",
		Description:   "The size of any of the page boundaries described in ISO 32000-1:2008, 14.11.2 shall not be less than 3 units in either direction, nor shall it be greater than 14 400 units in either direction",
	}

	Rule_19005_3_6_2_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.2",
		TestNumber:    1,
		Object:        "Op_Undefined",
		Description:   "Content streams shall not contain any operators not defined in ISO 32000-1 even if such operators are bracketed by the BX/EX compatibility operators",
	}

	Rule_19005_3_6_2_2_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.2",
		TestNumber:    2,
		Object:        "PDContentStream",
		Description:   "A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-1:2008, 7.8.3",
	}

	Rule_19005_3_6_2_3_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.3",
		TestNumber:    1,
		Object:        "ICCOutputProfile",
		Description:   "The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = \"prtr\") or a monitor profile (Device Class = \"mntr\"). The profiles shall have a colour space of either \"GRAY\", \"RGB\", or \"CMYK\"",
	}

	Rule_19005_3_6_2_3_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.3",
		TestNumber:    2,
		Object:        "OutputIntents",
		Description:   "If a file's OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X-4 or PDF/E-1, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream",
	}

	Rule_19005_3_6_2_3_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.3",
		TestNumber:    3,
		Object:        "PDOutputIntent",
		Description:   "In addition, the DestOutputProfileRef key, as defined in ISO 15930-7:2010, Annex A, shall not be present in any PDF/X OutputIntent",
	}

	Rule_19005_3_6_2_4_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.4.2",
		TestNumber:    1,
		Object:        "ICCInputProfile",
		Description:   "The profile that forms the stream of an ICCBased colour space shall conform to ICC.1:1998-09, ICC.1:2001-12, ICC.1:2003-09 or ISO 15076-1",
	}

	Rule_19005_3_6_2_4_2_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.4.2",
		TestNumber:    2,
		Object:        "PDICCBasedCMYK",
		Description:   "Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and overprinting for fill is set to true, or both",
	}

	Rule_19005_3_6_2_4_3_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.4.3",
		TestNumber:    2,
		Object:        "PDDeviceRGB",
		Description:   "DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used, or if the file has a PDF/A OutputIntent that contains an RGB destination profile",
	}

	Rule_19005_3_6_2_4_3_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.4.3",
		TestNumber:    3,
		Object:        "PDDeviceCMYK",
		Description:   "DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set or if a DeviceN-based DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or the file has a PDF/A OutputIntent that contains a CMYK destination profile",
	}

	Rule_19005_3_6_2_4_3_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.4.3",
		TestNumber:    4,
		Object:        "PDDeviceGray",
		Description:   "DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is present",
	}

	Rule_19005_3_6_2_4_4_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.4.4",
		TestNumber:    1,
		Object:        "PDDeviceN",
		Description:   "For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present",
	}

	Rule_19005_3_6_2_4_4_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.4.4",
		TestNumber:    2,
		Object:        "PDSeparation",
		Description:   "All Separation arrays within a single PDF/A-3 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, rather than the computational result of the use of those PDF objects. Compression and whether or not an object is direct or indirect shall be ignored",
	}

	Rule_19005_3_6_2_5_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.5",
		TestNumber:    1,
		Object:        "PDExtGState",
		Description:   "An ExtGState dictionary shall not contain the TR key",
	}

	Rule_19005_3_6_2_5_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.5",
		TestNumber:    2,
		Object:        "PDExtGState",
		Description:   "An ExtGState dictionary shall not contain the TR2 key with a value other than Default",
	}

	Rule_19005_3_6_2_5_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.5",
		TestNumber:    3,
		Object:        "PDExtGState",
		Description:   "An ExtGState dictionary shall not contain the HTP key",
	}

	Rule_19005_3_6_2_5_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.5",
		TestNumber:    4,
		Object:        "PDHalftone",
		Description:   "All halftones in a conforming PDF/A-3 file shall have the value 1 or 5 for the HalftoneType key",
	}

	Rule_19005_3_6_2_5_t05 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.5",
		TestNumber:    5,
		Object:        "PDHalftone",
		Description:   "Halftones in a conforming PDF/A-3 file shall not contain a HalftoneName key",
	}

	Rule_19005_3_6_2_5_t06 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.5",
		TestNumber:    6,
		Object:        "PDHalftone",
		Description:   "The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-1",
	}

	Rule_19005_3_6_2_6_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.6",
		TestNumber:    1,
		Object:        "CosRenderingIntent",
		Description:   "Where a rendering intent is specified, its value shall be one of the four values defined in ISO 32000-1:2008, Table 70: RelativeColorimetric, AbsoluteColorimetric, Perceptual or Saturation",
	}

	Rule_19005_3_6_2_8_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.8",
		TestNumber:    1,
		Object:        "PDXImage",
		Description:   "An Image dictionary shall not contain the Alternates key",
	}

	Rule_19005_3_6_2_8_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.8",
		TestNumber:    2,
		Object:        "PDXImage",
		Description:   "An Image dictionary shall not contain the OPI key",
	}

	Rule_19005_3_6_2_8_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.8",
		TestNumber:    3,
		Object:        "PDXImage",
		Description:   "If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key shall have a value of false",
	}

	Rule_19005_3_6_2_8_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.8",
		TestNumber:    4,
		Object:        "PDXImage",
		Description:   "If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16",
	}

	Rule_19005_3_6_2_8_t05 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.8",
		TestNumber:    5,
		Object:        "PDMaskImage",
		Description:   "If an image mask dictionary contains the BitsPerComponent key, its value shall be 1",
	}

	Rule_19005_3_6_2_8_3_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.8.3",
		TestNumber:    1,
		Object:        "JPEG2000",
		Description:   "The number of colour channels in the JPEG2000 data shall be 1, 3 or 4",
	}

	Rule_19005_3_6_2_8_3_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.8.3",
		TestNumber:    2,
		Object:        "JPEG2000",
		Description:   "If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field",
	}

	Rule_19005_3_6_2_8_3_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.8.3",
		TestNumber:    3,
		Object:        "JPEG2000",
		Description:   "The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming reader shall use only that colour space and shall ignore all other colour space specifications",
	}

	Rule_19005_3_6_2_8_3_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.8.3",
		TestNumber:    4,
		Object:        "JPEG2000",
		Description:   "JPEG2000 enumerated colour space 19 (CIEJab) shall not be used",
	}

	Rule_19005_3_6_2_8_3_t05 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.8.3",
		TestNumber:    5,
		Object:        "JPEG2000",
		Description:   "The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth",
	}

	Rule_19005_3_6_2_9_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.9",
		TestNumber:    1,
		Object:        "PDXForm",
		Description:   "A form XObject dictionary shall not contain any of the following: - the OPI key; - the Subtype2 key with a value of PS; - the PS key",
	}

	Rule_19005_3_6_2_9_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.9",
		TestNumber:    2,
		Object:        "PDXForm",
		Description:   "A conforming file shall not contain any reference XObjects",
	}

	Rule_19005_3_6_2_9_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.9",
		TestNumber:    3,
		Object:        "PDXObject",
		Description:   "A conforming file shall not contain any PostScript XObjects",
	}

	Rule_19005_3_6_2_10_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.10",
		TestNumber:    1,
		Object:        "CosBM",
		Description:   "Only blend modes that are specified in ISO 32000-1:2008 shall be used for the value of the BM key in an extended graphic state dictionary",
	}

	Rule_19005_3_6_2_10_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.10",
		TestNumber:    2,
		Object:        "PDPage",
		Description:   "If the document does not contain a PDF/A OutputIntent, then all Page objects that contain transparency shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space",
	}

	Rule_19005_3_6_2_11_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.2",
		TestNumber:    1,
		Object:        "PDFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary",
	}

	Rule_19005_3_6_2_11_2_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.2",
		TestNumber:    2,
		Object:        "PDFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Required) The type of font; must be \"Type1\" for Type 1 fonts, \"MMType1\" for multiple master fonts, \"TrueType\" for TrueType fonts \"Type3\" for Type 3 fonts, \"Type0\" for Type 0 fonts and \"CIDFontType0\" or \"CIDFontType2\" for CID fonts",
	}

	Rule_19005_3_6_2_11_2_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.2",
		TestNumber:    3,
		Object:        "PDFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (Required) The PostScript name of the font",
	}

	Rule_19005_3_6_2_11_2_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.2",
		TestNumber:    4,
		Object:        "PDSimpleFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array",
	}

	Rule_19005_3_6_2_11_2_t05 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.2",
		TestNumber:    5,
		Object:        "PDSimpleFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array",
	}

	Rule_19005_3_6_2_11_2_t06 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.2",
		TestNumber:    6,
		Object:        "PDSimpleFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-1:2008, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths",
	}

	Rule_19005_3_6_2_11_2_t07 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.2",
		TestNumber:    7,
		Object:        "PDFont",
		Description:   "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 1.7 are Type1C - Type 1–equivalent font program represented in the Compact Font Format (CFF), CIDFontType0C - Type 0 CIDFont program represented in the Compact Font Format (CFF) and OpenType - OpenType® font program, as described in the OpenType Specification v.1.4",
	}

	Rule_19005_3_6_2_11_3_1_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.3.1",
		TestNumber:    1,
		Object:        "PDType0Font",
		Description:   "For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary is Identity-H or Identity-V, any values of Registry, Ordering, and Supplement may be used in the CIDSystemInfo entry of the CIDFont. - Otherwise, the corresponding Registry and Ordering strings in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be less than or equal to the Supplement key in the CIDSystemInfo dictionary of the CMap",
	}

	Rule_19005_3_6_2_11_3_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.3.2",
		TestNumber:    1,
		Object:        "PDCIDFont",
		Description:   "ISO 32000-1:2008, 9.7.4, Table 117 requires that all embedded Type 2 CIDFonts in the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-1:2008, 9.7.4, Table 117",
	}

	Rule_19005_3_6_2_11_3_3_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.3.3",
		TestNumber:    1,
		Object:        "PDCMap",
		Description:   "All CMaps used within a PDF/A-3 file, except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118, shall be embedded in that file as described in ISO 32000-1:2008, 9.7.5",
	}

	Rule_19005_3_6_2_11_3_3_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.3.3",
		TestNumber:    2,
		Object:        "CMapFile",
		Description:   "For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream",
	}

	Rule_19005_3_6_2_11_3_3_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.3.3",
		TestNumber:    3,
		Object:        "PDReferencedCMap",
		Description:   "A CMap shall not reference any other CMap except those listed in ISO 32000-1:2008, 9.7.5.2, Table 118",
	}

	Rule_19005_3_6_2_11_4_1_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.4.1",
		TestNumber:    1,
		Object:        "PDFont",
		Description:   "The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-1:2008, 9.9",
	}

	Rule_19005_3_6_2_11_4_1_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.4.1",
		TestNumber:    2,
		Object:        "Glyph",
		Description:   "Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType fonts that are to be rendered, character codes shall be able to be mapped to glyphs according to ISO 32000-1:2008, 9.6.6.4 without the use of a non-standard mapping chosen by the conforming processor",
	}

	Rule_19005_3_6_2_11_4_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.4.2",
		TestNumber:    1,
		Object:        "PDType1Font",
		Description:   "If the FontDescriptor dictionary of an embedded Type 1 font contains a CharSet string, then it shall list the character names of all glyphs present in the font program, regardless of whether a glyph in the font is referenced or used by the PDF or not",
	}

	Rule_19005_3_6_2_11_4_2_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.4.2",
		TestNumber:    2,
		Object:        "PDCIDFont",
		Description:   "If the FontDescriptor dictionary of an embedded CID font contains a CIDSet stream, then it shall identify all CIDs which are present in the font program, regardless of whether a CID in the font is referenced or used by the PDF or not",
	}

	Rule_19005_3_6_2_11_5_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.5",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "For every font embedded in a conforming file and used for rendering, the glyph width information in the font dictionary and in the embedded font program shall be consistent",
	}

	Rule_19005_3_6_2_11_6_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.6",
		TestNumber:    1,
		Object:        "TrueTypeFontProgram",
		Description:   "For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain one or several non-symbolic cmap entries such that all necessary glyph lookups can be carried out",
	}

	Rule_19005_3_6_2_11_6_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.6",
		TestNumber:    2,
		Object:        "PDTrueTypeFont",
		Description:   "All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary that is the value of the Encoding key in the Font dictionary. In addition, all non-symbolic TrueType fonts shall not define a Differences array unless all of the glyph names in the Differences array are listed in the Adobe Glyph List and, if the font program is embedded, it shall contain at least the Microsoft Unicode (3,1 – Platform ID = 3, Encoding ID = 1) encoding in the 'cmap' table",
	}

	Rule_19005_3_6_2_11_6_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.6",
		TestNumber:    3,
		Object:        "PDTrueTypeFont",
		Description:   "Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary",
	}

	Rule_19005_3_6_2_11_6_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.6",
		TestNumber:    4,
		Object:        "TrueTypeFontProgram",
		Description:   "The 'cmap' table in the embedded font program for a symbolic TrueType font shall contain either exactly one encoding or it shall contain, at least, the Microsoft Symbol (3,0 - Platform ID=3, Encoding ID=0) encoding",
	}

	Rule_19005_3_6_2_11_7_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.7.2",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "The Font dictionary of all fonts shall define the map of all used character codes to Unicode values, either via a ToUnicode entry, or other mechanisms as defined in ISO 19005-3, 6.2.11.7.2",
	}

	Rule_19005_3_6_2_11_7_2_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.7.2",
		TestNumber:    2,
		Object:        "Glyph",
		Description:   "The Unicode values specified in the ToUnicode CMap shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE",
	}

	Rule_19005_3_6_2_11_7_3_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.7.3",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "For any character, regardless of its rendering mode, that is mapped to a code or codes in the Unicode Private Use Area (PUA), an ActualText entry as described in ISO 32000-1:2008, 14.9.4 shall be present for this character or a sequence of characters of which such a character is a part",
	}

	Rule_19005_3_6_2_11_8_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.2.11.8",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "A PDF/A-3 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream",
	}

	Rule_19005_3_6_3_1_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.3.1",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Annotation types not defined in ISO 32000-1 shall not be permitted. Additionally, the 3D, Sound, Screen and Movie types shall not be permitted",
	}

	Rule_19005_3_6_3_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.3.2",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key",
	}

	Rule_19005_3_6_3_2_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.3.2",
		TestNumber:    2,
		Object:        "PDAnnot",
		Description:   "If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0",
	}

	Rule_19005_3_6_3_3_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.3.3",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: - annotations where the value of the Rect key consists of an array where value 1 is equal to value 3 and value 2 is equal to value 4; - annotations whose Subtype value is Popup or Link",
	}

	Rule_19005_3_6_3_3_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.3.3",
		TestNumber:    2,
		Object:        "PDAnnot",
		Description:   "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key",
	}

	Rule_19005_3_6_3_3_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.3.3",
		TestNumber:    3,
		Object:        "PDAnnot",
		Description:   "If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary",
	}

	Rule_19005_3_6_3_3_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.3.3",
		TestNumber:    4,
		Object:        "PDAnnot",
		Description:   "If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream",
	}

	Rule_19005_3_6_4_1_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.4.1",
		TestNumber:    1,
		Object:        "PDWidgetAnnot",
		Description:   "A Widget annotation dictionary shall not contain the A or AA keys",
	}

	Rule_19005_3_6_4_1_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.4.1",
		TestNumber:    2,
		Object:        "PDFormField",
		Description:   "A Field dictionary shall not contain the A or AA keys",
	}

	Rule_19005_3_6_4_1_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.4.1",
		TestNumber:    3,
		Object:        "PDAcroForm",
		Description:   "The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false",
	}

	Rule_19005_3_6_4_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.4.2",
		TestNumber:    1,
		Object:        "PDAcroForm",
		Description:   "The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-3 file, if present, shall not contain the XFA key",
	}

	Rule_19005_3_6_4_2_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.4.2",
		TestNumber:    2,
		Object:        "CosDocument",
		Description:   "A document's Catalog shall not contain the NeedsRendering key",
	}

	Rule_19005_3_6_4_3_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.4.3",
		TestNumber:    1,
		Object:        "PDSignature",
		Description:   "When computing the digest for the file, it shall be computed over the entire file, including the signature dictionary but excluding the PDF Signature itself",
	}

	Rule_19005_3_6_4_3_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.4.3",
		TestNumber:    2,
		Object:        "PKCSDataObject",
		Description:   "The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, it shall include the signer's X.509 signing certificate",
	}

	Rule_19005_3_6_4_3_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.4.3",
		TestNumber:    3,
		Object:        "PKCSDataObject",
		Description:   "The PDF Signature (a DER-encoded PKCS#7 binary data object) shall be placed into the Contents entry of the signature dictionary. The PKCS#7 object shall conform to the PKCS#7 specification in RFC 2315. At minimum, there shall only be a single signer (e.g. a single \"SignerInfo\" structure) in the PDF Signature",
	}

	Rule_19005_3_6_5_1_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.5.1",
		TestNumber:    1,
		Object:        "PDAction",
		Description:   "The Launch, Sound, Movie, ResetForm, ImportData, Hide, SetOCGState, Rendition, Trans, GoTo3DView and JavaScript actions shall not be permitted. Additionally, the deprecated set-state and no-op actions shall not be permitted",
	}

	Rule_19005_3_6_5_1_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.5.1",
		TestNumber:    2,
		Object:        "PDNamedAction",
		Description:   "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted",
	}

	Rule_19005_3_6_5_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.5.2",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "The document's Catalog shall not include an AA entry for an additional-actions dictionary",
	}

	Rule_19005_3_6_5_2_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.5.2",
		TestNumber:    2,
		Object:        "PDPage",
		Description:   "The Page dictionary shall not include an AA entry for an additional-actions dictionary",
	}

	Rule_19005_3_6_6_2_1_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.1",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "The Catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-1:2008, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML",
	}

	Rule_19005_3_6_6_2_1_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.1",
		TestNumber:    2,
		Object:        "XMPPackage",
		Description:   "The bytes attribute shall not be used in the header of an XMP packet",
	}

	Rule_19005_3_6_6_2_1_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.1",
		TestNumber:    3,
		Object:        "XMPPackage",
		Description:   "The encoding attribute shall not be used in the header of an XMP packet",
	}

	Rule_19005_3_6_6_2_1_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.1",
		TestNumber:    4,
		Object:        "XMPPackage",
		Description:   "All metadata streams present in the PDF shall conform to the XMP Specification. All content of all XMP packets shall be well-formed, as defined by Extensible Markup Language (XML) 1.0 (Third Edition), 2.1, and the RDF/XML Syntax Specification (Revised)",
	}

	Rule_19005_3_6_6_2_1_t05 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.1",
		TestNumber:    5,
		Object:        "XMPPackage",
		Description:   "All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8",
	}

	Rule_19005_3_6_6_2_3_1_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.1",
		TestNumber:    1,
		Object:        "XMPProperty",
		Description:   "All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2",
	}

	Rule_19005_3_6_6_2_3_1_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.1",
		TestNumber:    2,
		Object:        "XMPProperty",
		Description:   "All properties specified in XMP form shall use either the predefined schemas defined in the XMP Specification, ISO 19005-1 or this part of ISO 19005, or any extension schemas that comply with 6.6.2.3.2",
	}

	Rule_19005_3_6_6_2_3_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.2",
		TestNumber:    1,
		Object:        "ExtensionSchemaObject",
		Description:   "Extension schemas shall be specified using the PDF/A extension schema container schema defined in 6.6.2.3.3. All fields described in each of the tables in 6.6.2.3.3 shall be present in any extension schema container schema",
	}

	Rule_19005_3_6_6_2_3_3_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    1,
		Object:        "ExtensionSchemasContainer",
		Description:   "The extension schema container schema uses the namespace URI \"http://www.aiim.org/pdfa/ns/extension/\". The required schema namespace prefix is pdfaExtension. pdfaExtension:schemas - Bag Schema - Description of extension schemas",
	}

	Rule_19005_3_6_6_2_3_3_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    2,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'schema' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_3_6_6_2_3_3_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    3,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'namespaceURI' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_3_6_6_2_3_3_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    4,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'prefix' of the PDF/A Schema value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_3_6_6_2_3_3_t05 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    5,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'property' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq Property and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_3_6_6_2_3_3_t06 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    6,
		Object:        "ExtensionSchemaDefinition",
		Description:   "Field 'valueType' of the PDF/A Schema value type in the PDF/A extension schema shall have type Seq ValueType and namespace prefix 'pdfaSchema'",
	}

	Rule_19005_3_6_6_2_3_3_t07 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    7,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'name' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'",
	}

	Rule_19005_3_6_6_2_3_3_t08 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    8,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'valueType' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'. Value of valueType property shall be defined",
	}

	Rule_19005_3_6_6_2_3_3_t09 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    9,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'category' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text, value external or internal and namespace prefix 'pdfaProperty'",
	}

	Rule_19005_3_6_6_2_3_3_t10 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    10,
		Object:        "ExtensionSchemaProperty",
		Description:   "Field 'description' of the PDF/A Property value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaProperty'",
	}

	Rule_19005_3_6_6_2_3_3_t11 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    11,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'type' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'",
	}

	Rule_19005_3_6_6_2_3_3_t12 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    12,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'namespaceURI' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type URI and namespace prefix 'pdfaType'",
	}

	Rule_19005_3_6_6_2_3_3_t13 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    13,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'prefix' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'",
	}

	Rule_19005_3_6_6_2_3_3_t14 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    14,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'description' of the PDF/A ValueType value type in the PDF/A extension schema shall be present and shall have type Text and namespace prefix 'pdfaType'",
	}

	Rule_19005_3_6_6_2_3_3_t15 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    15,
		Object:        "ExtensionSchemaValueType",
		Description:   "Field 'field' of the PDF/A ValueType value type in the PDF/A extension schema shall have type Seq Field and namespace prefix 'pdfaType'",
	}

	Rule_19005_3_6_6_2_3_3_t16 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    16,
		Object:        "ExtensionSchemaField",
		Description:   "Field 'name' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'",
	}

	Rule_19005_3_6_6_2_3_3_t17 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    17,
		Object:        "ExtensionSchemaField",
		Description:   "Field 'valueType' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'. Value of valueType property shall be defined",
	}

	Rule_19005_3_6_6_2_3_3_t18 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.2.3.3",
		TestNumber:    18,
		Object:        "ExtensionSchemaField",
		Description:   "Field 'description' of the PDF/A Field value type in the PDF/A extension schema shall have type Text and namespace prefix 'pdfaField'",
	}

	Rule_19005_3_6_6_4_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.4",
		TestNumber:    1,
		Object:        "MainXMPPackage",
		Description:   "The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema",
	}

	Rule_19005_3_6_6_4_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.4",
		TestNumber:    2,
		Object:        "PDFAIdentification",
		Description:   "The value of \"pdfaid:part\" shall be the part number of ISO 19005 to which the file conforms",
	}

	Rule_19005_3_6_6_4_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.4",
		TestNumber:    3,
		Object:        "PDFAIdentification",
		Description:   "A Level A conforming file shall specify the value of \"pdfaid:conformance\" as A. A Level B conforming file shall specify the value of \"pdfaid:conformance\" as B. A Level U conforming file shall specify the value of \"pdfaid:conformance\" as U",
	}

	Rule_19005_3_6_6_4_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.4",
		TestNumber:    4,
		Object:        "PDFAIdentification",
		Description:   "Property \"part\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_3_6_6_4_t05 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.4",
		TestNumber:    5,
		Object:        "PDFAIdentification",
		Description:   "Property \"conformance\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_3_6_6_4_t06 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.4",
		TestNumber:    6,
		Object:        "PDFAIdentification",
		Description:   "Property \"amd\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_3_6_6_4_t07 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.6.4",
		TestNumber:    7,
		Object:        "PDFAIdentification",
		Description:   "Property \"corr\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_3_6_7_2_2_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.7.2.2",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The document catalog dictionary shall include a MarkInfo dictionary containing an entry, Marked, whose value shall be true",
	}

	Rule_19005_3_6_7_3_3_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.7.3.3",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "The logical structure of the conforming file shall be described by a structure hierarchy rooted in the StructTreeRoot entry of the document's Catalog dictionary, as described in ISO 32000-1:2008, 14.7",
	}

	Rule_19005_3_6_7_3_4_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.7.3.4",
		TestNumber:    1,
		Object:        "SENonStandard",
		Description:   "All non-standard structure types shall be mapped to the nearest functionally equivalent standard type, as defined in ISO 32000-1:2008, 14.8.4, in the role map dictionary of the structure tree root",
	}

	Rule_19005_3_6_7_3_4_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.7.3.4",
		TestNumber:    2,
		Object:        "SENonStandard",
		Description:   "A circular mapping shall not exist",
	}

	Rule_19005_3_6_7_3_4_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.7.3.4",
		TestNumber:    3,
		Object:        "SENonStandard",
		Description:   "Standard tags shall not be remapped to a non-standard type",
	}

	Rule_19005_3_6_7_4_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.7.4",
		TestNumber:    1,
		Object:        "CosLang",
		Description:   "If the Lang entry is present in the document's Catalog dictionary or in a structure element dictionary or property list, its value shall be a language identifier as described in ISO 32000-1:2008, 14.9.2. A language identifier shall either be the empty text string, to indicate that the language is unknown, or a Language-Tag as defined in RFC 3066, Tags for the Identification of Languages",
	}

	Rule_19005_3_6_8_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.8",
		TestNumber:    1,
		Object:        "EmbeddedFile",
		Description:   "The MIME type of an embedded file, or a subset of a file, shall be specified using the Subtype key of the file specification dictionary. If the MIME type is not known, the \"application/octet-stream\" shall be used",
	}

	Rule_19005_3_6_8_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.8",
		TestNumber:    2,
		Object:        "CosFileSpecification",
		Description:   "The file specification dictionary for an embedded file shall contain the F and UF keys",
	}

	Rule_19005_3_6_8_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.8",
		TestNumber:    3,
		Object:        "CosFileSpecification",
		Description:   "The file specification dictionary shall contain key AFRelationship of type Name identifying the relationship between the embedded file and the content of the document",
	}

	Rule_19005_3_6_8_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.8",
		TestNumber:    4,
		Object:        "CosFileSpecification",
		Description:   "The additional information provided for associated files as well as the usage requirements for associated files indicate the relationship between the embedded file and the PDF document or the part of the PDF document with which it is associated",
	}

	Rule_19005_3_6_9_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.9",
		TestNumber:    1,
		Object:        "PDOCConfig",
		Description:   "Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key",
	}

	Rule_19005_3_6_9_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.9",
		TestNumber:    2,
		Object:        "PDOCConfig",
		Description:   "Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-3 file",
	}

	Rule_19005_3_6_9_t03 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.9",
		TestNumber:    3,
		Object:        "PDOCConfig",
		Description:   "If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file",
	}

	Rule_19005_3_6_9_t04 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.9",
		TestNumber:    4,
		Object:        "PDOCConfig",
		Description:   "The AS key shall not appear in any optional content configuration dictionary",
	}

	Rule_19005_3_6_10_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.10",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "There shall be no AlternatePresentations entry in the document's name dictionary",
	}

	Rule_19005_3_6_10_t02 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.10",
		TestNumber:    2,
		Object:        "PDPage",
		Description:   "There shall be no PresSteps entry in any Page dictionary",
	}

	Rule_19005_3_6_11_t01 = Rule{
		Specification: "ISO_19005_3",
		Clause:        "6.11",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The document catalog shall not contain the Requirements key",
	}

	// ISO 19005-4:2020 (PDF/A-4)

	Rule_19005_4_6_1_2_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.2",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The file header shall begin at byte zero and shall consist of \"%PDF-2.n\" followed by a single EOL marker, where 'n' is a single digit number between 0 (30h) and 9 (39h)",
	}

	Rule_19005_4_6_1_2_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.2",
		TestNumber:    2,
		Object:        "CosDocument",
		Description:   "The aforementioned EOL marker shall be immediately followed by a % (25h) character followed by at least four bytes, each of whose encoded byte values shall have a decimal value greater than 127",
	}

	Rule_19005_4_6_1_3_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.3",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "File identifiers shall be defined by the ID entry in a PDF file’s trailer dictionary",
	}

	Rule_19005_4_6_1_3_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.3",
		TestNumber:    2,
		Object:        "CosTrailer",
		Description:   "The Encrypt key shall not be present in the trailer dictionary",
	}

	Rule_19005_4_6_1_3_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.3",
		TestNumber:    3,
		Object:        "CosDocument",
		Description:   "No data shall follow the last end-of-file marker as described in ISO 32000-2:2020, 7.5.5",
	}

	Rule_19005_4_6_1_3_t04 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.3",
		TestNumber:    4,
		Object:        "CosDocument",
		Description:   "The Info key shall not be present in the trailer dictionary of PDF/A-4 conforming files unless there exists a PieceInfo entry in the document catalog dictionary",
	}

	Rule_19005_4_6_1_3_t05 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.3",
		TestNumber:    5,
		Object:        "CosInfo",
		Description:   "If a document information dictionary is present, it shall only contain a ModDate entry",
	}

	Rule_19005_4_6_1_4_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.4",
		TestNumber:    1,
		Object:        "CosXRef",
		Description:   "The xref keyword and the cross-reference subsection header shall be separated by a single EOL marker",
	}

	Rule_19005_4_6_1_5_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.5",
		TestNumber:    1,
		Object:        "CosString",
		Description:   "Hexadecimal strings shall contain an even number of non-white-space characters",
	}

	Rule_19005_4_6_1_5_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.5",
		TestNumber:    2,
		Object:        "CosString",
		Description:   "A hexadecimal string is written as a sequence of hexadecimal digits (0–9 and either A–F or a–f)",
	}

	Rule_19005_4_6_1_6_1_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.6.1",
		TestNumber:    1,
		Object:        "CosStream",
		Description:   "The value of the Length key specified in the stream dictionary shall match the number of bytes in the file following the LINE FEED (0Ah) character after the stream keyword and preceding the EOL marker before the endstream keyword",
	}

	Rule_19005_4_6_1_6_1_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.6.1",
		TestNumber:    2,
		Object:        "CosStream",
		Description:   "A stream dictionary shall not contain the F, FFilter, or FDecodeParms keys",
	}

	Rule_19005_4_6_1_6_1_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.6.1",
		TestNumber:    3,
		Object:        "PD3DStream",
		Description:   "The Subtype entry in a 3D stream dictionary (ISO 32000-2:2020, 13.6.3) shall have a value which is either U3D or PRC as described in Annex B",
	}

	Rule_19005_4_6_1_6_2_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.6.2",
		TestNumber:    1,
		Object:        "CosFilter",
		Description:   "All standard stream filters listed in ISO 32000-2:2020, 7.4, Table 6 may be used, with the exception of LZWDecode. Filters that are not listed in ISO 32000-2:2020, 7.4, Table 6 shall not be used. In addition, the Crypt filter shall not be used unless the value of the Name key in the decode parameters dictionary is Identity",
	}

	Rule_19005_4_6_1_7_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.7",
		TestNumber:    1,
		Object:        "CosUnicodeName",
		Description:   "Font names, names of colourants in Separation and DeviceN colour spaces, and structure type names, after expansion of character sequences escaped with a NUMBER SIGN (23h), if any, shall be valid UTF-8 character sequences",
	}

	Rule_19005_4_6_1_8_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.8",
		TestNumber:    1,
		Object:        "CosIndirect",
		Description:   "The object number and generation number shall be separated by a single white-space character. The generation number and obj keyword shall be separated by a single white-space character. The object number and endobj keyword shall each be preceded by an EOL marker. The obj and endobj keywords shall each be followed by an EOL marker",
	}

	Rule_19005_4_6_1_9_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.9",
		TestNumber:    1,
		Object:        "CosIIFilter",
		Description:   "The value of the F key in the Inline Image dictionary shall not be LZW, LZWDecode, Crypt, a value not listed in ISO 32000-2:2020, 8.9.7, Table 92, or an array containing any such value",
	}

	Rule_19005_4_6_1_11_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.11",
		TestNumber:    1,
		Object:        "PDPerms",
		Description:   "No keys other than UR3 and DocMDP shall be present in a permissions dictionary (ISO 32000-2:2020, 12.8.6, Table 263)",
	}

	Rule_19005_4_6_1_12_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.1.12",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "If the Version key is present in the document catalog dictionary, the first character in its value shall be a 2 (32h) and the second character of its value shall be a PERIOD (2Eh) (decimal point). The third character shall be a decimal digit. The number of characters of the value of the Version key shall be exactly 3",
	}

	Rule_19005_4_6_2_2_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.2",
		TestNumber:    1,
		Object:        "Op_Undefined",
		Description:   "Content streams shall not contain any operators not defined in ISO 32000-2:2020 even if such operators are bracketed by the BX/EX compatibility operators",
	}

	Rule_19005_4_6_2_2_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.2",
		TestNumber:    2,
		Object:        "PDContentStream",
		Description:   "A content stream that references other objects, such as images and fonts that are necessary to fully render or process the stream, shall have an explicitly associated Resources dictionary as described in ISO 32000-2:2020, 7.8.3. Such resource dictionaries shall define all named resources referenced by this content stream",
	}

	Rule_19005_4_6_2_2_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.2",
		TestNumber:    3,
		Object:        "PDContentStream",
		Description:   "A content stream's named resources shall be defined by a resource dictionary, which shall enumerate the named resources needed by the operators in the content stream and the names by which they can be referred to",
	}

	Rule_19005_4_6_2_3_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.3",
		TestNumber:    1,
		Object:        "ICCOutputProfile",
		Description:   "The profile stream that is the value of the DestOutputProfile key shall either be an output profile (Device Class = \"prtr\") or a monitor profile (Device Class = \"mntr\"). The profiles shall have a colour space of either \"GRAY\", \"RGB\", or \"CMYK\"",
	}

	Rule_19005_4_6_2_3_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.3",
		TestNumber:    2,
		Object:        "OutputIntents",
		Description:   "If any OutputIntents array contains more than one entry, as might be the case where a file is compliant with this part of ISO 19005 and at the same time with PDF/X or PDF/E, then all entries that contain a DestOutputProfile key shall have as the value of that key the same indirect object, which shall be a valid ICC profile stream",
	}

	Rule_19005_4_6_2_3_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.3",
		TestNumber:    3,
		Object:        "PDOutputIntent",
		Description:   "The DestOutputProfileRef key, as defined in ISO 32000-2:2020, 14.11.5, Table 401, shall not be present in any output intent dictionary",
	}

	Rule_19005_4_6_2_4_2_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.4.2",
		TestNumber:    1,
		Object:        "ICCInputProfile",
		Description:   "The profile that forms the stream of an ICCBased colour space shall conform to ISO 32000-2:2020, 8.6.5.5",
	}

	Rule_19005_4_6_2_4_2_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.4.2",
		TestNumber:    2,
		Object:        "PDICCBasedCMYK",
		Description:   "Overprint mode (as set by the OPM value in an ExtGState dictionary) shall not be one (1) when an ICCBased CMYK colour space is used for stroke and overprinting for stroke is set to true, or when ICCBased CMYK colour space is used for fill and overprinting for fill is set to true, or both",
	}

	Rule_19005_4_6_2_4_2_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.4.2",
		TestNumber:    3,
		Object:        "PDICCBasedCMYK",
		Description:   "An ICCBased colour space shall not be used where the profile is a CMYK destination profile and is identical to that in the current PDF/A OutputIntent or the current transparency blending colorspace",
	}

	Rule_19005_4_6_2_4_3_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.4.3",
		TestNumber:    2,
		Object:        "PDDeviceRGB",
		Description:   "DeviceRGB shall only be used if a device independent DefaultRGB colour space has been set when the DeviceRGB colour space is used or if the current transparency blending space, when the DeviceRGB colour space is used, is a device independent RGB-based colour space or the current PDF/A OutputIntent, when the DeviceRGB colour space is used, contains an 'RGB ' destination profile",
	}

	Rule_19005_4_6_2_4_3_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.4.3",
		TestNumber:    3,
		Object:        "PDDeviceCMYK",
		Description:   "DeviceCMYK shall only be used if a device independent DefaultCMYK colour space has been set when the DeviceCMYK colour space is used or if the current transparency blending space, when the DeviceCMYK colour space is used, is a device independent CMYK-based colour space or the current PDF/A OutputIntent, when the DeviceCMYK colour space is used, contains a 'CMYK' destination profile",
	}

	Rule_19005_4_6_2_4_3_t04 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.4.3",
		TestNumber:    4,
		Object:        "PDDeviceGray",
		Description:   "DeviceGray shall only be used if a device independent DefaultGray colour space has been set when the DeviceGray colour space is used, or if a PDF/A OutputIntent is in effect",
	}

	Rule_19005_4_6_2_4_4_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.4.4",
		TestNumber:    1,
		Object:        "PDDeviceN",
		Description:   "For any spot colour used in a DeviceN or NChannel colour space, an entry in the Colorants dictionary shall be present",
	}

	Rule_19005_4_6_2_4_4_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.4.4",
		TestNumber:    2,
		Object:        "PDSeparation",
		Description:   "All Separation arrays within a single conforming PDF/A-4 file (including those in Colorants dictionaries) that have the same name shall have the same tintTransform and alternateSpace. In evaluating equivalence, the PDF objects shall be compared, rather than the computational result of the use of those PDF objects. Compression and whether or not an object is direct or indirect shall be ignored",
	}

	Rule_19005_4_6_2_5_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.5",
		TestNumber:    1,
		Object:        "PDExtGState",
		Description:   "A graphics state parameter dictionary (ISO 32000-2:2020, 8.4.5) shall not contain the TR key",
	}

	Rule_19005_4_6_2_5_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.5",
		TestNumber:    2,
		Object:        "PDExtGState",
		Description:   "A graphics state parameter dictionary shall not contain the TR2 key with a value other than Default",
	}

	Rule_19005_4_6_2_5_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.5",
		TestNumber:    3,
		Object:        "PDExtGState",
		Description:   "A graphics state parameter dictionary shall not contain the HTO key",
	}

	Rule_19005_4_6_2_5_t04 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.5",
		TestNumber:    4,
		Object:        "PDHalftone",
		Description:   "All halftones in a conforming PDF/A-4 file shall have the value 1 or 5 for the HalftoneType key",
	}

	Rule_19005_4_6_2_5_t05 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.5",
		TestNumber:    5,
		Object:        "PDHalftone",
		Description:   "Halftones in a conforming PDF/A-4 file shall not contain a HalftoneName key",
	}

	Rule_19005_4_6_2_5_t06 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.5",
		TestNumber:    6,
		Object:        "PDHalftone",
		Description:   "The TransferFunction key in a halftone dictionary shall be used only as required by ISO 32000-2:2020",
	}

	Rule_19005_4_6_2_7_1_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.7.1",
		TestNumber:    1,
		Object:        "PDXImage",
		Description:   "An Image dictionary shall not contain the Alternates key",
	}

	Rule_19005_4_6_2_7_1_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.7.1",
		TestNumber:    2,
		Object:        "PDXImage",
		Description:   "An Image dictionary shall not contain the OPI key",
	}

	Rule_19005_4_6_2_7_1_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.7.1",
		TestNumber:    3,
		Object:        "PDXImage",
		Description:   "If an Image dictionary contains the Interpolate key, its value shall be false. For an inline image, the I key, if present, shall have a value of false",
	}

	Rule_19005_4_6_2_7_1_t04 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.7.1",
		TestNumber:    4,
		Object:        "PDXImage",
		Description:   "If an Image dictionary contains the BitsPerComponent key, its value shall be 1, 2, 4, 8 or 16",
	}

	Rule_19005_4_6_2_7_1_t05 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.7.1",
		TestNumber:    5,
		Object:        "PDMaskImage",
		Description:   "If an image mask dictionary contains the BitsPerComponent key, its value shall be 1",
	}

	Rule_19005_4_6_2_7_3_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.7.3",
		TestNumber:    1,
		Object:        "JPEG2000",
		Description:   "The number of colour channels in the JPEG2000 data shall be 1, 3 or 4",
	}

	Rule_19005_4_6_2_7_3_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.7.3",
		TestNumber:    2,
		Object:        "JPEG2000",
		Description:   "If the number of colour space specifications in the JPEG2000 data is greater than 1, there shall be exactly one colour space specification that has the value 0x01 in the APPROX field",
	}

	Rule_19005_4_6_2_7_3_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.7.3",
		TestNumber:    3,
		Object:        "JPEG2000",
		Description:   "The value of the METH entry in its 'colr' box shall be 0x01, 0x02 or 0x03. A conforming processor shall use only that colour space and shall ignore all other colour space specifications",
	}

	Rule_19005_4_6_2_7_3_t04 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.7.3",
		TestNumber:    4,
		Object:        "JPEG2000",
		Description:   "JPEG2000 enumerated colour space 19 (CIEJab) shall not be used",
	}

	Rule_19005_4_6_2_7_3_t05 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.7.3",
		TestNumber:    5,
		Object:        "JPEG2000",
		Description:   "The bit-depth of the JPEG2000 data shall have a value in the range 1 to 38. All colour channels in the JPEG2000 data shall have the same bit-depth",
	}

	Rule_19005_4_6_2_8_1_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.8.1",
		TestNumber:    1,
		Object:        "PDXForm",
		Description:   "A form XObject dictionary shall not contain an OPI key",
	}

	Rule_19005_4_6_2_8_2_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.8.2",
		TestNumber:    1,
		Object:        "PDXForm",
		Description:   "A conforming file shall not contain any reference XObjects",
	}

	Rule_19005_4_6_2_9_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.9",
		TestNumber:    1,
		Object:        "CosBM",
		Description:   "Only blend modes that are specified in ISO 32000-2:2020 shall be used for the value of the BM key in a graphic state dictionary or an annotation dictionary",
	}

	Rule_19005_4_6_2_9_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.9",
		TestNumber:    2,
		Object:        "PDPage",
		Description:   "If the document does not contain a PDF/A output intent, then all pages that contain transparency shall either have a page-level PDF/A output intent or the page dictionary shall include the Group key, and the attribute dictionary that forms the value of that Group key shall include a CS entry whose value shall be used as the default blending colour space",
	}

	Rule_19005_4_6_2_10_2_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.2",
		TestNumber:    1,
		Object:        "PDFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Type - name - (Required) The type of PDF object that this dictionary describes; must be Font for a font dictionary",
	}

	Rule_19005_4_6_2_10_2_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.2",
		TestNumber:    2,
		Object:        "PDFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Subtype - name - (Required) The type of font; must be \"Type1\" for Type 1 fonts, \"MMType1\" for multiple master fonts, \"TrueType\" for TrueType fonts \"Type3\" for Type 3 fonts, \"Type0\" for Type 0 fonts and \"CIDFontType0\" or \"CIDFontType2\" for CID fonts",
	}

	Rule_19005_4_6_2_10_2_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.2",
		TestNumber:    3,
		Object:        "PDFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. BaseFont - name - (Required) The PostScript name of the font",
	}

	Rule_19005_4_6_2_10_2_t04 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.2",
		TestNumber:    4,
		Object:        "PDSimpleFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. FirstChar - integer - (Required except for the standard 14 fonts) The first character code defined in the font's Widths array",
	}

	Rule_19005_4_6_2_10_2_t05 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.2",
		TestNumber:    5,
		Object:        "PDSimpleFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. LastChar - integer - (Required except for the standard 14 fonts) The last character code defined in the font's Widths array",
	}

	Rule_19005_4_6_2_10_2_t06 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.2",
		TestNumber:    6,
		Object:        "PDSimpleFont",
		Description:   "All fonts and font programs used in a conforming file, regardless of rendering mode usage, shall conform to the provisions in ISO 32000-2:2020, 9.6 and 9.7, as well as to the font specifications referenced by these provisions. Widths - array - (Required except for the standard 14 fonts; indirect reference preferred) An array of (LastChar − FirstChar + 1) widths",
	}

	Rule_19005_4_6_2_10_2_t07 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.2",
		TestNumber:    7,
		Object:        "PDFont",
		Description:   "All fonts used in a conforming file shall conform to the font specifications defined in PDF Reference 5.5. The subtype is the value of the Subtype key, if present, in the font file stream dictionary. The only valid values of this key in PDF 2.0 are Type1C - Type 1–equivalent font program represented in the Compact Font Format (CFF), CIDFontType0C - Type 0 CIDFont program represented in the Compact Font Format (CFF) and OpenType - OpenType® font program, as described in the ISO/IEC 14496-22",
	}

	Rule_19005_4_6_2_10_3_1_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.3.1",
		TestNumber:    1,
		Object:        "PDType0Font",
		Description:   "For any given composite (Type 0) font within a conforming file, the CIDSystemInfo entry in its CIDFont dictionary and its Encoding dictionary shall have the following relationship: - If the Encoding key in the Type 0 font dictionary has a value of Identity-H or Identity-V, then any values for the Registry, Ordering, and Supplement keys may be used in the CIDSystemInfo dictionary of the CIDFont. - Otherwise the corresponding values of the Registry and Ordering keys in both CIDSystemInfo dictionaries shall be identical, and the value of the Supplement key in the CIDSystemInfo dictionary of the CIDFont shall be less than or equal to the value of the Supplement key in the CIDSystemInfo dictionary of the CMap",
	}

	Rule_19005_4_6_2_10_3_2_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.3.2",
		TestNumber:    1,
		Object:        "PDCIDFont",
		Description:   "ISO 32000-2:2020, 9.7.4 Table 115 requires that all embedded Type 2 CIDFonts, the CIDFont dictionary shall contain a CIDToGIDMap entry that shall be a stream mapping from CIDs to glyph indices or the name Identity, as described in ISO 32000-2:2020, 9.7.4 Table 115",
	}

	Rule_19005_4_6_2_10_3_3_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.3.3",
		TestNumber:    1,
		Object:        "PDCMap",
		Description:   "All CMaps used within a conforming PDF/A-4 file, except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116, shall be embedded in that file as described in ISO 32000-2:2020, 9.7.5",
	}

	Rule_19005_4_6_2_10_3_3_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.3.3",
		TestNumber:    2,
		Object:        "CMapFile",
		Description:   "For those CMaps that are embedded, the integer value of the WMode entry in the CMap dictionary shall be identical to the WMode value in the embedded CMap stream",
	}

	Rule_19005_4_6_2_10_3_3_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.3.3",
		TestNumber:    3,
		Object:        "PDReferencedCMap",
		Description:   "A CMap shall not reference any other CMap except those listed in ISO 32000-2:2020, 9.7.5.2 Table 116",
	}

	Rule_19005_4_6_2_10_4_1_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.4.1",
		TestNumber:    1,
		Object:        "PDFont",
		Description:   "The font programs for all fonts used for rendering within a conforming file shall be embedded within that file, as defined in ISO 32000-2:2020, 9.9",
	}

	Rule_19005_4_6_2_10_4_1_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.4.1",
		TestNumber:    2,
		Object:        "Glyph",
		Description:   "Embedded fonts shall define all glyphs referenced for rendering within the conforming file. A font referenced for use solely in rendering mode 3 is therefore not rendered and is thus exempt from the embedding requirement. In all cases for TrueType fonts that are to be rendered, character codes shall be able to be mapped to glyphs according to ISO 32000-2:2020, 9.6.5 without the use of a non-standard mapping chosen by the conforming processor",
	}

	Rule_19005_4_6_2_10_5_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.5",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "For every font embedded in a conforming file, the glyph width information in the font dictionary and in the embedded font program shall be consistent for every glyph referenced for rendering. Glyphs that are referenced only with rendering mode 3 are exempt from this requirement",
	}

	Rule_19005_4_6_2_10_6_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.6",
		TestNumber:    1,
		Object:        "TrueTypeFontProgram",
		Description:   "For all non-symbolic TrueType fonts used for rendering, the embedded TrueType font program shall contain at least Microsoft Unicode (3,1 – Platform ID=3, Encoding ID=1), or Macintosh Roman (1,0 – Platform ID=1, Encoding ID=0) 'cmap' subtable that all necessary glyph lookups are able to be carried out",
	}

	Rule_19005_4_6_2_10_6_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.6",
		TestNumber:    2,
		Object:        "PDTrueTypeFont",
		Description:   "All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding as the value for the Encoding key in the Font dictionary or as the value for the BaseEncoding key in the dictionary which is the value of the Encoding key in the Font dictionary. In addition, no non-symbolic TrueType font shall define a Differences array unless all of the glyph names in the Differences array are listed in the Adobe Glyph List and, if the font program is embedded, it shall contain at least the Microsoft Unicode (3,1 – Platform ID = 3, Encoding ID = 1) encoding in the 'cmap' subtable",
	}

	Rule_19005_4_6_2_10_6_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.6",
		TestNumber:    3,
		Object:        "PDTrueTypeFont",
		Description:   "Symbolic TrueType fonts shall not contain an Encoding entry in the font dictionary",
	}

	Rule_19005_4_6_2_10_6_t04 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.6",
		TestNumber:    4,
		Object:        "TrueTypeFontProgram",
		Description:   "The 'cmap' subtable in the embedded font program for a symbolic TrueType font shall contain either the Microsoft Symbol (3,0 – Platform ID=3, Encoding ID=0) or the Mac Roman (1,0 – Platform ID=1, Encoding ID=0) encoding",
	}

	Rule_19005_4_6_2_10_7_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.7",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "If a ToUnicode CMap is present, the Unicode values specified there shall all be greater than zero (0), but not equal to either U+FEFF or U+FFFE",
	}

	Rule_19005_4_6_2_10_8_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.8",
		TestNumber:    1,
		Object:        "CosActualText",
		Description:   "The ActualText entry shall not contain any PUA values",
	}

	Rule_19005_4_6_2_10_9_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.2.10.9",
		TestNumber:    1,
		Object:        "Glyph",
		Description:   "A PDF/A-4 compliant document shall not contain a reference to the .notdef glyph from any of the text showing operators, regardless of text rendering mode, in any content stream",
	}

	Rule_19005_4_6_3_1_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.3.1",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Annotation types not defined in ISO 32000-2:2020, 12.5.6.1, Table 171 shall not be permitted. Additionally, the Sound, Screen, Movie, 3D, RichMedia and FileAttachment types shall not be permitted",
	}

	Rule_19005_4_6_3_2_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.3.2",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Except for annotation dictionaries whose Subtype value is Popup, all annotation dictionaries shall contain the F key",
	}

	Rule_19005_4_6_3_2_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.3.2",
		TestNumber:    2,
		Object:        "PDAnnot",
		Description:   "If present, the F key's Print flag bit shall be set to 1 and its Hidden, Invisible, ToggleNoView, and NoView flag bits shall be set to 0",
	}

	Rule_19005_4_6_3_3_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.3.3",
		TestNumber:    1,
		Object:        "PDAnnot",
		Description:   "Every annotation (including those whose Subtype value is Widget, as used for form fields), except for the two cases listed below, shall have at least one appearance dictionary: Annotations where the value of the Rect key consists of an array where the value at index 1 is equal to the value at index 3 and the value at index 2 is equal to the value at index 4; - annotations whose Subtype value is Popup, Link or Projection",
	}

	Rule_19005_4_6_3_3_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.3.3",
		TestNumber:    2,
		Object:        "PDAnnot",
		Description:   "For all annotation dictionaries containing an AP key, the appearance dictionary that it defines as its value shall contain only the N key",
	}

	Rule_19005_4_6_3_3_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.3.3",
		TestNumber:    3,
		Object:        "PDAnnot",
		Description:   "If an annotation dictionary's Subtype key has a value of Widget and its FT key has a value of Btn, the value of the N key shall be an appearance subdictionary",
	}

	Rule_19005_4_6_3_3_t04 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.3.3",
		TestNumber:    4,
		Object:        "PDAnnot",
		Description:   "If an annotation dictionary's Subtype key has value other than Widget, or if FT key associated with Widget annotation has value other than Btn, the value of the N key shall be an appearance stream",
	}

	Rule_19005_4_6_4_1_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.4.1",
		TestNumber:    1,
		Object:        "PDWidgetAnnot",
		Description:   "A Widget annotation dictionary shall not contain the A key",
	}

	Rule_19005_4_6_4_1_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.4.1",
		TestNumber:    2,
		Object:        "PDAcroForm",
		Description:   "The NeedAppearances flag of the interactive form dictionary shall either not be present or shall be false",
	}

	Rule_19005_4_6_4_2_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.4.2",
		TestNumber:    1,
		Object:        "PDAcroForm",
		Description:   "The document's interactive form dictionary that forms the value of the AcroForm key in the document's Catalog of a PDF/A-4 file, if present, shall not contain the XFA key",
	}

	Rule_19005_4_6_4_2_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.4.2",
		TestNumber:    2,
		Object:        "CosDocument",
		Description:   "A document's Catalog shall not contain the NeedsRendering key",
	}

	Rule_19005_4_6_6_1_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.6.1",
		TestNumber:    1,
		Object:        "PDAction",
		Description:   "The Launch, Sound, Movie, ResetForm, ImportData, Hide, Rendition, Trans, SetOCGState and GoTo3DView actions shall not be permitted. Additionally, the obsoleted set-state and no-op actions shall not be permitted",
	}

	Rule_19005_4_6_6_1_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.6.1",
		TestNumber:    2,
		Object:        "PDNamedAction",
		Description:   "Named actions other than NextPage, PrevPage, FirstPage, and LastPage shall not be permitted",
	}

	Rule_19005_4_6_6_3_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.6.3",
		TestNumber:    1,
		Object:        "PDAdditionalActions",
		Description:   "If a document catalog dictionary or page dictionary or an annotation dictionary (other than a Widget annotation dictionary) include an AA entry, its value (which is an additional-actions dictionary) shall only contain keys from the following list: E, X, D, U, Fo, and Bl",
	}

	Rule_19005_4_6_7_2_1_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.2.1",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "The document catalog dictionary of a conforming file shall contain the Metadata key whose value is a metadata stream as defined in ISO 32000-2:2020, 14.3.2. The metadata stream dictionary shall contain entry Type with value /Metadata and entry Subtype with value /XML",
	}

	Rule_19005_4_6_7_2_1_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.2.1",
		TestNumber:    2,
		Object:        "XMPPackage",
		Description:   "The bytes attribute shall not be used in the header of an XMP packet",
	}

	Rule_19005_4_6_7_2_1_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.2.1",
		TestNumber:    3,
		Object:        "XMPPackage",
		Description:   "The encoding attribute shall not be used in the header of an XMP packet",
	}

	Rule_19005_4_6_7_2_1_t04 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.2.1",
		TestNumber:    4,
		Object:        "XMPPackage",
		Description:   "All content of all XMP packets located in any metadata stream present in the PDF shall be well-formed as defined by XMP (ISO 16684-1)",
	}

	Rule_19005_4_6_7_2_1_t05 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.2.1",
		TestNumber:    5,
		Object:        "XMPPackage",
		Description:   "All metadata streams present in the PDF shall conform to the XMP Specification. The XMP package must be encoded as UTF-8",
	}

	Rule_19005_4_6_7_3_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.3",
		TestNumber:    1,
		Object:        "MainXMPPackage",
		Description:   "The PDF/A version and conformance level of a file shall be specified using the PDF/A Identification extension schema. The PDF/A Identification schema defined in Table 2 uses the namespace URI \"http://www.aiim.org/pdfa/ns/id/\"",
	}

	Rule_19005_4_6_7_3_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.3",
		TestNumber:    2,
		Object:        "PDFAIdentification",
		Description:   "The value of \"pdfaid:part\" shall be the part number of ISO 19005 to which the file conforms",
	}

	Rule_19005_4_6_7_3_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.3",
		TestNumber:    3,
		Object:        "PDFAIdentification",
		Description:   "A file that does not conform to either PDF/A-4e or PDF/A-4f shall not provide any \"pdfaid:conformance\"",
	}

	Rule_19005_4_6_7_3_t04 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.3",
		TestNumber:    4,
		Object:        "PDFAIdentification",
		Description:   "Property \"part\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

	Rule_19005_4_6_7_3_t05 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.3",
		TestNumber:    5,
		Object:        "PDFAIdentification",
		Description:   "The value of \"pdfaid:rev\" shall be \"2020\"",
	}

	Rule_19005_4_6_7_3_t06 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.7.3",
		TestNumber:    6,
		Object:        "PDFAIdentification",
		Description:   "Property \"rev\" of the PDF/A Identification Schema shall have namespace prefix \"pdfaid\"",
	}

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

	Rule_19005_4_6_9_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.9",
		TestNumber:    1,
		Object:        "EmbeddedFile",
		Description:   "The embedded file stream dictionary shall include a valid MIME type value for the Subtype key. If the MIME type is not known, the value \"application/octet-stream\" shall be used",
	}

	Rule_19005_4_6_9_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.9",
		TestNumber:    2,
		Object:        "CosFileSpecification",
		Description:   "The file specification dictionary for an embedded file shall contain the F and UF keys",
	}

	Rule_19005_4_6_9_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.9",
		TestNumber:    3,
		Object:        "EmbeddedFile",
		Description:   "All of the embedded files shall be compliant with ISO 19005-1, 19005-2 or 19005-4",
	}

	Rule_19005_4_6_9_t04 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.9",
		TestNumber:    4,
		Object:        "CosFileSpecification",
		Description:   "Each embedded file’s file specification dictionary shall contain an AFRelationship key (ISO 32000-2, 7.11.3) that describes how this embedded file relates to the content of the PDF",
	}

	Rule_19005_4_6_9_t05 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.9",
		TestNumber:    5,
		Object:        "CosDocument",
		Description:   "A PDF/A-4f conforming file shall contain an EmbeddedFiles key in the name dictionary of the document catalog dictionary",
	}

	Rule_19005_4_6_10_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.10",
		TestNumber:    1,
		Object:        "PDOCConfig",
		Description:   "Each optional content configuration dictionary that forms the value of the D key, or that is an element in the array that forms the value of the Configs key in the OCProperties dictionary, shall contain the Name key",
	}

	Rule_19005_4_6_10_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.10",
		TestNumber:    2,
		Object:        "PDOCConfig",
		Description:   "Each optional content configuration dictionary shall contain the Name key, whose value shall be unique amongst all optional content configuration dictionaries within the PDF/A-4 file",
	}

	Rule_19005_4_6_10_t03 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.10",
		TestNumber:    3,
		Object:        "PDOCConfig",
		Description:   "If an optional content configuration dictionary contains the Order key, the array which is the value of this Order key shall contain references to all OCGs in the conforming file",
	}

	Rule_19005_4_6_11_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.11",
		TestNumber:    1,
		Object:        "PDDocument",
		Description:   "There shall be no AlternatePresentations entry in the document's name dictionary",
	}

	Rule_19005_4_6_11_t02 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.11",
		TestNumber:    2,
		Object:        "PDPage",
		Description:   "There shall be no PresSteps entry in any Page dictionary",
	}

	Rule_19005_4_6_12_t01 = Rule{
		Specification: "ISO_19005_4",
		Clause:        "6.12",
		TestNumber:    1,
		Object:        "CosDocument",
		Description:   "The document catalog shall not contain the Requirements key",
	}
)

// --- Profile rule sets ---
//
// Each function returns the complete set of rules for a profile.

// PDFA1aRules returns the PDF/A-1a rule set (ISO 19005-1:2005, Level A, 135 rules).
func PDFA1aRules() []Rule {
	return []Rule{
		Rule_19005_1_6_1_2_t01,
		Rule_19005_1_6_1_2_t02,
		Rule_19005_1_6_1_3_t01,
		Rule_19005_1_6_1_3_t02,
		Rule_19005_1_6_1_3_t03,
		Rule_19005_1_6_1_3_t04,
		Rule_19005_1_6_1_4_t01,
		Rule_19005_1_6_1_4_t02,
		Rule_19005_1_6_1_4_t03,
		Rule_19005_1_6_1_6_t01,
		Rule_19005_1_6_1_6_t02,
		Rule_19005_1_6_1_7_t01,
		Rule_19005_1_6_1_7_t02,
		Rule_19005_1_6_1_7_t03,
		Rule_19005_1_6_1_8_t01,
		Rule_19005_1_6_1_10_t01,
		Rule_19005_1_6_1_10_t02,
		Rule_19005_1_6_1_11_t01,
		Rule_19005_1_6_1_11_t02,
		Rule_19005_1_6_1_12_t01,
		Rule_19005_1_6_1_12_t02,
		Rule_19005_1_6_1_12_t03,
		Rule_19005_1_6_1_12_t04,
		Rule_19005_1_6_1_12_t05,
		Rule_19005_1_6_1_12_t06,
		Rule_19005_1_6_1_12_t07,
		Rule_19005_1_6_1_12_t08,
		Rule_19005_1_6_1_12_t09,
		Rule_19005_1_6_1_12_t10,
		Rule_19005_1_6_1_13_t01,
		Rule_19005_1_6_2_2_t01,
		Rule_19005_1_6_2_2_t02,
		Rule_19005_1_6_2_3_2_t01,
		Rule_19005_1_6_2_3_2_t02,
		Rule_19005_1_6_2_3_3_t01,
		Rule_19005_1_6_2_3_3_t02,
		Rule_19005_1_6_2_3_3_t03,
		Rule_19005_1_6_2_4_t01,
		Rule_19005_1_6_2_4_t02,
		Rule_19005_1_6_2_4_t03,
		Rule_19005_1_6_2_4_t04,
		Rule_19005_1_6_2_4_t05,
		Rule_19005_1_6_2_5_t01,
		Rule_19005_1_6_2_6_t01,
		Rule_19005_1_6_2_7_t01,
		Rule_19005_1_6_2_8_t01,
		Rule_19005_1_6_2_8_t02,
		Rule_19005_1_6_2_9_t01,
		Rule_19005_1_6_2_10_t01,
		Rule_19005_1_6_3_2_t01,
		Rule_19005_1_6_3_2_t02,
		Rule_19005_1_6_3_2_t03,
		Rule_19005_1_6_3_2_t04,
		Rule_19005_1_6_3_2_t05,
		Rule_19005_1_6_3_2_t06,
		Rule_19005_1_6_3_2_t07,
		Rule_19005_1_6_3_3_1_t01,
		Rule_19005_1_6_3_3_2_t01,
		Rule_19005_1_6_3_3_3_t01,
		Rule_19005_1_6_3_3_3_t02,
		Rule_19005_1_6_3_4_t01,
		Rule_19005_1_6_3_5_t01,
		Rule_19005_1_6_3_5_t02,
		Rule_19005_1_6_3_5_t03,
		Rule_19005_1_6_3_6_t01,
		Rule_19005_1_6_3_7_t01,
		Rule_19005_1_6_3_7_t02,
		Rule_19005_1_6_3_7_t03,
		Rule_19005_1_6_3_8_t01,
		Rule_19005_1_6_4_t01,
		Rule_19005_1_6_4_t02,
		Rule_19005_1_6_4_t03,
		Rule_19005_1_6_4_t04,
		Rule_19005_1_6_4_t05,
		Rule_19005_1_6_4_t06,
		Rule_19005_1_6_5_2_t01,
		Rule_19005_1_6_5_3_t01,
		Rule_19005_1_6_5_3_t02,
		Rule_19005_1_6_5_3_t03,
		Rule_19005_1_6_5_3_t04,
		Rule_19005_1_6_5_3_t05,
		Rule_19005_1_6_5_3_t06,
		Rule_19005_1_6_6_1_t01,
		Rule_19005_1_6_6_1_t02,
		Rule_19005_1_6_6_1_t03,
		Rule_19005_1_6_6_2_t01,
		Rule_19005_1_6_6_2_t02,
		Rule_19005_1_6_6_2_t03,
		Rule_19005_1_6_7_2_t01,
		Rule_19005_1_6_7_2_t02,
		Rule_19005_1_6_7_3_t01,
		Rule_19005_1_6_7_3_t02,
		Rule_19005_1_6_7_3_t03,
		Rule_19005_1_6_7_3_t04,
		Rule_19005_1_6_7_3_t05,
		Rule_19005_1_6_7_3_t06,
		Rule_19005_1_6_7_3_t07,
		Rule_19005_1_6_7_3_t08,
		Rule_19005_1_6_7_5_t01,
		Rule_19005_1_6_7_5_t02,
		Rule_19005_1_6_7_8_t01,
		Rule_19005_1_6_7_8_t02,
		Rule_19005_1_6_7_8_t03,
		Rule_19005_1_6_7_8_t04,
		Rule_19005_1_6_7_8_t05,
		Rule_19005_1_6_7_8_t06,
		Rule_19005_1_6_7_8_t07,
		Rule_19005_1_6_7_8_t08,
		Rule_19005_1_6_7_8_t09,
		Rule_19005_1_6_7_8_t10,
		Rule_19005_1_6_7_8_t11,
		Rule_19005_1_6_7_8_t12,
		Rule_19005_1_6_7_8_t13,
		Rule_19005_1_6_7_8_t14,
		Rule_19005_1_6_7_8_t15,
		Rule_19005_1_6_7_8_t16,
		Rule_19005_1_6_7_8_t17,
		Rule_19005_1_6_7_8_t18,
		Rule_19005_1_6_7_8_t19,
		Rule_19005_1_6_7_9_t01,
		Rule_19005_1_6_7_9_t02,
		Rule_19005_1_6_7_9_t03,
		Rule_19005_1_6_7_11_t01,
		Rule_19005_1_6_7_11_t02,
		Rule_19005_1_6_7_11_t03,
		Rule_19005_1_6_7_11_t04,
		Rule_19005_1_6_7_11_t05,
		Rule_19005_1_6_7_11_t06,
		Rule_19005_1_6_8_2_2_t01,
		Rule_19005_1_6_8_3_3_t01,
		Rule_19005_1_6_8_3_4_t01,
		Rule_19005_1_6_8_3_4_t02,
		Rule_19005_1_6_8_4_t01,
		Rule_19005_1_6_9_t01,
		Rule_19005_1_6_9_t02,
	}
}

// PDFA1bRules returns the PDF/A-1b rule set (ISO 19005-1:2005, Level B, 129 rules).
func PDFA1bRules() []Rule {
	return []Rule{
		Rule_19005_1_6_1_2_t01,
		Rule_19005_1_6_1_2_t02,
		Rule_19005_1_6_1_3_t01,
		Rule_19005_1_6_1_3_t02,
		Rule_19005_1_6_1_3_t03,
		Rule_19005_1_6_1_3_t04,
		Rule_19005_1_6_1_4_t01,
		Rule_19005_1_6_1_4_t02,
		Rule_19005_1_6_1_4_t03,
		Rule_19005_1_6_1_6_t01,
		Rule_19005_1_6_1_6_t02,
		Rule_19005_1_6_1_7_t01,
		Rule_19005_1_6_1_7_t02,
		Rule_19005_1_6_1_7_t03,
		Rule_19005_1_6_1_8_t01,
		Rule_19005_1_6_1_10_t01,
		Rule_19005_1_6_1_10_t02,
		Rule_19005_1_6_1_11_t01,
		Rule_19005_1_6_1_11_t02,
		Rule_19005_1_6_1_12_t01,
		Rule_19005_1_6_1_12_t02,
		Rule_19005_1_6_1_12_t03,
		Rule_19005_1_6_1_12_t04,
		Rule_19005_1_6_1_12_t05,
		Rule_19005_1_6_1_12_t06,
		Rule_19005_1_6_1_12_t07,
		Rule_19005_1_6_1_12_t08,
		Rule_19005_1_6_1_12_t09,
		Rule_19005_1_6_1_12_t10,
		Rule_19005_1_6_1_13_t01,
		Rule_19005_1_6_2_2_t01,
		Rule_19005_1_6_2_2_t02,
		Rule_19005_1_6_2_3_2_t01,
		Rule_19005_1_6_2_3_2_t02,
		Rule_19005_1_6_2_3_3_t01,
		Rule_19005_1_6_2_3_3_t02,
		Rule_19005_1_6_2_3_3_t03,
		Rule_19005_1_6_2_4_t01,
		Rule_19005_1_6_2_4_t02,
		Rule_19005_1_6_2_4_t03,
		Rule_19005_1_6_2_4_t04,
		Rule_19005_1_6_2_4_t05,
		Rule_19005_1_6_2_5_t01,
		Rule_19005_1_6_2_6_t01,
		Rule_19005_1_6_2_7_t01,
		Rule_19005_1_6_2_8_t01,
		Rule_19005_1_6_2_8_t02,
		Rule_19005_1_6_2_9_t01,
		Rule_19005_1_6_2_10_t01,
		Rule_19005_1_6_3_2_t01,
		Rule_19005_1_6_3_2_t02,
		Rule_19005_1_6_3_2_t03,
		Rule_19005_1_6_3_2_t04,
		Rule_19005_1_6_3_2_t05,
		Rule_19005_1_6_3_2_t06,
		Rule_19005_1_6_3_2_t07,
		Rule_19005_1_6_3_3_1_t01,
		Rule_19005_1_6_3_3_2_t01,
		Rule_19005_1_6_3_3_3_t01,
		Rule_19005_1_6_3_3_3_t02,
		Rule_19005_1_6_3_4_t01,
		Rule_19005_1_6_3_5_t01,
		Rule_19005_1_6_3_5_t02,
		Rule_19005_1_6_3_5_t03,
		Rule_19005_1_6_3_6_t01,
		Rule_19005_1_6_3_7_t01,
		Rule_19005_1_6_3_7_t02,
		Rule_19005_1_6_3_7_t03,
		Rule_19005_1_6_4_t01,
		Rule_19005_1_6_4_t02,
		Rule_19005_1_6_4_t03,
		Rule_19005_1_6_4_t04,
		Rule_19005_1_6_4_t05,
		Rule_19005_1_6_4_t06,
		Rule_19005_1_6_5_2_t01,
		Rule_19005_1_6_5_3_t01,
		Rule_19005_1_6_5_3_t02,
		Rule_19005_1_6_5_3_t03,
		Rule_19005_1_6_5_3_t04,
		Rule_19005_1_6_5_3_t05,
		Rule_19005_1_6_5_3_t06,
		Rule_19005_1_6_6_1_t01,
		Rule_19005_1_6_6_1_t02,
		Rule_19005_1_6_6_1_t03,
		Rule_19005_1_6_6_2_t01,
		Rule_19005_1_6_6_2_t02,
		Rule_19005_1_6_6_2_t03,
		Rule_19005_1_6_7_2_t01,
		Rule_19005_1_6_7_2_t02,
		Rule_19005_1_6_7_3_t01,
		Rule_19005_1_6_7_3_t02,
		Rule_19005_1_6_7_3_t03,
		Rule_19005_1_6_7_3_t04,
		Rule_19005_1_6_7_3_t05,
		Rule_19005_1_6_7_3_t06,
		Rule_19005_1_6_7_3_t07,
		Rule_19005_1_6_7_3_t08,
		Rule_19005_1_6_7_5_t01,
		Rule_19005_1_6_7_5_t02,
		Rule_19005_1_6_7_8_t01,
		Rule_19005_1_6_7_8_t02,
		Rule_19005_1_6_7_8_t03,
		Rule_19005_1_6_7_8_t04,
		Rule_19005_1_6_7_8_t05,
		Rule_19005_1_6_7_8_t06,
		Rule_19005_1_6_7_8_t07,
		Rule_19005_1_6_7_8_t08,
		Rule_19005_1_6_7_8_t09,
		Rule_19005_1_6_7_8_t10,
		Rule_19005_1_6_7_8_t11,
		Rule_19005_1_6_7_8_t12,
		Rule_19005_1_6_7_8_t13,
		Rule_19005_1_6_7_8_t14,
		Rule_19005_1_6_7_8_t15,
		Rule_19005_1_6_7_8_t16,
		Rule_19005_1_6_7_8_t17,
		Rule_19005_1_6_7_8_t18,
		Rule_19005_1_6_7_8_t19,
		Rule_19005_1_6_7_9_t01,
		Rule_19005_1_6_7_9_t02,
		Rule_19005_1_6_7_9_t03,
		Rule_19005_1_6_7_11_t01,
		Rule_19005_1_6_7_11_t02,
		Rule_19005_1_6_7_11_t03,
		Rule_19005_1_6_7_11_t04,
		Rule_19005_1_6_7_11_t05,
		Rule_19005_1_6_7_11_t06,
		Rule_19005_1_6_9_t01,
		Rule_19005_1_6_9_t02,
	}
}

// PDFA2aRules returns the PDF/A-2a rule set (ISO 19005-2:2011, Level A, 153 rules).
func PDFA2aRules() []Rule {
	return []Rule{
		Rule_19005_2_6_1_2_t01,
		Rule_19005_2_6_1_2_t02,
		Rule_19005_2_6_1_3_t01,
		Rule_19005_2_6_1_3_t02,
		Rule_19005_2_6_1_3_t03,
		Rule_19005_2_6_1_4_t02,
		Rule_19005_2_6_1_6_t01,
		Rule_19005_2_6_1_6_t02,
		Rule_19005_2_6_1_7_1_t01,
		Rule_19005_2_6_1_7_1_t02,
		Rule_19005_2_6_1_7_1_t03,
		Rule_19005_2_6_1_7_2_t01,
		Rule_19005_2_6_1_8_t01,
		Rule_19005_2_6_1_9_t01,
		Rule_19005_2_6_1_10_t01,
		Rule_19005_2_6_1_12_t01,
		Rule_19005_2_6_1_12_t02,
		Rule_19005_2_6_1_13_t01,
		Rule_19005_2_6_1_13_t02,
		Rule_19005_2_6_1_13_t03,
		Rule_19005_2_6_1_13_t04,
		Rule_19005_2_6_1_13_t05,
		Rule_19005_2_6_1_13_t07,
		Rule_19005_2_6_1_13_t08,
		Rule_19005_2_6_1_13_t09,
		Rule_19005_2_6_1_13_t10,
		Rule_19005_2_6_1_13_t11,
		Rule_19005_2_6_2_2_t01,
		Rule_19005_2_6_2_2_t02,
		Rule_19005_2_6_2_3_t01,
		Rule_19005_2_6_2_3_t02,
		Rule_19005_2_6_2_3_t03,
		Rule_19005_2_6_2_4_2_t01,
		Rule_19005_2_6_2_4_2_t02,
		Rule_19005_2_6_2_4_3_t02,
		Rule_19005_2_6_2_4_3_t03,
		Rule_19005_2_6_2_4_3_t04,
		Rule_19005_2_6_2_4_4_t01,
		Rule_19005_2_6_2_4_4_t02,
		Rule_19005_2_6_2_5_t01,
		Rule_19005_2_6_2_5_t02,
		Rule_19005_2_6_2_5_t03,
		Rule_19005_2_6_2_5_t04,
		Rule_19005_2_6_2_5_t05,
		Rule_19005_2_6_2_5_t06,
		Rule_19005_2_6_2_6_t01,
		Rule_19005_2_6_2_8_t01,
		Rule_19005_2_6_2_8_t02,
		Rule_19005_2_6_2_8_t03,
		Rule_19005_2_6_2_8_t04,
		Rule_19005_2_6_2_8_t05,
		Rule_19005_2_6_2_8_3_t01,
		Rule_19005_2_6_2_8_3_t02,
		Rule_19005_2_6_2_8_3_t03,
		Rule_19005_2_6_2_8_3_t04,
		Rule_19005_2_6_2_8_3_t05,
		Rule_19005_2_6_2_9_t01,
		Rule_19005_2_6_2_9_t02,
		Rule_19005_2_6_2_9_t03,
		Rule_19005_2_6_2_10_t01,
		Rule_19005_2_6_2_10_t02,
		Rule_19005_2_6_2_11_2_t01,
		Rule_19005_2_6_2_11_2_t02,
		Rule_19005_2_6_2_11_2_t03,
		Rule_19005_2_6_2_11_2_t04,
		Rule_19005_2_6_2_11_2_t05,
		Rule_19005_2_6_2_11_2_t06,
		Rule_19005_2_6_2_11_2_t07,
		Rule_19005_2_6_2_11_3_1_t01,
		Rule_19005_2_6_2_11_3_2_t01,
		Rule_19005_2_6_2_11_3_3_t01,
		Rule_19005_2_6_2_11_3_3_t02,
		Rule_19005_2_6_2_11_3_3_t03,
		Rule_19005_2_6_2_11_4_1_t01,
		Rule_19005_2_6_2_11_4_1_t02,
		Rule_19005_2_6_2_11_4_2_t01,
		Rule_19005_2_6_2_11_4_2_t02,
		Rule_19005_2_6_2_11_5_t01,
		Rule_19005_2_6_2_11_6_t01,
		Rule_19005_2_6_2_11_6_t02,
		Rule_19005_2_6_2_11_6_t03,
		Rule_19005_2_6_2_11_6_t04,
		Rule_19005_2_6_2_11_7_2_t01,
		Rule_19005_2_6_2_11_7_2_t02,
		Rule_19005_2_6_2_11_7_3_t01,
		Rule_19005_2_6_2_11_8_t01,
		Rule_19005_2_6_3_1_t01,
		Rule_19005_2_6_3_2_t01,
		Rule_19005_2_6_3_2_t02,
		Rule_19005_2_6_3_3_t01,
		Rule_19005_2_6_3_3_t02,
		Rule_19005_2_6_3_3_t03,
		Rule_19005_2_6_3_3_t04,
		Rule_19005_2_6_4_1_t01,
		Rule_19005_2_6_4_1_t02,
		Rule_19005_2_6_4_1_t03,
		Rule_19005_2_6_4_2_t01,
		Rule_19005_2_6_4_2_t02,
		Rule_19005_2_6_4_3_t01,
		Rule_19005_2_6_4_3_t02,
		Rule_19005_2_6_4_3_t03,
		Rule_19005_2_6_5_1_t01,
		Rule_19005_2_6_5_1_t02,
		Rule_19005_2_6_5_2_t01,
		Rule_19005_2_6_5_2_t02,
		Rule_19005_2_6_6_2_1_t01,
		Rule_19005_2_6_6_2_1_t02,
		Rule_19005_2_6_6_2_1_t03,
		Rule_19005_2_6_6_2_1_t04,
		Rule_19005_2_6_6_2_1_t05,
		Rule_19005_2_6_6_2_3_1_t01,
		Rule_19005_2_6_6_2_3_1_t02,
		Rule_19005_2_6_6_2_3_2_t01,
		Rule_19005_2_6_6_2_3_3_t01,
		Rule_19005_2_6_6_2_3_3_t02,
		Rule_19005_2_6_6_2_3_3_t03,
		Rule_19005_2_6_6_2_3_3_t04,
		Rule_19005_2_6_6_2_3_3_t05,
		Rule_19005_2_6_6_2_3_3_t06,
		Rule_19005_2_6_6_2_3_3_t07,
		Rule_19005_2_6_6_2_3_3_t08,
		Rule_19005_2_6_6_2_3_3_t09,
		Rule_19005_2_6_6_2_3_3_t10,
		Rule_19005_2_6_6_2_3_3_t11,
		Rule_19005_2_6_6_2_3_3_t12,
		Rule_19005_2_6_6_2_3_3_t13,
		Rule_19005_2_6_6_2_3_3_t14,
		Rule_19005_2_6_6_2_3_3_t15,
		Rule_19005_2_6_6_2_3_3_t16,
		Rule_19005_2_6_6_2_3_3_t17,
		Rule_19005_2_6_6_2_3_3_t18,
		Rule_19005_2_6_6_4_t01,
		Rule_19005_2_6_6_4_t02,
		Rule_19005_2_6_6_4_t03,
		Rule_19005_2_6_6_4_t04,
		Rule_19005_2_6_6_4_t05,
		Rule_19005_2_6_6_4_t06,
		Rule_19005_2_6_6_4_t07,
		Rule_19005_2_6_7_2_2_t01,
		Rule_19005_2_6_7_3_3_t01,
		Rule_19005_2_6_7_3_4_t01,
		Rule_19005_2_6_7_3_4_t02,
		Rule_19005_2_6_7_3_4_t03,
		Rule_19005_2_6_7_4_t01,
		Rule_19005_2_6_8_t02,
		Rule_19005_2_6_8_t05,
		Rule_19005_2_6_9_t01,
		Rule_19005_2_6_9_t02,
		Rule_19005_2_6_9_t03,
		Rule_19005_2_6_9_t04,
		Rule_19005_2_6_10_t01,
		Rule_19005_2_6_10_t02,
		Rule_19005_2_6_11_t01,
	}
}

// PDFA2bRules returns the PDF/A-2b rule set (ISO 19005-2:2011, Level B, 144 rules).
func PDFA2bRules() []Rule {
	return []Rule{
		Rule_19005_2_6_1_2_t01,
		Rule_19005_2_6_1_2_t02,
		Rule_19005_2_6_1_3_t01,
		Rule_19005_2_6_1_3_t02,
		Rule_19005_2_6_1_3_t03,
		Rule_19005_2_6_1_4_t02,
		Rule_19005_2_6_1_6_t01,
		Rule_19005_2_6_1_6_t02,
		Rule_19005_2_6_1_7_1_t01,
		Rule_19005_2_6_1_7_1_t02,
		Rule_19005_2_6_1_7_1_t03,
		Rule_19005_2_6_1_7_2_t01,
		Rule_19005_2_6_1_8_t01,
		Rule_19005_2_6_1_9_t01,
		Rule_19005_2_6_1_10_t01,
		Rule_19005_2_6_1_12_t01,
		Rule_19005_2_6_1_12_t02,
		Rule_19005_2_6_1_13_t01,
		Rule_19005_2_6_1_13_t02,
		Rule_19005_2_6_1_13_t03,
		Rule_19005_2_6_1_13_t04,
		Rule_19005_2_6_1_13_t05,
		Rule_19005_2_6_1_13_t07,
		Rule_19005_2_6_1_13_t08,
		Rule_19005_2_6_1_13_t09,
		Rule_19005_2_6_1_13_t10,
		Rule_19005_2_6_1_13_t11,
		Rule_19005_2_6_2_2_t01,
		Rule_19005_2_6_2_2_t02,
		Rule_19005_2_6_2_3_t01,
		Rule_19005_2_6_2_3_t02,
		Rule_19005_2_6_2_3_t03,
		Rule_19005_2_6_2_4_2_t01,
		Rule_19005_2_6_2_4_2_t02,
		Rule_19005_2_6_2_4_3_t02,
		Rule_19005_2_6_2_4_3_t03,
		Rule_19005_2_6_2_4_3_t04,
		Rule_19005_2_6_2_4_4_t01,
		Rule_19005_2_6_2_4_4_t02,
		Rule_19005_2_6_2_5_t01,
		Rule_19005_2_6_2_5_t02,
		Rule_19005_2_6_2_5_t03,
		Rule_19005_2_6_2_5_t04,
		Rule_19005_2_6_2_5_t05,
		Rule_19005_2_6_2_5_t06,
		Rule_19005_2_6_2_6_t01,
		Rule_19005_2_6_2_8_t01,
		Rule_19005_2_6_2_8_t02,
		Rule_19005_2_6_2_8_t03,
		Rule_19005_2_6_2_8_t04,
		Rule_19005_2_6_2_8_t05,
		Rule_19005_2_6_2_8_3_t01,
		Rule_19005_2_6_2_8_3_t02,
		Rule_19005_2_6_2_8_3_t03,
		Rule_19005_2_6_2_8_3_t04,
		Rule_19005_2_6_2_8_3_t05,
		Rule_19005_2_6_2_9_t01,
		Rule_19005_2_6_2_9_t02,
		Rule_19005_2_6_2_9_t03,
		Rule_19005_2_6_2_10_t01,
		Rule_19005_2_6_2_10_t02,
		Rule_19005_2_6_2_11_2_t01,
		Rule_19005_2_6_2_11_2_t02,
		Rule_19005_2_6_2_11_2_t03,
		Rule_19005_2_6_2_11_2_t04,
		Rule_19005_2_6_2_11_2_t05,
		Rule_19005_2_6_2_11_2_t06,
		Rule_19005_2_6_2_11_2_t07,
		Rule_19005_2_6_2_11_3_1_t01,
		Rule_19005_2_6_2_11_3_2_t01,
		Rule_19005_2_6_2_11_3_3_t01,
		Rule_19005_2_6_2_11_3_3_t02,
		Rule_19005_2_6_2_11_3_3_t03,
		Rule_19005_2_6_2_11_4_1_t01,
		Rule_19005_2_6_2_11_4_1_t02,
		Rule_19005_2_6_2_11_4_2_t01,
		Rule_19005_2_6_2_11_4_2_t02,
		Rule_19005_2_6_2_11_5_t01,
		Rule_19005_2_6_2_11_6_t01,
		Rule_19005_2_6_2_11_6_t02,
		Rule_19005_2_6_2_11_6_t03,
		Rule_19005_2_6_2_11_6_t04,
		Rule_19005_2_6_2_11_8_t01,
		Rule_19005_2_6_3_1_t01,
		Rule_19005_2_6_3_2_t01,
		Rule_19005_2_6_3_2_t02,
		Rule_19005_2_6_3_3_t01,
		Rule_19005_2_6_3_3_t02,
		Rule_19005_2_6_3_3_t03,
		Rule_19005_2_6_3_3_t04,
		Rule_19005_2_6_4_1_t01,
		Rule_19005_2_6_4_1_t02,
		Rule_19005_2_6_4_1_t03,
		Rule_19005_2_6_4_2_t01,
		Rule_19005_2_6_4_2_t02,
		Rule_19005_2_6_4_3_t01,
		Rule_19005_2_6_4_3_t02,
		Rule_19005_2_6_4_3_t03,
		Rule_19005_2_6_5_1_t01,
		Rule_19005_2_6_5_1_t02,
		Rule_19005_2_6_5_2_t01,
		Rule_19005_2_6_5_2_t02,
		Rule_19005_2_6_6_2_1_t01,
		Rule_19005_2_6_6_2_1_t02,
		Rule_19005_2_6_6_2_1_t03,
		Rule_19005_2_6_6_2_1_t04,
		Rule_19005_2_6_6_2_1_t05,
		Rule_19005_2_6_6_2_3_1_t01,
		Rule_19005_2_6_6_2_3_1_t02,
		Rule_19005_2_6_6_2_3_2_t01,
		Rule_19005_2_6_6_2_3_3_t01,
		Rule_19005_2_6_6_2_3_3_t02,
		Rule_19005_2_6_6_2_3_3_t03,
		Rule_19005_2_6_6_2_3_3_t04,
		Rule_19005_2_6_6_2_3_3_t05,
		Rule_19005_2_6_6_2_3_3_t06,
		Rule_19005_2_6_6_2_3_3_t07,
		Rule_19005_2_6_6_2_3_3_t08,
		Rule_19005_2_6_6_2_3_3_t09,
		Rule_19005_2_6_6_2_3_3_t10,
		Rule_19005_2_6_6_2_3_3_t11,
		Rule_19005_2_6_6_2_3_3_t12,
		Rule_19005_2_6_6_2_3_3_t13,
		Rule_19005_2_6_6_2_3_3_t14,
		Rule_19005_2_6_6_2_3_3_t15,
		Rule_19005_2_6_6_2_3_3_t16,
		Rule_19005_2_6_6_2_3_3_t17,
		Rule_19005_2_6_6_2_3_3_t18,
		Rule_19005_2_6_6_4_t01,
		Rule_19005_2_6_6_4_t02,
		Rule_19005_2_6_6_4_t03,
		Rule_19005_2_6_6_4_t04,
		Rule_19005_2_6_6_4_t05,
		Rule_19005_2_6_6_4_t06,
		Rule_19005_2_6_6_4_t07,
		Rule_19005_2_6_8_t02,
		Rule_19005_2_6_8_t05,
		Rule_19005_2_6_9_t01,
		Rule_19005_2_6_9_t02,
		Rule_19005_2_6_9_t03,
		Rule_19005_2_6_9_t04,
		Rule_19005_2_6_10_t01,
		Rule_19005_2_6_10_t02,
		Rule_19005_2_6_11_t01,
	}
}

// PDFA2uRules returns the PDF/A-2u rule set (ISO 19005-2:2011, Level U, 146 rules).
func PDFA2uRules() []Rule { //nolint:dupl // PDF/A profiles share overlapping rule sets by design.
	return []Rule{
		Rule_19005_2_6_1_2_t01,
		Rule_19005_2_6_1_2_t02,
		Rule_19005_2_6_1_3_t01,
		Rule_19005_2_6_1_3_t02,
		Rule_19005_2_6_1_3_t03,
		Rule_19005_2_6_1_4_t02,
		Rule_19005_2_6_1_6_t01,
		Rule_19005_2_6_1_6_t02,
		Rule_19005_2_6_1_7_1_t01,
		Rule_19005_2_6_1_7_1_t02,
		Rule_19005_2_6_1_7_1_t03,
		Rule_19005_2_6_1_7_2_t01,
		Rule_19005_2_6_1_8_t01,
		Rule_19005_2_6_1_9_t01,
		Rule_19005_2_6_1_10_t01,
		Rule_19005_2_6_1_12_t01,
		Rule_19005_2_6_1_12_t02,
		Rule_19005_2_6_1_13_t01,
		Rule_19005_2_6_1_13_t02,
		Rule_19005_2_6_1_13_t03,
		Rule_19005_2_6_1_13_t04,
		Rule_19005_2_6_1_13_t05,
		Rule_19005_2_6_1_13_t07,
		Rule_19005_2_6_1_13_t08,
		Rule_19005_2_6_1_13_t09,
		Rule_19005_2_6_1_13_t10,
		Rule_19005_2_6_1_13_t11,
		Rule_19005_2_6_2_2_t01,
		Rule_19005_2_6_2_2_t02,
		Rule_19005_2_6_2_3_t01,
		Rule_19005_2_6_2_3_t02,
		Rule_19005_2_6_2_3_t03,
		Rule_19005_2_6_2_4_2_t01,
		Rule_19005_2_6_2_4_2_t02,
		Rule_19005_2_6_2_4_3_t02,
		Rule_19005_2_6_2_4_3_t03,
		Rule_19005_2_6_2_4_3_t04,
		Rule_19005_2_6_2_4_4_t01,
		Rule_19005_2_6_2_4_4_t02,
		Rule_19005_2_6_2_5_t01,
		Rule_19005_2_6_2_5_t02,
		Rule_19005_2_6_2_5_t03,
		Rule_19005_2_6_2_5_t04,
		Rule_19005_2_6_2_5_t05,
		Rule_19005_2_6_2_5_t06,
		Rule_19005_2_6_2_6_t01,
		Rule_19005_2_6_2_8_t01,
		Rule_19005_2_6_2_8_t02,
		Rule_19005_2_6_2_8_t03,
		Rule_19005_2_6_2_8_t04,
		Rule_19005_2_6_2_8_t05,
		Rule_19005_2_6_2_8_3_t01,
		Rule_19005_2_6_2_8_3_t02,
		Rule_19005_2_6_2_8_3_t03,
		Rule_19005_2_6_2_8_3_t04,
		Rule_19005_2_6_2_8_3_t05,
		Rule_19005_2_6_2_9_t01,
		Rule_19005_2_6_2_9_t02,
		Rule_19005_2_6_2_9_t03,
		Rule_19005_2_6_2_10_t01,
		Rule_19005_2_6_2_10_t02,
		Rule_19005_2_6_2_11_2_t01,
		Rule_19005_2_6_2_11_2_t02,
		Rule_19005_2_6_2_11_2_t03,
		Rule_19005_2_6_2_11_2_t04,
		Rule_19005_2_6_2_11_2_t05,
		Rule_19005_2_6_2_11_2_t06,
		Rule_19005_2_6_2_11_2_t07,
		Rule_19005_2_6_2_11_3_1_t01,
		Rule_19005_2_6_2_11_3_2_t01,
		Rule_19005_2_6_2_11_3_3_t01,
		Rule_19005_2_6_2_11_3_3_t02,
		Rule_19005_2_6_2_11_3_3_t03,
		Rule_19005_2_6_2_11_4_1_t01,
		Rule_19005_2_6_2_11_4_1_t02,
		Rule_19005_2_6_2_11_4_2_t01,
		Rule_19005_2_6_2_11_4_2_t02,
		Rule_19005_2_6_2_11_5_t01,
		Rule_19005_2_6_2_11_6_t01,
		Rule_19005_2_6_2_11_6_t02,
		Rule_19005_2_6_2_11_6_t03,
		Rule_19005_2_6_2_11_6_t04,
		Rule_19005_2_6_2_11_7_2_t01,
		Rule_19005_2_6_2_11_7_2_t02,
		Rule_19005_2_6_2_11_8_t01,
		Rule_19005_2_6_3_1_t01,
		Rule_19005_2_6_3_2_t01,
		Rule_19005_2_6_3_2_t02,
		Rule_19005_2_6_3_3_t01,
		Rule_19005_2_6_3_3_t02,
		Rule_19005_2_6_3_3_t03,
		Rule_19005_2_6_3_3_t04,
		Rule_19005_2_6_4_1_t01,
		Rule_19005_2_6_4_1_t02,
		Rule_19005_2_6_4_1_t03,
		Rule_19005_2_6_4_2_t01,
		Rule_19005_2_6_4_2_t02,
		Rule_19005_2_6_4_3_t01,
		Rule_19005_2_6_4_3_t02,
		Rule_19005_2_6_4_3_t03,
		Rule_19005_2_6_5_1_t01,
		Rule_19005_2_6_5_1_t02,
		Rule_19005_2_6_5_2_t01,
		Rule_19005_2_6_5_2_t02,
		Rule_19005_2_6_6_2_1_t01,
		Rule_19005_2_6_6_2_1_t02,
		Rule_19005_2_6_6_2_1_t03,
		Rule_19005_2_6_6_2_1_t04,
		Rule_19005_2_6_6_2_1_t05,
		Rule_19005_2_6_6_2_3_1_t01,
		Rule_19005_2_6_6_2_3_1_t02,
		Rule_19005_2_6_6_2_3_2_t01,
		Rule_19005_2_6_6_2_3_3_t01,
		Rule_19005_2_6_6_2_3_3_t02,
		Rule_19005_2_6_6_2_3_3_t03,
		Rule_19005_2_6_6_2_3_3_t04,
		Rule_19005_2_6_6_2_3_3_t05,
		Rule_19005_2_6_6_2_3_3_t06,
		Rule_19005_2_6_6_2_3_3_t07,
		Rule_19005_2_6_6_2_3_3_t08,
		Rule_19005_2_6_6_2_3_3_t09,
		Rule_19005_2_6_6_2_3_3_t10,
		Rule_19005_2_6_6_2_3_3_t11,
		Rule_19005_2_6_6_2_3_3_t12,
		Rule_19005_2_6_6_2_3_3_t13,
		Rule_19005_2_6_6_2_3_3_t14,
		Rule_19005_2_6_6_2_3_3_t15,
		Rule_19005_2_6_6_2_3_3_t16,
		Rule_19005_2_6_6_2_3_3_t17,
		Rule_19005_2_6_6_2_3_3_t18,
		Rule_19005_2_6_6_4_t01,
		Rule_19005_2_6_6_4_t02,
		Rule_19005_2_6_6_4_t03,
		Rule_19005_2_6_6_4_t04,
		Rule_19005_2_6_6_4_t05,
		Rule_19005_2_6_6_4_t06,
		Rule_19005_2_6_6_4_t07,
		Rule_19005_2_6_8_t02,
		Rule_19005_2_6_8_t05,
		Rule_19005_2_6_9_t01,
		Rule_19005_2_6_9_t02,
		Rule_19005_2_6_9_t03,
		Rule_19005_2_6_9_t04,
		Rule_19005_2_6_10_t01,
		Rule_19005_2_6_10_t02,
		Rule_19005_2_6_11_t01,
	}
}

// PDFA3aRules returns the PDF/A-3a rule set (ISO 19005-3:2012, Level A, 155 rules).
func PDFA3aRules() []Rule {
	return []Rule{
		Rule_19005_3_6_1_2_t01,
		Rule_19005_3_6_1_2_t02,
		Rule_19005_3_6_1_3_t01,
		Rule_19005_3_6_1_3_t02,
		Rule_19005_3_6_1_3_t03,
		Rule_19005_3_6_1_4_t02,
		Rule_19005_3_6_1_6_t01,
		Rule_19005_3_6_1_6_t02,
		Rule_19005_3_6_1_7_1_t01,
		Rule_19005_3_6_1_7_1_t02,
		Rule_19005_3_6_1_7_1_t03,
		Rule_19005_3_6_1_7_2_t01,
		Rule_19005_3_6_1_8_t01,
		Rule_19005_3_6_1_9_t01,
		Rule_19005_3_6_1_10_t01,
		Rule_19005_3_6_1_12_t01,
		Rule_19005_3_6_1_12_t02,
		Rule_19005_3_6_1_13_t01,
		Rule_19005_3_6_1_13_t02,
		Rule_19005_3_6_1_13_t03,
		Rule_19005_3_6_1_13_t04,
		Rule_19005_3_6_1_13_t05,
		Rule_19005_3_6_1_13_t07,
		Rule_19005_3_6_1_13_t08,
		Rule_19005_3_6_1_13_t09,
		Rule_19005_3_6_1_13_t10,
		Rule_19005_3_6_1_13_t11,
		Rule_19005_3_6_2_2_t01,
		Rule_19005_3_6_2_2_t02,
		Rule_19005_3_6_2_3_t01,
		Rule_19005_3_6_2_3_t02,
		Rule_19005_3_6_2_3_t03,
		Rule_19005_3_6_2_4_2_t01,
		Rule_19005_3_6_2_4_2_t02,
		Rule_19005_3_6_2_4_3_t02,
		Rule_19005_3_6_2_4_3_t03,
		Rule_19005_3_6_2_4_3_t04,
		Rule_19005_3_6_2_4_4_t01,
		Rule_19005_3_6_2_4_4_t02,
		Rule_19005_3_6_2_5_t01,
		Rule_19005_3_6_2_5_t02,
		Rule_19005_3_6_2_5_t03,
		Rule_19005_3_6_2_5_t04,
		Rule_19005_3_6_2_5_t05,
		Rule_19005_3_6_2_5_t06,
		Rule_19005_3_6_2_6_t01,
		Rule_19005_3_6_2_8_t01,
		Rule_19005_3_6_2_8_t02,
		Rule_19005_3_6_2_8_t03,
		Rule_19005_3_6_2_8_t04,
		Rule_19005_3_6_2_8_t05,
		Rule_19005_3_6_2_8_3_t01,
		Rule_19005_3_6_2_8_3_t02,
		Rule_19005_3_6_2_8_3_t03,
		Rule_19005_3_6_2_8_3_t04,
		Rule_19005_3_6_2_8_3_t05,
		Rule_19005_3_6_2_9_t01,
		Rule_19005_3_6_2_9_t02,
		Rule_19005_3_6_2_9_t03,
		Rule_19005_3_6_2_10_t01,
		Rule_19005_3_6_2_10_t02,
		Rule_19005_3_6_2_11_2_t01,
		Rule_19005_3_6_2_11_2_t02,
		Rule_19005_3_6_2_11_2_t03,
		Rule_19005_3_6_2_11_2_t04,
		Rule_19005_3_6_2_11_2_t05,
		Rule_19005_3_6_2_11_2_t06,
		Rule_19005_3_6_2_11_2_t07,
		Rule_19005_3_6_2_11_3_1_t01,
		Rule_19005_3_6_2_11_3_2_t01,
		Rule_19005_3_6_2_11_3_3_t01,
		Rule_19005_3_6_2_11_3_3_t02,
		Rule_19005_3_6_2_11_3_3_t03,
		Rule_19005_3_6_2_11_4_1_t01,
		Rule_19005_3_6_2_11_4_1_t02,
		Rule_19005_3_6_2_11_4_2_t01,
		Rule_19005_3_6_2_11_4_2_t02,
		Rule_19005_3_6_2_11_5_t01,
		Rule_19005_3_6_2_11_6_t01,
		Rule_19005_3_6_2_11_6_t02,
		Rule_19005_3_6_2_11_6_t03,
		Rule_19005_3_6_2_11_6_t04,
		Rule_19005_3_6_2_11_7_2_t01,
		Rule_19005_3_6_2_11_7_2_t02,
		Rule_19005_3_6_2_11_7_3_t01,
		Rule_19005_3_6_2_11_8_t01,
		Rule_19005_3_6_3_1_t01,
		Rule_19005_3_6_3_2_t01,
		Rule_19005_3_6_3_2_t02,
		Rule_19005_3_6_3_3_t01,
		Rule_19005_3_6_3_3_t02,
		Rule_19005_3_6_3_3_t03,
		Rule_19005_3_6_3_3_t04,
		Rule_19005_3_6_4_1_t01,
		Rule_19005_3_6_4_1_t02,
		Rule_19005_3_6_4_1_t03,
		Rule_19005_3_6_4_2_t01,
		Rule_19005_3_6_4_2_t02,
		Rule_19005_3_6_4_3_t01,
		Rule_19005_3_6_4_3_t02,
		Rule_19005_3_6_4_3_t03,
		Rule_19005_3_6_5_1_t01,
		Rule_19005_3_6_5_1_t02,
		Rule_19005_3_6_5_2_t01,
		Rule_19005_3_6_5_2_t02,
		Rule_19005_3_6_6_2_1_t01,
		Rule_19005_3_6_6_2_1_t02,
		Rule_19005_3_6_6_2_1_t03,
		Rule_19005_3_6_6_2_1_t04,
		Rule_19005_3_6_6_2_1_t05,
		Rule_19005_3_6_6_2_3_1_t01,
		Rule_19005_3_6_6_2_3_1_t02,
		Rule_19005_3_6_6_2_3_2_t01,
		Rule_19005_3_6_6_2_3_3_t01,
		Rule_19005_3_6_6_2_3_3_t02,
		Rule_19005_3_6_6_2_3_3_t03,
		Rule_19005_3_6_6_2_3_3_t04,
		Rule_19005_3_6_6_2_3_3_t05,
		Rule_19005_3_6_6_2_3_3_t06,
		Rule_19005_3_6_6_2_3_3_t07,
		Rule_19005_3_6_6_2_3_3_t08,
		Rule_19005_3_6_6_2_3_3_t09,
		Rule_19005_3_6_6_2_3_3_t10,
		Rule_19005_3_6_6_2_3_3_t11,
		Rule_19005_3_6_6_2_3_3_t12,
		Rule_19005_3_6_6_2_3_3_t13,
		Rule_19005_3_6_6_2_3_3_t14,
		Rule_19005_3_6_6_2_3_3_t15,
		Rule_19005_3_6_6_2_3_3_t16,
		Rule_19005_3_6_6_2_3_3_t17,
		Rule_19005_3_6_6_2_3_3_t18,
		Rule_19005_3_6_6_4_t01,
		Rule_19005_3_6_6_4_t02,
		Rule_19005_3_6_6_4_t03,
		Rule_19005_3_6_6_4_t04,
		Rule_19005_3_6_6_4_t05,
		Rule_19005_3_6_6_4_t06,
		Rule_19005_3_6_6_4_t07,
		Rule_19005_3_6_7_2_2_t01,
		Rule_19005_3_6_7_3_3_t01,
		Rule_19005_3_6_7_3_4_t01,
		Rule_19005_3_6_7_3_4_t02,
		Rule_19005_3_6_7_3_4_t03,
		Rule_19005_3_6_7_4_t01,
		Rule_19005_3_6_8_t01,
		Rule_19005_3_6_8_t02,
		Rule_19005_3_6_8_t03,
		Rule_19005_3_6_8_t04,
		Rule_19005_3_6_9_t01,
		Rule_19005_3_6_9_t02,
		Rule_19005_3_6_9_t03,
		Rule_19005_3_6_9_t04,
		Rule_19005_3_6_10_t01,
		Rule_19005_3_6_10_t02,
		Rule_19005_3_6_11_t01,
	}
}

// PDFA3bRules returns the PDF/A-3b rule set (ISO 19005-3:2012, Level B, 146 rules).
func PDFA3bRules() []Rule { //nolint:dupl // PDF/A profiles share overlapping rule sets by design.
	return []Rule{
		Rule_19005_3_6_1_2_t01,
		Rule_19005_3_6_1_2_t02,
		Rule_19005_3_6_1_3_t01,
		Rule_19005_3_6_1_3_t02,
		Rule_19005_3_6_1_3_t03,
		Rule_19005_3_6_1_4_t02,
		Rule_19005_3_6_1_6_t01,
		Rule_19005_3_6_1_6_t02,
		Rule_19005_3_6_1_7_1_t01,
		Rule_19005_3_6_1_7_1_t02,
		Rule_19005_3_6_1_7_1_t03,
		Rule_19005_3_6_1_7_2_t01,
		Rule_19005_3_6_1_8_t01,
		Rule_19005_3_6_1_9_t01,
		Rule_19005_3_6_1_10_t01,
		Rule_19005_3_6_1_12_t01,
		Rule_19005_3_6_1_12_t02,
		Rule_19005_3_6_1_13_t01,
		Rule_19005_3_6_1_13_t02,
		Rule_19005_3_6_1_13_t03,
		Rule_19005_3_6_1_13_t04,
		Rule_19005_3_6_1_13_t05,
		Rule_19005_3_6_1_13_t07,
		Rule_19005_3_6_1_13_t08,
		Rule_19005_3_6_1_13_t09,
		Rule_19005_3_6_1_13_t10,
		Rule_19005_3_6_1_13_t11,
		Rule_19005_3_6_2_2_t01,
		Rule_19005_3_6_2_2_t02,
		Rule_19005_3_6_2_3_t01,
		Rule_19005_3_6_2_3_t02,
		Rule_19005_3_6_2_3_t03,
		Rule_19005_3_6_2_4_2_t01,
		Rule_19005_3_6_2_4_2_t02,
		Rule_19005_3_6_2_4_3_t02,
		Rule_19005_3_6_2_4_3_t03,
		Rule_19005_3_6_2_4_3_t04,
		Rule_19005_3_6_2_4_4_t01,
		Rule_19005_3_6_2_4_4_t02,
		Rule_19005_3_6_2_5_t01,
		Rule_19005_3_6_2_5_t02,
		Rule_19005_3_6_2_5_t03,
		Rule_19005_3_6_2_5_t04,
		Rule_19005_3_6_2_5_t05,
		Rule_19005_3_6_2_5_t06,
		Rule_19005_3_6_2_6_t01,
		Rule_19005_3_6_2_8_t01,
		Rule_19005_3_6_2_8_t02,
		Rule_19005_3_6_2_8_t03,
		Rule_19005_3_6_2_8_t04,
		Rule_19005_3_6_2_8_t05,
		Rule_19005_3_6_2_8_3_t01,
		Rule_19005_3_6_2_8_3_t02,
		Rule_19005_3_6_2_8_3_t03,
		Rule_19005_3_6_2_8_3_t04,
		Rule_19005_3_6_2_8_3_t05,
		Rule_19005_3_6_2_9_t01,
		Rule_19005_3_6_2_9_t02,
		Rule_19005_3_6_2_9_t03,
		Rule_19005_3_6_2_10_t01,
		Rule_19005_3_6_2_10_t02,
		Rule_19005_3_6_2_11_2_t01,
		Rule_19005_3_6_2_11_2_t02,
		Rule_19005_3_6_2_11_2_t03,
		Rule_19005_3_6_2_11_2_t04,
		Rule_19005_3_6_2_11_2_t05,
		Rule_19005_3_6_2_11_2_t06,
		Rule_19005_3_6_2_11_2_t07,
		Rule_19005_3_6_2_11_3_1_t01,
		Rule_19005_3_6_2_11_3_2_t01,
		Rule_19005_3_6_2_11_3_3_t01,
		Rule_19005_3_6_2_11_3_3_t02,
		Rule_19005_3_6_2_11_3_3_t03,
		Rule_19005_3_6_2_11_4_1_t01,
		Rule_19005_3_6_2_11_4_1_t02,
		Rule_19005_3_6_2_11_4_2_t01,
		Rule_19005_3_6_2_11_4_2_t02,
		Rule_19005_3_6_2_11_5_t01,
		Rule_19005_3_6_2_11_6_t01,
		Rule_19005_3_6_2_11_6_t02,
		Rule_19005_3_6_2_11_6_t03,
		Rule_19005_3_6_2_11_6_t04,
		Rule_19005_3_6_2_11_8_t01,
		Rule_19005_3_6_3_1_t01,
		Rule_19005_3_6_3_2_t01,
		Rule_19005_3_6_3_2_t02,
		Rule_19005_3_6_3_3_t01,
		Rule_19005_3_6_3_3_t02,
		Rule_19005_3_6_3_3_t03,
		Rule_19005_3_6_3_3_t04,
		Rule_19005_3_6_4_1_t01,
		Rule_19005_3_6_4_1_t02,
		Rule_19005_3_6_4_1_t03,
		Rule_19005_3_6_4_2_t01,
		Rule_19005_3_6_4_2_t02,
		Rule_19005_3_6_4_3_t01,
		Rule_19005_3_6_4_3_t02,
		Rule_19005_3_6_4_3_t03,
		Rule_19005_3_6_5_1_t01,
		Rule_19005_3_6_5_1_t02,
		Rule_19005_3_6_5_2_t01,
		Rule_19005_3_6_5_2_t02,
		Rule_19005_3_6_6_2_1_t01,
		Rule_19005_3_6_6_2_1_t02,
		Rule_19005_3_6_6_2_1_t03,
		Rule_19005_3_6_6_2_1_t04,
		Rule_19005_3_6_6_2_1_t05,
		Rule_19005_3_6_6_2_3_1_t01,
		Rule_19005_3_6_6_2_3_1_t02,
		Rule_19005_3_6_6_2_3_2_t01,
		Rule_19005_3_6_6_2_3_3_t01,
		Rule_19005_3_6_6_2_3_3_t02,
		Rule_19005_3_6_6_2_3_3_t03,
		Rule_19005_3_6_6_2_3_3_t04,
		Rule_19005_3_6_6_2_3_3_t05,
		Rule_19005_3_6_6_2_3_3_t06,
		Rule_19005_3_6_6_2_3_3_t07,
		Rule_19005_3_6_6_2_3_3_t08,
		Rule_19005_3_6_6_2_3_3_t09,
		Rule_19005_3_6_6_2_3_3_t10,
		Rule_19005_3_6_6_2_3_3_t11,
		Rule_19005_3_6_6_2_3_3_t12,
		Rule_19005_3_6_6_2_3_3_t13,
		Rule_19005_3_6_6_2_3_3_t14,
		Rule_19005_3_6_6_2_3_3_t15,
		Rule_19005_3_6_6_2_3_3_t16,
		Rule_19005_3_6_6_2_3_3_t17,
		Rule_19005_3_6_6_2_3_3_t18,
		Rule_19005_3_6_6_4_t01,
		Rule_19005_3_6_6_4_t02,
		Rule_19005_3_6_6_4_t03,
		Rule_19005_3_6_6_4_t04,
		Rule_19005_3_6_6_4_t05,
		Rule_19005_3_6_6_4_t06,
		Rule_19005_3_6_6_4_t07,
		Rule_19005_3_6_8_t01,
		Rule_19005_3_6_8_t02,
		Rule_19005_3_6_8_t03,
		Rule_19005_3_6_8_t04,
		Rule_19005_3_6_9_t01,
		Rule_19005_3_6_9_t02,
		Rule_19005_3_6_9_t03,
		Rule_19005_3_6_9_t04,
		Rule_19005_3_6_10_t01,
		Rule_19005_3_6_10_t02,
		Rule_19005_3_6_11_t01,
	}
}

// PDFA3uRules returns the PDF/A-3u rule set (ISO 19005-3:2012, Level U, 148 rules).
func PDFA3uRules() []Rule {
	return []Rule{
		Rule_19005_3_6_1_2_t01,
		Rule_19005_3_6_1_2_t02,
		Rule_19005_3_6_1_3_t01,
		Rule_19005_3_6_1_3_t02,
		Rule_19005_3_6_1_3_t03,
		Rule_19005_3_6_1_4_t02,
		Rule_19005_3_6_1_6_t01,
		Rule_19005_3_6_1_6_t02,
		Rule_19005_3_6_1_7_1_t01,
		Rule_19005_3_6_1_7_1_t02,
		Rule_19005_3_6_1_7_1_t03,
		Rule_19005_3_6_1_7_2_t01,
		Rule_19005_3_6_1_8_t01,
		Rule_19005_3_6_1_9_t01,
		Rule_19005_3_6_1_10_t01,
		Rule_19005_3_6_1_12_t01,
		Rule_19005_3_6_1_12_t02,
		Rule_19005_3_6_1_13_t01,
		Rule_19005_3_6_1_13_t02,
		Rule_19005_3_6_1_13_t03,
		Rule_19005_3_6_1_13_t04,
		Rule_19005_3_6_1_13_t05,
		Rule_19005_3_6_1_13_t07,
		Rule_19005_3_6_1_13_t08,
		Rule_19005_3_6_1_13_t09,
		Rule_19005_3_6_1_13_t10,
		Rule_19005_3_6_1_13_t11,
		Rule_19005_3_6_2_2_t01,
		Rule_19005_3_6_2_2_t02,
		Rule_19005_3_6_2_3_t01,
		Rule_19005_3_6_2_3_t02,
		Rule_19005_3_6_2_3_t03,
		Rule_19005_3_6_2_4_2_t01,
		Rule_19005_3_6_2_4_2_t02,
		Rule_19005_3_6_2_4_3_t02,
		Rule_19005_3_6_2_4_3_t03,
		Rule_19005_3_6_2_4_3_t04,
		Rule_19005_3_6_2_4_4_t01,
		Rule_19005_3_6_2_4_4_t02,
		Rule_19005_3_6_2_5_t01,
		Rule_19005_3_6_2_5_t02,
		Rule_19005_3_6_2_5_t03,
		Rule_19005_3_6_2_5_t04,
		Rule_19005_3_6_2_5_t05,
		Rule_19005_3_6_2_5_t06,
		Rule_19005_3_6_2_6_t01,
		Rule_19005_3_6_2_8_t01,
		Rule_19005_3_6_2_8_t02,
		Rule_19005_3_6_2_8_t03,
		Rule_19005_3_6_2_8_t04,
		Rule_19005_3_6_2_8_t05,
		Rule_19005_3_6_2_8_3_t01,
		Rule_19005_3_6_2_8_3_t02,
		Rule_19005_3_6_2_8_3_t03,
		Rule_19005_3_6_2_8_3_t04,
		Rule_19005_3_6_2_8_3_t05,
		Rule_19005_3_6_2_9_t01,
		Rule_19005_3_6_2_9_t02,
		Rule_19005_3_6_2_9_t03,
		Rule_19005_3_6_2_10_t01,
		Rule_19005_3_6_2_10_t02,
		Rule_19005_3_6_2_11_2_t01,
		Rule_19005_3_6_2_11_2_t02,
		Rule_19005_3_6_2_11_2_t03,
		Rule_19005_3_6_2_11_2_t04,
		Rule_19005_3_6_2_11_2_t05,
		Rule_19005_3_6_2_11_2_t06,
		Rule_19005_3_6_2_11_2_t07,
		Rule_19005_3_6_2_11_3_1_t01,
		Rule_19005_3_6_2_11_3_2_t01,
		Rule_19005_3_6_2_11_3_3_t01,
		Rule_19005_3_6_2_11_3_3_t02,
		Rule_19005_3_6_2_11_3_3_t03,
		Rule_19005_3_6_2_11_4_1_t01,
		Rule_19005_3_6_2_11_4_1_t02,
		Rule_19005_3_6_2_11_4_2_t01,
		Rule_19005_3_6_2_11_4_2_t02,
		Rule_19005_3_6_2_11_5_t01,
		Rule_19005_3_6_2_11_6_t01,
		Rule_19005_3_6_2_11_6_t02,
		Rule_19005_3_6_2_11_6_t03,
		Rule_19005_3_6_2_11_6_t04,
		Rule_19005_3_6_2_11_7_2_t01,
		Rule_19005_3_6_2_11_7_2_t02,
		Rule_19005_3_6_2_11_8_t01,
		Rule_19005_3_6_3_1_t01,
		Rule_19005_3_6_3_2_t01,
		Rule_19005_3_6_3_2_t02,
		Rule_19005_3_6_3_3_t01,
		Rule_19005_3_6_3_3_t02,
		Rule_19005_3_6_3_3_t03,
		Rule_19005_3_6_3_3_t04,
		Rule_19005_3_6_4_1_t01,
		Rule_19005_3_6_4_1_t02,
		Rule_19005_3_6_4_1_t03,
		Rule_19005_3_6_4_2_t01,
		Rule_19005_3_6_4_2_t02,
		Rule_19005_3_6_4_3_t01,
		Rule_19005_3_6_4_3_t02,
		Rule_19005_3_6_4_3_t03,
		Rule_19005_3_6_5_1_t01,
		Rule_19005_3_6_5_1_t02,
		Rule_19005_3_6_5_2_t01,
		Rule_19005_3_6_5_2_t02,
		Rule_19005_3_6_6_2_1_t01,
		Rule_19005_3_6_6_2_1_t02,
		Rule_19005_3_6_6_2_1_t03,
		Rule_19005_3_6_6_2_1_t04,
		Rule_19005_3_6_6_2_1_t05,
		Rule_19005_3_6_6_2_3_1_t01,
		Rule_19005_3_6_6_2_3_1_t02,
		Rule_19005_3_6_6_2_3_2_t01,
		Rule_19005_3_6_6_2_3_3_t01,
		Rule_19005_3_6_6_2_3_3_t02,
		Rule_19005_3_6_6_2_3_3_t03,
		Rule_19005_3_6_6_2_3_3_t04,
		Rule_19005_3_6_6_2_3_3_t05,
		Rule_19005_3_6_6_2_3_3_t06,
		Rule_19005_3_6_6_2_3_3_t07,
		Rule_19005_3_6_6_2_3_3_t08,
		Rule_19005_3_6_6_2_3_3_t09,
		Rule_19005_3_6_6_2_3_3_t10,
		Rule_19005_3_6_6_2_3_3_t11,
		Rule_19005_3_6_6_2_3_3_t12,
		Rule_19005_3_6_6_2_3_3_t13,
		Rule_19005_3_6_6_2_3_3_t14,
		Rule_19005_3_6_6_2_3_3_t15,
		Rule_19005_3_6_6_2_3_3_t16,
		Rule_19005_3_6_6_2_3_3_t17,
		Rule_19005_3_6_6_2_3_3_t18,
		Rule_19005_3_6_6_4_t01,
		Rule_19005_3_6_6_4_t02,
		Rule_19005_3_6_6_4_t03,
		Rule_19005_3_6_6_4_t04,
		Rule_19005_3_6_6_4_t05,
		Rule_19005_3_6_6_4_t06,
		Rule_19005_3_6_6_4_t07,
		Rule_19005_3_6_8_t01,
		Rule_19005_3_6_8_t02,
		Rule_19005_3_6_8_t03,
		Rule_19005_3_6_8_t04,
		Rule_19005_3_6_9_t01,
		Rule_19005_3_6_9_t02,
		Rule_19005_3_6_9_t03,
		Rule_19005_3_6_9_t04,
		Rule_19005_3_6_10_t01,
		Rule_19005_3_6_10_t02,
		Rule_19005_3_6_11_t01,
	}
}

// PDFA4Rules returns the PDF/A-4 rule set (ISO 19005-4:2020, 109 rules).
func PDFA4Rules() []Rule {
	return []Rule{
		Rule_19005_4_6_1_2_t01,
		Rule_19005_4_6_1_2_t02,
		Rule_19005_4_6_1_3_t01,
		Rule_19005_4_6_1_3_t02,
		Rule_19005_4_6_1_3_t03,
		Rule_19005_4_6_1_3_t04,
		Rule_19005_4_6_1_3_t05,
		Rule_19005_4_6_1_4_t01,
		Rule_19005_4_6_1_5_t01,
		Rule_19005_4_6_1_5_t02,
		Rule_19005_4_6_1_6_1_t01,
		Rule_19005_4_6_1_6_1_t02,
		Rule_19005_4_6_1_6_2_t01,
		Rule_19005_4_6_1_7_t01,
		Rule_19005_4_6_1_8_t01,
		Rule_19005_4_6_1_9_t01,
		Rule_19005_4_6_1_11_t01,
		Rule_19005_4_6_1_12_t01,
		Rule_19005_4_6_2_2_t01,
		Rule_19005_4_6_2_2_t02,
		Rule_19005_4_6_2_2_t03,
		Rule_19005_4_6_2_3_t01,
		Rule_19005_4_6_2_3_t02,
		Rule_19005_4_6_2_3_t03,
		Rule_19005_4_6_2_4_2_t01,
		Rule_19005_4_6_2_4_2_t02,
		Rule_19005_4_6_2_4_2_t03,
		Rule_19005_4_6_2_4_3_t02,
		Rule_19005_4_6_2_4_3_t03,
		Rule_19005_4_6_2_4_3_t04,
		Rule_19005_4_6_2_4_4_t01,
		Rule_19005_4_6_2_4_4_t02,
		Rule_19005_4_6_2_5_t01,
		Rule_19005_4_6_2_5_t02,
		Rule_19005_4_6_2_5_t03,
		Rule_19005_4_6_2_5_t04,
		Rule_19005_4_6_2_5_t05,
		Rule_19005_4_6_2_5_t06,
		Rule_19005_4_6_2_7_1_t01,
		Rule_19005_4_6_2_7_1_t02,
		Rule_19005_4_6_2_7_1_t03,
		Rule_19005_4_6_2_7_1_t04,
		Rule_19005_4_6_2_7_1_t05,
		Rule_19005_4_6_2_7_3_t01,
		Rule_19005_4_6_2_7_3_t02,
		Rule_19005_4_6_2_7_3_t03,
		Rule_19005_4_6_2_7_3_t04,
		Rule_19005_4_6_2_7_3_t05,
		Rule_19005_4_6_2_8_1_t01,
		Rule_19005_4_6_2_8_2_t01,
		Rule_19005_4_6_2_9_t01,
		Rule_19005_4_6_2_9_t02,
		Rule_19005_4_6_2_10_2_t01,
		Rule_19005_4_6_2_10_2_t02,
		Rule_19005_4_6_2_10_2_t03,
		Rule_19005_4_6_2_10_2_t04,
		Rule_19005_4_6_2_10_2_t05,
		Rule_19005_4_6_2_10_2_t06,
		Rule_19005_4_6_2_10_2_t07,
		Rule_19005_4_6_2_10_3_1_t01,
		Rule_19005_4_6_2_10_3_2_t01,
		Rule_19005_4_6_2_10_3_3_t01,
		Rule_19005_4_6_2_10_3_3_t02,
		Rule_19005_4_6_2_10_3_3_t03,
		Rule_19005_4_6_2_10_4_1_t01,
		Rule_19005_4_6_2_10_4_1_t02,
		Rule_19005_4_6_2_10_5_t01,
		Rule_19005_4_6_2_10_6_t01,
		Rule_19005_4_6_2_10_6_t02,
		Rule_19005_4_6_2_10_6_t03,
		Rule_19005_4_6_2_10_6_t04,
		Rule_19005_4_6_2_10_7_t01,
		Rule_19005_4_6_2_10_8_t01,
		Rule_19005_4_6_2_10_9_t01,
		Rule_19005_4_6_3_1_t01,
		Rule_19005_4_6_3_2_t01,
		Rule_19005_4_6_3_2_t02,
		Rule_19005_4_6_3_3_t01,
		Rule_19005_4_6_3_3_t02,
		Rule_19005_4_6_3_3_t03,
		Rule_19005_4_6_3_3_t04,
		Rule_19005_4_6_4_1_t01,
		Rule_19005_4_6_4_1_t02,
		Rule_19005_4_6_4_2_t01,
		Rule_19005_4_6_4_2_t02,
		Rule_19005_4_6_6_1_t01,
		Rule_19005_4_6_6_1_t02,
		Rule_19005_4_6_6_3_t01,
		Rule_19005_4_6_7_2_1_t01,
		Rule_19005_4_6_7_2_1_t02,
		Rule_19005_4_6_7_2_1_t03,
		Rule_19005_4_6_7_2_1_t04,
		Rule_19005_4_6_7_2_1_t05,
		Rule_19005_4_6_7_3_t01,
		Rule_19005_4_6_7_3_t02,
		Rule_19005_4_6_7_3_t03,
		Rule_19005_4_6_7_3_t04,
		Rule_19005_4_6_7_3_t05,
		Rule_19005_4_6_7_3_t06,
		Rule_19005_4_6_7_3_t07,
		Rule_19005_4_6_7_3_t08,
		Rule_19005_4_6_9_t01,
		Rule_19005_4_6_9_t02,
		Rule_19005_4_6_9_t03,
		Rule_19005_4_6_9_t04,
		Rule_19005_4_6_10_t01,
		Rule_19005_4_6_10_t02,
		Rule_19005_4_6_10_t03,
		Rule_19005_4_6_11_t01,
		Rule_19005_4_6_11_t02,
		Rule_19005_4_6_12_t01,
	}
}

// PDFA4eRules returns the PDF/A-4e rule set (ISO 19005-4:2020, Level E, 109 rules).
func PDFA4eRules() []Rule {
	return []Rule{
		Rule_19005_4_6_1_2_t01,
		Rule_19005_4_6_1_2_t02,
		Rule_19005_4_6_1_3_t01,
		Rule_19005_4_6_1_3_t02,
		Rule_19005_4_6_1_3_t03,
		Rule_19005_4_6_1_3_t04,
		Rule_19005_4_6_1_3_t05,
		Rule_19005_4_6_1_4_t01,
		Rule_19005_4_6_1_5_t01,
		Rule_19005_4_6_1_5_t02,
		Rule_19005_4_6_1_6_1_t01,
		Rule_19005_4_6_1_6_1_t02,
		Rule_19005_4_6_1_6_1_t03,
		Rule_19005_4_6_1_6_2_t01,
		Rule_19005_4_6_1_7_t01,
		Rule_19005_4_6_1_8_t01,
		Rule_19005_4_6_1_9_t01,
		Rule_19005_4_6_1_11_t01,
		Rule_19005_4_6_1_12_t01,
		Rule_19005_4_6_2_2_t01,
		Rule_19005_4_6_2_2_t02,
		Rule_19005_4_6_2_2_t03,
		Rule_19005_4_6_2_3_t01,
		Rule_19005_4_6_2_3_t02,
		Rule_19005_4_6_2_3_t03,
		Rule_19005_4_6_2_4_2_t01,
		Rule_19005_4_6_2_4_2_t02,
		Rule_19005_4_6_2_4_2_t03,
		Rule_19005_4_6_2_4_3_t02,
		Rule_19005_4_6_2_4_3_t03,
		Rule_19005_4_6_2_4_3_t04,
		Rule_19005_4_6_2_4_4_t01,
		Rule_19005_4_6_2_4_4_t02,
		Rule_19005_4_6_2_5_t01,
		Rule_19005_4_6_2_5_t02,
		Rule_19005_4_6_2_5_t03,
		Rule_19005_4_6_2_5_t04,
		Rule_19005_4_6_2_5_t05,
		Rule_19005_4_6_2_5_t06,
		Rule_19005_4_6_2_7_1_t01,
		Rule_19005_4_6_2_7_1_t02,
		Rule_19005_4_6_2_7_1_t03,
		Rule_19005_4_6_2_7_1_t04,
		Rule_19005_4_6_2_7_1_t05,
		Rule_19005_4_6_2_7_3_t01,
		Rule_19005_4_6_2_7_3_t02,
		Rule_19005_4_6_2_7_3_t03,
		Rule_19005_4_6_2_7_3_t04,
		Rule_19005_4_6_2_7_3_t05,
		Rule_19005_4_6_2_8_1_t01,
		Rule_19005_4_6_2_8_2_t01,
		Rule_19005_4_6_2_9_t01,
		Rule_19005_4_6_2_9_t02,
		Rule_19005_4_6_2_10_2_t01,
		Rule_19005_4_6_2_10_2_t02,
		Rule_19005_4_6_2_10_2_t03,
		Rule_19005_4_6_2_10_2_t04,
		Rule_19005_4_6_2_10_2_t05,
		Rule_19005_4_6_2_10_2_t06,
		Rule_19005_4_6_2_10_2_t07,
		Rule_19005_4_6_2_10_3_1_t01,
		Rule_19005_4_6_2_10_3_2_t01,
		Rule_19005_4_6_2_10_3_3_t01,
		Rule_19005_4_6_2_10_3_3_t02,
		Rule_19005_4_6_2_10_3_3_t03,
		Rule_19005_4_6_2_10_4_1_t01,
		Rule_19005_4_6_2_10_4_1_t02,
		Rule_19005_4_6_2_10_5_t01,
		Rule_19005_4_6_2_10_6_t01,
		Rule_19005_4_6_2_10_6_t02,
		Rule_19005_4_6_2_10_6_t03,
		Rule_19005_4_6_2_10_6_t04,
		Rule_19005_4_6_2_10_7_t01,
		Rule_19005_4_6_2_10_8_t01,
		Rule_19005_4_6_2_10_9_t01,
		Rule_19005_4_6_3_1_t01,
		Rule_19005_4_6_3_2_t01,
		Rule_19005_4_6_3_2_t02,
		Rule_19005_4_6_3_3_t01,
		Rule_19005_4_6_3_3_t02,
		Rule_19005_4_6_3_3_t03,
		Rule_19005_4_6_3_3_t04,
		Rule_19005_4_6_4_1_t01,
		Rule_19005_4_6_4_1_t02,
		Rule_19005_4_6_4_2_t01,
		Rule_19005_4_6_4_2_t02,
		Rule_19005_4_6_6_1_t01,
		Rule_19005_4_6_6_1_t02,
		Rule_19005_4_6_6_3_t01,
		Rule_19005_4_6_7_2_1_t01,
		Rule_19005_4_6_7_2_1_t02,
		Rule_19005_4_6_7_2_1_t03,
		Rule_19005_4_6_7_2_1_t04,
		Rule_19005_4_6_7_2_1_t05,
		Rule_19005_4_6_7_3_t01,
		Rule_19005_4_6_7_3_t02,
		Rule_19005_4_6_7_3_t03,
		Rule_19005_4_6_7_3_t04,
		Rule_19005_4_6_7_3_t05,
		Rule_19005_4_6_7_3_t06,
		Rule_19005_4_6_7_3_t07,
		Rule_19005_4_6_7_3_t08,
		Rule_19005_4_6_9_t01,
		Rule_19005_4_6_9_t02,
		Rule_19005_4_6_9_t04,
		Rule_19005_4_6_10_t01,
		Rule_19005_4_6_10_t02,
		Rule_19005_4_6_10_t03,
		Rule_19005_4_6_11_t01,
		Rule_19005_4_6_11_t02,
		Rule_19005_4_6_12_t01,
	}
}

// PDFA4fRules returns the PDF/A-4f rule set (ISO 19005-4:2020, Level F, 109 rules).
func PDFA4fRules() []Rule {
	return []Rule{
		Rule_19005_4_6_1_2_t01,
		Rule_19005_4_6_1_2_t02,
		Rule_19005_4_6_1_3_t01,
		Rule_19005_4_6_1_3_t02,
		Rule_19005_4_6_1_3_t03,
		Rule_19005_4_6_1_3_t04,
		Rule_19005_4_6_1_3_t05,
		Rule_19005_4_6_1_4_t01,
		Rule_19005_4_6_1_5_t01,
		Rule_19005_4_6_1_5_t02,
		Rule_19005_4_6_1_6_1_t01,
		Rule_19005_4_6_1_6_1_t02,
		Rule_19005_4_6_1_6_2_t01,
		Rule_19005_4_6_1_7_t01,
		Rule_19005_4_6_1_8_t01,
		Rule_19005_4_6_1_9_t01,
		Rule_19005_4_6_1_11_t01,
		Rule_19005_4_6_1_12_t01,
		Rule_19005_4_6_2_2_t01,
		Rule_19005_4_6_2_2_t02,
		Rule_19005_4_6_2_2_t03,
		Rule_19005_4_6_2_3_t01,
		Rule_19005_4_6_2_3_t02,
		Rule_19005_4_6_2_3_t03,
		Rule_19005_4_6_2_4_2_t01,
		Rule_19005_4_6_2_4_2_t02,
		Rule_19005_4_6_2_4_2_t03,
		Rule_19005_4_6_2_4_3_t02,
		Rule_19005_4_6_2_4_3_t03,
		Rule_19005_4_6_2_4_3_t04,
		Rule_19005_4_6_2_4_4_t01,
		Rule_19005_4_6_2_4_4_t02,
		Rule_19005_4_6_2_5_t01,
		Rule_19005_4_6_2_5_t02,
		Rule_19005_4_6_2_5_t03,
		Rule_19005_4_6_2_5_t04,
		Rule_19005_4_6_2_5_t05,
		Rule_19005_4_6_2_5_t06,
		Rule_19005_4_6_2_7_1_t01,
		Rule_19005_4_6_2_7_1_t02,
		Rule_19005_4_6_2_7_1_t03,
		Rule_19005_4_6_2_7_1_t04,
		Rule_19005_4_6_2_7_1_t05,
		Rule_19005_4_6_2_7_3_t01,
		Rule_19005_4_6_2_7_3_t02,
		Rule_19005_4_6_2_7_3_t03,
		Rule_19005_4_6_2_7_3_t04,
		Rule_19005_4_6_2_7_3_t05,
		Rule_19005_4_6_2_8_1_t01,
		Rule_19005_4_6_2_8_2_t01,
		Rule_19005_4_6_2_9_t01,
		Rule_19005_4_6_2_9_t02,
		Rule_19005_4_6_2_10_2_t01,
		Rule_19005_4_6_2_10_2_t02,
		Rule_19005_4_6_2_10_2_t03,
		Rule_19005_4_6_2_10_2_t04,
		Rule_19005_4_6_2_10_2_t05,
		Rule_19005_4_6_2_10_2_t06,
		Rule_19005_4_6_2_10_2_t07,
		Rule_19005_4_6_2_10_3_1_t01,
		Rule_19005_4_6_2_10_3_2_t01,
		Rule_19005_4_6_2_10_3_3_t01,
		Rule_19005_4_6_2_10_3_3_t02,
		Rule_19005_4_6_2_10_3_3_t03,
		Rule_19005_4_6_2_10_4_1_t01,
		Rule_19005_4_6_2_10_4_1_t02,
		Rule_19005_4_6_2_10_5_t01,
		Rule_19005_4_6_2_10_6_t01,
		Rule_19005_4_6_2_10_6_t02,
		Rule_19005_4_6_2_10_6_t03,
		Rule_19005_4_6_2_10_6_t04,
		Rule_19005_4_6_2_10_7_t01,
		Rule_19005_4_6_2_10_8_t01,
		Rule_19005_4_6_2_10_9_t01,
		Rule_19005_4_6_3_1_t01,
		Rule_19005_4_6_3_2_t01,
		Rule_19005_4_6_3_2_t02,
		Rule_19005_4_6_3_3_t01,
		Rule_19005_4_6_3_3_t02,
		Rule_19005_4_6_3_3_t03,
		Rule_19005_4_6_3_3_t04,
		Rule_19005_4_6_4_1_t01,
		Rule_19005_4_6_4_1_t02,
		Rule_19005_4_6_4_2_t01,
		Rule_19005_4_6_4_2_t02,
		Rule_19005_4_6_6_1_t01,
		Rule_19005_4_6_6_1_t02,
		Rule_19005_4_6_6_3_t01,
		Rule_19005_4_6_7_2_1_t01,
		Rule_19005_4_6_7_2_1_t02,
		Rule_19005_4_6_7_2_1_t03,
		Rule_19005_4_6_7_2_1_t04,
		Rule_19005_4_6_7_2_1_t05,
		Rule_19005_4_6_7_3_t01,
		Rule_19005_4_6_7_3_t02,
		Rule_19005_4_6_7_3_t03,
		Rule_19005_4_6_7_3_t04,
		Rule_19005_4_6_7_3_t05,
		Rule_19005_4_6_7_3_t06,
		Rule_19005_4_6_7_3_t07,
		Rule_19005_4_6_7_3_t08,
		Rule_19005_4_6_9_t01,
		Rule_19005_4_6_9_t02,
		Rule_19005_4_6_9_t04,
		Rule_19005_4_6_9_t05,
		Rule_19005_4_6_10_t01,
		Rule_19005_4_6_10_t02,
		Rule_19005_4_6_10_t03,
		Rule_19005_4_6_11_t01,
		Rule_19005_4_6_11_t02,
		Rule_19005_4_6_12_t01,
	}
}

// PDFUA1Rules returns the PDF/UA-1 rule set (ISO 14289-1:2014, 106 rules).
func PDFUA1Rules() []Rule {
	return []Rule{
		Rule_14289_1_5_t01,
		Rule_14289_1_5_t02,
		Rule_14289_1_5_t03,
		Rule_14289_1_5_t04,
		Rule_14289_1_5_t05,
		Rule_14289_1_6_1_t01,
		Rule_14289_1_6_2_t01,
		Rule_14289_1_7_1_t01,
		Rule_14289_1_7_1_t02,
		Rule_14289_1_7_1_t03,
		Rule_14289_1_7_1_t04,
		Rule_14289_1_7_1_t05,
		Rule_14289_1_7_1_t06,
		Rule_14289_1_7_1_t07,
		Rule_14289_1_7_1_t08,
		Rule_14289_1_7_1_t09,
		Rule_14289_1_7_1_t10,
		Rule_14289_1_7_1_t11,
		Rule_14289_1_7_1_t12,
		Rule_14289_1_7_2_t02,
		Rule_14289_1_7_2_t03,
		Rule_14289_1_7_2_t04,
		Rule_14289_1_7_2_t05,
		Rule_14289_1_7_2_t06,
		Rule_14289_1_7_2_t07,
		Rule_14289_1_7_2_t08,
		Rule_14289_1_7_2_t09,
		Rule_14289_1_7_2_t10,
		Rule_14289_1_7_2_t11,
		Rule_14289_1_7_2_t12,
		Rule_14289_1_7_2_t13,
		Rule_14289_1_7_2_t14,
		Rule_14289_1_7_2_t15,
		Rule_14289_1_7_2_t16,
		Rule_14289_1_7_2_t17,
		Rule_14289_1_7_2_t18,
		Rule_14289_1_7_2_t19,
		Rule_14289_1_7_2_t20,
		Rule_14289_1_7_2_t21,
		Rule_14289_1_7_2_t22,
		Rule_14289_1_7_2_t23,
		Rule_14289_1_7_2_t24,
		Rule_14289_1_7_2_t25,
		Rule_14289_1_7_2_t26,
		Rule_14289_1_7_2_t27,
		Rule_14289_1_7_2_t28,
		Rule_14289_1_7_2_t29,
		Rule_14289_1_7_2_t30,
		Rule_14289_1_7_2_t31,
		Rule_14289_1_7_2_t32,
		Rule_14289_1_7_2_t33,
		Rule_14289_1_7_2_t34,
		Rule_14289_1_7_2_t36,
		Rule_14289_1_7_2_t37,
		Rule_14289_1_7_2_t38,
		Rule_14289_1_7_2_t39,
		Rule_14289_1_7_2_t40,
		Rule_14289_1_7_2_t41,
		Rule_14289_1_7_2_t42,
		Rule_14289_1_7_2_t43,
		Rule_14289_1_7_3_t01,
		Rule_14289_1_7_4_2_t01,
		Rule_14289_1_7_4_4_t01,
		Rule_14289_1_7_4_4_t02,
		Rule_14289_1_7_4_4_t03,
		Rule_14289_1_7_5_t01,
		Rule_14289_1_7_5_t02,
		Rule_14289_1_7_7_t01,
		Rule_14289_1_7_9_t01,
		Rule_14289_1_7_9_t02,
		Rule_14289_1_7_10_t01,
		Rule_14289_1_7_10_t02,
		Rule_14289_1_7_11_t01,
		Rule_14289_1_7_15_t01,
		Rule_14289_1_7_16_t01,
		Rule_14289_1_7_18_1_t01,
		Rule_14289_1_7_18_1_t02,
		Rule_14289_1_7_18_1_t03,
		Rule_14289_1_7_18_2_t01,
		Rule_14289_1_7_18_3_t01,
		Rule_14289_1_7_18_4_t01,
		Rule_14289_1_7_18_4_t02,
		Rule_14289_1_7_18_5_t01,
		Rule_14289_1_7_18_5_t02,
		Rule_14289_1_7_18_6_2_t01,
		Rule_14289_1_7_18_6_2_t02,
		Rule_14289_1_7_18_8_t01,
		Rule_14289_1_7_20_t01,
		Rule_14289_1_7_20_t02,
		Rule_14289_1_7_21_3_1_t01,
		Rule_14289_1_7_21_3_2_t01,
		Rule_14289_1_7_21_3_3_t01,
		Rule_14289_1_7_21_3_3_t02,
		Rule_14289_1_7_21_3_3_t03,
		Rule_14289_1_7_21_4_1_t01,
		Rule_14289_1_7_21_4_1_t02,
		Rule_14289_1_7_21_4_2_t01,
		Rule_14289_1_7_21_4_2_t02,
		Rule_14289_1_7_21_5_t01,
		Rule_14289_1_7_21_6_t01,
		Rule_14289_1_7_21_6_t02,
		Rule_14289_1_7_21_6_t03,
		Rule_14289_1_7_21_6_t04,
		Rule_14289_1_7_21_7_t01,
		Rule_14289_1_7_21_7_t02,
		Rule_14289_1_7_21_8_t01,
	}
}

// PDFUA2Rules returns the PDF/UA-2 rule set (ISO 14289-2:2024, 91 rules).
func PDFUA2Rules() []Rule {
	return []Rule{
		Rule_14289_2_5_t01,
		Rule_14289_2_5_t02,
		Rule_14289_2_5_t03,
		Rule_14289_2_5_t04,
		Rule_14289_2_5_t05,
		Rule_14289_2_6_2_t01,
		Rule_14289_2_8_2_1_t01,
		Rule_14289_2_8_2_1_t02,
		Rule_14289_2_8_2_2_t01,
		Rule_14289_2_8_2_4_t01,
		Rule_14289_2_8_2_4_t02,
		Rule_14289_2_8_2_4_t03,
		Rule_14289_2_8_2_4_t04,
		Rule_14289_2_8_2_5_2_t01,
		Rule_14289_2_8_2_5_2_t02,
		Rule_14289_2_8_2_5_8_t01,
		Rule_14289_2_8_2_5_12_t01,
		Rule_14289_2_8_2_5_14_t01,
		Rule_14289_2_8_2_5_14_t02,
		Rule_14289_2_8_2_5_14_t03,
		Rule_14289_2_8_2_5_14_t04,
		Rule_14289_2_8_2_5_20_t01,
		Rule_14289_2_8_2_5_20_t02,
		Rule_14289_2_8_2_5_23_t01,
		Rule_14289_2_8_2_5_24_t01,
		Rule_14289_2_8_2_5_25_t01,
		Rule_14289_2_8_2_5_25_t02,
		Rule_14289_2_8_2_5_26_t01,
		Rule_14289_2_8_2_5_26_t02,
		Rule_14289_2_8_2_5_26_t03,
		Rule_14289_2_8_2_5_26_t04,
		Rule_14289_2_8_2_5_26_t05,
		Rule_14289_2_8_2_5_26_t06,
		Rule_14289_2_8_2_5_27_t01,
		Rule_14289_2_8_2_5_28_2_t01,
		Rule_14289_2_8_2_5_29_t01,
		Rule_14289_2_8_4_3_t01,
		Rule_14289_2_8_4_3_t02,
		Rule_14289_2_8_4_3_t03,
		Rule_14289_2_8_4_4_t01,
		Rule_14289_2_8_4_4_t02,
		Rule_14289_2_8_4_5_3_1_t01,
		Rule_14289_2_8_4_5_3_2_t01,
		Rule_14289_2_8_4_5_4_t01,
		Rule_14289_2_8_4_5_4_t02,
		Rule_14289_2_8_4_5_4_t03,
		Rule_14289_2_8_4_5_5_1_t01,
		Rule_14289_2_8_4_5_5_1_t02,
		Rule_14289_2_8_4_5_6_t01,
		Rule_14289_2_8_4_5_7_t01,
		Rule_14289_2_8_4_5_7_t02,
		Rule_14289_2_8_4_5_7_t03,
		Rule_14289_2_8_4_5_7_t04,
		Rule_14289_2_8_4_5_8_t01,
		Rule_14289_2_8_4_5_8_t02,
		Rule_14289_2_8_4_5_9_t01,
		Rule_14289_2_8_6_t01,
		Rule_14289_2_8_7_t01,
		Rule_14289_2_8_7_t02,
		Rule_14289_2_8_8_t01,
		Rule_14289_2_8_8_t02,
		Rule_14289_2_8_9_2_2_t01,
		Rule_14289_2_8_9_2_2_t02,
		Rule_14289_2_8_9_2_3_t01,
		Rule_14289_2_8_9_2_3_t02,
		Rule_14289_2_8_9_2_4_7_t01,
		Rule_14289_2_8_9_2_4_8_t01,
		Rule_14289_2_8_9_2_4_9_t01,
		Rule_14289_2_8_9_2_4_10_t01,
		Rule_14289_2_8_9_2_4_11_t01,
		Rule_14289_2_8_9_2_4_11_t02,
		Rule_14289_2_8_9_2_4_12_t01,
		Rule_14289_2_8_9_2_4_13_t01,
		Rule_14289_2_8_9_2_4_14_t01,
		Rule_14289_2_8_9_2_4_15_t01,
		Rule_14289_2_8_9_2_4_16_t01,
		Rule_14289_2_8_9_2_4_19_t01,
		Rule_14289_2_8_9_2_4_19_t02,
		Rule_14289_2_8_9_3_3_t01,
		Rule_14289_2_8_9_4_2_t01,
		Rule_14289_2_8_10_1_t01,
		Rule_14289_2_8_10_1_t02,
		Rule_14289_2_8_10_1_t03,
		Rule_14289_2_8_10_2_3_t01,
		Rule_14289_2_8_10_2_3_t02,
		Rule_14289_2_8_10_3_3_t01,
		Rule_14289_2_8_10_3_5_t01,
		Rule_14289_2_8_11_1_t01,
		Rule_14289_2_8_11_1_t02,
		Rule_14289_2_8_11_2_t01,
		Rule_14289_2_8_14_1_t01,
	}
}
