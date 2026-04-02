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
)
