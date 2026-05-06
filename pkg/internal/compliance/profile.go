package compliance

import "strings"

// Profile-identifier parsing helpers. These are the single source of truth
// for interpreting profile strings like "PDF/A-2b" or "PDF/UA-1" across
// every litho package that needs to branch on part, conformance level, or
// family. The compliance engine uses them to select rules; the remediation
// fixers use them to generate XMP metadata.

// PDFAPart returns the PDF/A part number (1, 2, 3, or 4) for the given
// profile identifier. Returns 0 if the string is not a recognized PDF/A
// profile.
func PDFAPart(profile string) int {
	switch profile {
	case "PDF/A-1a", "PDF/A-1b":
		return 1
	case "PDF/A-2a", "PDF/A-2b", "PDF/A-2u":
		return 2
	case "PDF/A-3a", "PDF/A-3b", "PDF/A-3u":
		return 3
	case "PDF/A-4", "PDF/A-4e", "PDF/A-4f":
		return 4
	}
	return 0
}

// PDFALevel returns the last byte of the profile identifier, or 0 if the
// string is empty. For PDF/A profiles with an explicit conformance level
// ("PDF/A-2b", "PDF/A-3u", "PDF/A-4e") this is the level character as a
// lowercase ASCII byte. For "PDF/A-4" without an explicit level it returns
// '4', which has no conformance meaning and should be treated as "no level
// present" by callers. Preserving the raw-last-byte behavior keeps the
// compliance engine's existing switch-on-level checks unchanged.
func PDFALevel(profile string) byte {
	if len(profile) == 0 {
		return 0
	}
	return profile[len(profile)-1]
}

// PDFAConformance returns the PDF/A conformance level as an uppercase
// single-character string ("A", "B", "U", "E", "F"), or "" if the profile
// is not a recognized PDF/A identifier. For the bare "PDF/A-4" identifier
// this returns "B" to match the XMP default used by remediation: when no
// explicit level is declared, writers treat the document as conformance B.
func PDFAConformance(profile string) string {
	if PDFAPart(profile) == 0 {
		return ""
	}
	const prefix = "PDF/A-"
	rest := profile[len(prefix):]
	// "PDF/A-4" with no level → default to "B" per the remediation
	// fixer's legacy behavior. The part digit alone is not a valid level.
	if len(rest) < 2 {
		return "B"
	}
	return strings.ToUpper(string(rest[1]))
}

// IsPDFA reports whether the profile identifier names a PDF/A profile.
func IsPDFA(profile string) bool {
	return PDFAPart(profile) != 0
}

// IsPDFUA reports whether the profile identifier names a PDF/UA profile.
func IsPDFUA(profile string) bool {
	return strings.HasPrefix(profile, "PDF/UA")
}

// PDFUAPart returns the PDF/UA part number (1 or 2) for the given profile
// identifier. Returns 0 if the string is not a recognized PDF/UA profile.
func PDFUAPart(profile string) int {
	switch profile {
	case "PDF/UA-1":
		return 1
	case "PDF/UA-2":
		return 2
	}
	return 0
}

// RequiresUnicodeMapping reports whether the profile mandates that
// text content be mapped back to Unicode. Per ISO 19005 the
// requirement keys off the conformance level: Level A (Part 1) and
// Levels U or A (Parts 2/3) require it; Level B never does. Part 4
// always requires it because the spec mandates Unicode mapping for
// every PDF/A-4 conforming document.
//
// Compliance rule selectors that gate on text-extraction (ToUnicode,
// PUA + ActualText, ToUnicode CMap values) consult this helper so a
// "fail under Level A" fixture does not incorrectly fail under
// Level B as well.
func RequiresUnicodeMapping(profile string) bool {
	part := PDFAPart(profile)
	switch part {
	case 0:
		return false
	case 4:
		return true
	}
	lvl := PDFALevel(profile)
	return lvl == 'a' || lvl == 'u'
}

// ConformanceSatisfies reports whether a file whose XMP claims
// pdfaid:conformance = xmpConf satisfies a profile whose level
// (last byte of "PDF/A-Nx") is profileLevel. Per ISO 19005 the
// conformance levels nest: Level A files meet Level U requirements,
// and Level U files meet Level B requirements (A ⊃ U ⊃ B). A
// stricter XMP claim therefore covers a looser validation profile;
// a looser claim does not cover a stricter profile.
//
// xmpConf is matched case-insensitively. Unknown letters (anything
// other than A/B/U) and an empty string fail. profileLevel is matched
// case-insensitively too so callers can pass either the lowercase
// byte returned by [PDFALevel] or an uppercase variant.
func ConformanceSatisfies(xmpConf string, profileLevel byte) bool {
	if len(xmpConf) != 1 {
		return false
	}
	claim := xmpConf[0]
	if claim >= 'a' && claim <= 'z' {
		claim -= 'a' - 'A'
	}
	prof := profileLevel
	if prof >= 'A' && prof <= 'Z' {
		prof += 'a' - 'A'
	}
	switch prof {
	case 'a':
		return claim == 'A'
	case 'u':
		return claim == 'A' || claim == 'U'
	case 'b':
		return claim == 'A' || claim == 'U' || claim == 'B'
	}
	return false
}
