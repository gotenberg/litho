package compliance

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// xmpCheckDocInfoSync verifies that document info dict entries are consistent
// with their XMP equivalents (§6.7.3).
func xmpCheckDocInfoSync(ctx context.Context, doc *document.Document, xmpData []byte, part int, results *[]Violation) {
	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		return
	}
	infoDict, err := document.ResolveDict(ctx, doc, infoObj)
	if err != nil || infoDict == nil {
		doc.Logger.Debug("compliance.xmp: failed to resolve Info dict", "error", err)
		return
	}

	syncResult := xmpParseForSync(xmpData)
	if syncResult == nil {
		return
	}

	syncPairs := []struct {
		infoKey string
		xmpKey  string
		testNum int
	}{
		{"CreationDate", "xmp:CreateDate", 1},
		{"Title", "dc:title", 2},
		{"Author", "dc:creator", 3},
		{"Subject", "dc:description", 4},
		{"Keywords", "pdf:Keywords", 5},
		{"Creator", "xmp:CreatorTool", 6},
		{"Producer", "pdf:Producer", 7},
		{"ModDate", "xmp:ModifyDate", 8},
	}

	for _, pair := range syncPairs {
		infoVal := xmpGetInfoString(ctx, doc, infoDict, pair.infoKey)
		if infoVal == "" {
			// If the key exists and the value is a non-null indirect
			// reference that can't be resolved as a string, it's a sync
			// violation when XMP has a non-empty value.
			if xmpInfoKeyPresent(infoDict, pair.infoKey) &&
				!xmpInfoValueIsNull(ctx, doc, infoDict, pair.infoKey) {
				xmpVal := syncResult.values[pair.xmpKey]
				if xmpVal != "" {
					*results = append(*results, xmpDocInfoSyncRule(part, pair.testNum, pair.infoKey, "(unresolvable)", pair.xmpKey, xmpVal)...)
				}
			}
			continue
		}

		xmpVal, hasXMP := syncResult.values[pair.xmpKey]

		// If Info dict has a non-empty entry but XMP is missing the
		// corresponding property, that is a sync violation.
		if !hasXMP || xmpVal == "" {
			*results = append(*results, xmpDocInfoSyncRule(part, pair.testNum, pair.infoKey, infoVal, pair.xmpKey, "")...)
			continue
		}

		// §6.7.3 t3 (part 1): dc:creator must contain exactly one entry
		// when /Author is present.
		if part == 1 && pair.infoKey == "Author" {
			if count, ok := syncResult.entryCounts[pair.xmpKey]; ok && count > 1 {
				*results = append(*results, xmpDocInfoSyncRule(part, pair.testNum, pair.infoKey, infoVal, pair.xmpKey,
					fmt.Sprintf("%d entries", count))...)
				continue
			}
		}

		normInfo := infoVal
		normXMP := xmpVal
		// Date fields: Info dict dates must use D: prefix format.
		// A date in non-D: format (e.g., raw ISO 8601) is malformed and
		// considered non-equivalent even if the time value matches.
		isDateField := pair.infoKey == "CreationDate" || pair.infoKey == "ModDate"
		if isDateField && !strings.HasPrefix(normInfo, "D:") && normInfo != "" {
			*results = append(*results, xmpDocInfoSyncRule(part, pair.testNum, pair.infoKey, infoVal, pair.xmpKey, xmpVal)...)
			continue
		}
		// Normalize dates for comparison.
		if strings.HasPrefix(normInfo, "D:") {
			normInfo = xmpNormalizePDFDate(normInfo)
		}
		normInfo = xmpNormalizeISO8601(normInfo)
		normXMP = xmpNormalizeISO8601(normXMP)

		// Non-date text fields compare exactly. The XMP parser already
		// trims element-form text per XML rules and preserves attribute
		// values as-is; the Info dict value is raw. That asymmetry
		// lines up with how VeraPDF reads these pairs. Trimming the
		// Info side hides a real §6.7.3 non-equivalence (trailing
		// whitespace in /Author vs trimmed dc:creator); trimming the
		// XMP side creates false positives when producers use
		// attribute-form properties like pdf:Producer="value ".
		if normInfo != normXMP {
			*results = append(*results, xmpDocInfoSyncRule(part, pair.testNum, pair.infoKey, infoVal, pair.xmpKey, xmpVal)...)
		}
	}
}

func xmpDocInfoSyncRule(part, testNum int, infoKey, infoVal, xmpKey, xmpVal string) []Violation {
	ctx := fmt.Sprintf("/%s=%q vs %s=%q", infoKey, infoVal, xmpKey, xmpVal)
	switch part {
	case 1:
		return xmpDocInfoSyncRulePart1(testNum, ctx)
	case 4:
		// Part 4 uses 6.7.3.3/6.7.3.4 clause numbering. Map test numbers
		// to the appropriate rules.
		return xmpDocInfoSyncRulePart4(testNum, ctx)
	}
	return nil
}

func xmpDocInfoSyncRulePart1(testNum int, ctx string) []Violation {
	switch testNum {
	case 1:
		return fail(Rule_19005_1_6_7_3_t01, ctx)
	case 2:
		return fail(Rule_19005_1_6_7_3_t02, ctx)
	case 3:
		return fail(Rule_19005_1_6_7_3_t03, ctx)
	case 4:
		return fail(Rule_19005_1_6_7_3_t04, ctx)
	case 5:
		return fail(Rule_19005_1_6_7_3_t05, ctx)
	case 6:
		return fail(Rule_19005_1_6_7_3_t06, ctx)
	case 7:
		return fail(Rule_19005_1_6_7_3_t07, ctx)
	case 8:
		return fail(Rule_19005_1_6_7_3_t08, ctx)
	}
	return nil
}

func xmpDocInfoSyncRulePart4(testNum int, ctx string) []Violation {
	// Part 4 uses Rule_19005_4_6_7_3_t07 for value mismatches and t08
	// for missing XMP properties corresponding to Info dict entries.
	// Rule t07 covers all sync mismatches uniformly.
	switch testNum {
	case 1:
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 2:
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 3:
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 4:
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 5:
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 6:
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 7:
		return fail(Rule_19005_4_6_7_3_t07, ctx)
	case 8:
		return fail(Rule_19005_4_6_7_3_t08, ctx)
	}
	return nil
}

// xmpGetInfoString retrieves a string value from the document info dict.
// xmpInfoKeyPresent returns true if the Info dict has the given key, even if
// the value can't be resolved as a string.
func xmpInfoKeyPresent(infoDict ast.PdfDict, key string) bool {
	_, ok := infoDict[ast.PdfName(key)]
	return ok
}

// xmpInfoValueIsNull returns true if the Info dict value for the given key
// resolves to a PDF null object.
func xmpInfoValueIsNull(ctx context.Context, doc *document.Document, infoDict ast.PdfDict, key string) bool {
	valObj, ok := infoDict[ast.PdfName(key)]
	if !ok {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, valObj)
	if err != nil {
		return false
	}
	_, isNull := resolved.(ast.PdfNull)
	return isNull
}

func xmpGetInfoString(ctx context.Context, doc *document.Document, infoDict ast.PdfDict, key string) string {
	valObj, ok := infoDict[ast.PdfName(key)]
	if !ok {
		return ""
	}
	s, err := document.ResolveString(ctx, doc, valObj)
	if err == nil && s != "" {
		return document.DecodePdfString(s)
	}
	return ""
}

// xmpSyncResult holds the parsed XMP values and container entry counts.
type xmpSyncResult struct {
	values      map[string]string // Property key → first value
	entryCounts map[string]int    // Property key → number of rdf:li entries (Seq/Bag only)
}

// xmpParseForSync extracts key XMP property values for DocInfo comparison.
// It handles both inline attribute values and rdf:Alt/rdf:Seq container
// values. For Alt containers, the x-default language entry is preferred;
// if absent, the first entry is used. For Seq containers, the first entry
// is used. Entry counts are tracked for Seq/Bag containers.
func xmpParseForSync(data []byte) *xmpSyncResult {
	result := make(map[string]string)
	entryCounts := make(map[string]int)
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	var propNS, propLocal string
	var inAlt, inSeqOrBag, inLI bool
	var liLang string // xml:lang attribute on current rdf:li
	depth := 0
	inDesc := false

	// simpleRaw accumulates the raw character data of a depth-1 simple
	// property. XMP preserves element text verbatim (whitespace included),
	// and veraPDF compares the un-trimmed value against the Info dict:
	// a pretty-printed "\n   " pdf:Keywords is NOT equivalent to an empty
	// /Keywords (§6.7.3 t5, verified live). Trimming here hid that class.
	var simpleRaw strings.Builder
	simpleHasChild := false

	// altDefaults tracks x-default values from rdf:Alt containers so they
	// can override any earlier first-entry value.
	altDefaults := make(map[string]string)
	// altFirst tracks the first rdf:li value in Alt containers as fallback.
	altFirst := make(map[string]string)

	// nsToPrefix maps namespace URIs to the shorthand prefixes used in sync keys.
	nsToPrefix := map[string]string{
		"http://purl.org/dc/elements/1.1/":   "dc",
		"http://ns.adobe.com/xap/1.0/":       "xmp",
		"http://ns.adobe.com/pdf/1.3/":       "pdf",
		"http://ns.adobe.com/xap/1.0/mm/":    "xmpMM",
		"http://ns.adobe.com/photoshop/1.0/": "photoshop",
	}

	makeKey := func(ns, local string) string {
		if prefix, ok := nsToPrefix[ns]; ok {
			return prefix + ":" + local
		}
		return ns + ":" + local
	}

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" {
				inDesc = true
				depth = 0
				for _, attr := range t.Attr {
					if attr.Name.Space != "" &&
						attr.Name.Space != "xmlns" &&
						attr.Name.Space != xmpRDFNamespace &&
						attr.Name.Space != "http://www.w3.org/XML/1998/namespace" {
						key := makeKey(attr.Name.Space, attr.Name.Local)
						result[key] = attr.Value
					}
				}
				continue
			}
			if inDesc {
				depth++
				if depth == 1 && t.Name.Space != "" && t.Name.Space != xmpRDFNamespace {
					propNS = t.Name.Space
					propLocal = t.Name.Local
					simpleRaw.Reset()
					simpleHasChild = false
				}
				if depth == 2 {
					simpleHasChild = true
				}
				if depth == 2 && t.Name.Space == xmpRDFNamespace {
					switch t.Name.Local {
					case "Alt":
						inAlt = true
					case "Seq", "Bag":
						inSeqOrBag = true
					}
				}
				if (inAlt || inSeqOrBag) && t.Name.Space == xmpRDFNamespace && t.Name.Local == "li" {
					inLI = true
					// Extract xml:lang attribute for Alt container preference.
					liLang = ""
					for _, attr := range t.Attr {
						if attr.Name.Local == "lang" &&
							(attr.Name.Space == "http://www.w3.org/XML/1998/namespace" || attr.Name.Space == "xml") {
							liLang = attr.Value
						}
					}
				}
			}
		case xml.CharData:
			if !inDesc {
				continue
			}
			if depth == 1 && propNS != "" {
				simpleRaw.Write(t)
			}
			raw := string(t)
			// Trimmed form gates structural whitespace (the pretty-print
			// newlines between rdf:Seq and rdf:li). The stored value stays
			// raw: veraPDF reads rdf:li text verbatim, so /Author=" x " is
			// non-equivalent to a trimmed dc:creator "x" yet equivalent to
			// a spaced dc:creator " x " (verified live, §6.7.3 t3). Trimming
			// here would erase whitespace the sync fixer deliberately writes
			// to match /Author, making litho unable to see its own fix.
			if strings.TrimSpace(raw) == "" {
				continue
			}
			if inLI && propNS != "" {
				key := makeKey(propNS, propLocal)
				if inAlt {
					// For Alt containers, track x-default and first entries separately.
					if liLang == "x-default" {
						altDefaults[key] = raw
					}
					if _, exists := altFirst[key]; !exists {
						altFirst[key] = raw
					}
				} else {
					// Seq/Bag: take the first entry; count all entries.
					entryCounts[key]++
					if _, exists := result[key]; !exists {
						result[key] = raw
					}
				}
			}
		case xml.EndElement:
			if t.Name.Space == xmpRDFNamespace && t.Name.Local == "Description" {
				inDesc = false
				depth = 0
				propNS = ""
				propLocal = ""
				inAlt = false
				inSeqOrBag = false
				inLI = false
				liLang = ""
			} else if inDesc {
				if t.Name.Space == xmpRDFNamespace && t.Name.Local == "li" {
					inLI = false
					liLang = ""
				}
				if depth == 2 && t.Name.Space == xmpRDFNamespace {
					switch t.Name.Local {
					case "Alt":
						inAlt = false
					case "Seq", "Bag":
						inSeqOrBag = false
					}
				}
				if depth == 1 {
					if propNS != "" && !simpleHasChild {
						result[makeKey(propNS, propLocal)] = simpleRaw.String()
					}
					propNS = ""
					propLocal = ""
				}
				depth--
			}
		}
	}

	// Merge Alt container values: prefer x-default, fall back to first entry.
	maps.Copy(result, altDefaults)
	for key, val := range altFirst {
		if _, exists := result[key]; !exists {
			result[key] = val
		}
	}

	// Pure-data helper (no *document.Document in scope): use slog.
	slog.Debug("compliance.xmp: sync parsing complete", "values", len(result))
	return &xmpSyncResult{values: result, entryCounts: entryCounts}
}

// xmpNormalizePDFDate converts a PDF date string (D:YYYYMMDDHHmmSS+HH'mm')
// to ISO 8601 format for comparison with XMP dates.
func xmpNormalizePDFDate(pdfDate string) string {
	s := strings.TrimPrefix(pdfDate, "D:")
	if len(s) < 4 {
		return pdfDate
	}

	year := s[:4]
	s = s[4:]

	get2 := func(def string) string {
		if len(s) < 2 {
			return def
		}
		val := s[:2]
		s = s[2:]
		return val
	}

	month := get2("01")
	day := get2("01")
	hour := get2("00")
	min := get2("00")
	sec := get2("00")

	tz := "" // No timezone means "relationship to UT is unknown" per PDF spec.
	s = strings.TrimSpace(s)
	if len(s) > 0 {
		switch s[0] {
		case 'Z':
			tz = "Z"
		case '+', '-':
			sign := string(s[0])
			s = s[1:]
			tzH := "00"
			tzM := "00"
			if len(s) >= 2 {
				tzH = s[:2]
				s = s[2:]
			}
			s = strings.TrimPrefix(s, "'")
			if len(s) >= 2 {
				tzM = s[:2]
			}
			tz = sign + tzH + ":" + tzM
		}
	}

	return year + "-" + month + "-" + day + "T" + hour + ":" + min + ":" + sec + tz
}

// xmpNormalizeISO8601 canonicalizes an ISO 8601 date string for comparison.
// It normalizes +00:00 and -00:00 to Z (all represent UTC), pads HH:MM to
// HH:MM:SS, but preserves the absence of timezone when none is specified.
func xmpNormalizeISO8601(s string) string {
	if !strings.Contains(s, "T") {
		return s
	}

	// Normalize explicit UTC offsets (+00:00 / -00:00) to Z.
	if before, ok := strings.CutSuffix(s, "+00:00"); ok {
		s = before + "Z"
	} else if before, ok := strings.CutSuffix(s, "-00:00"); ok {
		s = before + "Z"
	}
	// Do NOT append Z when no timezone is present. An absent timezone
	// means "unknown" and must not compare equal to explicit UTC.

	tIdx := strings.LastIndex(s, "T")
	timePart := s[tIdx+1:]
	tzSuffix := ""
	for _, tz := range []string{"Z", "+", "-"} {
		if idx := strings.Index(timePart, tz); idx >= 0 {
			tzSuffix = timePart[idx:]
			timePart = timePart[:idx]
			break
		}
	}

	if strings.Count(timePart, ":") == 1 {
		timePart += ":00"
	}

	return s[:tIdx+1] + timePart + tzSuffix
}
