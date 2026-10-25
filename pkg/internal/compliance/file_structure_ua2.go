package compliance

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// veraPDFDefaultHeaderVersion is the version veraPDF gives a header it cannot
// read a number from (PDFParser.parseHeader, PDF_DEFAULT_VERSION).
const veraPDFDefaultHeaderVersion float32 = 1.4

// headerExactRe is the header form veraPDF reads as it stands
// (PDFParser.parseHeader matches HEADER_PATTERN + "\\d.\\d", whose period is
// any character). A longer or shorter header is cut first.
var headerExactRe = regexp.MustCompile(`^%PDF-\d.\d$`)

// headerVersionWindow is the length veraPDF cuts a header with trailing bytes
// to before it reads the number: "%PDF-" and three characters.
const headerVersionWindow = 8

// checkUA2VersionGroup validates ISO 14289-2, 6.1 t1: the file version shall be
// 2.0, stated in the header or as the catalog /Version, which per ISO 32000-2,
// 7.5.2 replaces the header's when it is later. WTPDF 1.0 states the same test
// at its 6.1, and [ValidateWTPDF] runs this group under the PDF/UA-2 rule, the
// identifier both of veraPDF's WTPDF reports give it.
//
// The version is the one veraPDF's PDDocument.getVersion returns, compared as
// the profile does (pdfVersion == '2.0', the Java float printed): the header's
// number, replaced by the catalog's when the catalog's is not smaller. The
// catalog's is ignored when smaller, so a %PDF-2.9 header fails under a /2.0
// catalog entry and a %PDF-2.0 header passes under /1.7. Both numbers read as
// Java reads a float, "2", "2.00" and "2.f" included, and a header with no
// readable number reads 1.4.
//
// A header later than 2.0 fails whatever the catalog states, and its finding
// says so ([UA2LaterHeaderContext]): remediation repairs it by rewriting the
// header, where an earlier header is raised by a catalog /Version alone.
//
// # Corpus evidence
//
//   - verapdf PDF_A-4__veraPDF_test_suite_6-1-2-t01-pass-a: its %PDF-2.9
//     header fails the test on veraPDF 1.30.3, which added it; 1.30.2 has no
//     such rule.
//   - verapdf: veraPDF 1.30.3 fails it on 1946 of the 2601 files at PDF/UA-2
//     and at both WTPDF levels, and litho on the same 1946.
//   - litho: veraPDF 1.30.3 fails it on 214 of the 257 fixtures at PDF/UA-2,
//     and litho on the same 214.
func checkUA2VersionGroup(ctx context.Context, vctx *validationCtx) []Violation {
	line, ok := readUAHeaderLine(ctx, vctx.doc)
	if !ok {
		return nil
	}
	header := veraPDFHeaderVersion(line)
	version := header
	if catalog, ok := veraPDFCatalogVersion(ctx, vctx.doc, vctx.catalog["Version"]); ok && version <= catalog {
		version = catalog
	}
	if version == 2.0 {
		return nil
	}
	msg := fmt.Sprintf("file version %s is not 2.0 (header %q)", strconv.FormatFloat(float64(version), 'f', -1, 32), string(line))
	// NaN compares false to everything, so it takes this branch too: no
	// catalog entry replaces it.
	if !(header <= 2.0) {
		msg += ": " + UA2LaterHeaderContext
	}
	return fail(Rule_14289_2_6_1_t01, msg)
}

// UA2LaterHeaderContext ends the context of an ISO 14289-2, 6.1 t1 finding
// whose header states a version later than 2.0, which no catalog /Version
// lowers (ISO 32000-2, 7.5.2 lets the entry replace an earlier header only).
// Remediation matches it to repair the header by a full rewrite.
const UA2LaterHeaderContext = "the header states a later version, which no catalog /Version lowers"

// veraPDFHeaderVersion reads the version number of the header line as
// veraPDF's PDFParser.parseHeader does: from the "%PDF-" the line holds, cut to
// [headerVersionWindow] bytes unless it has the exact form, the number is what
// follows the one hyphen. A header too short to hold one, or whose number does
// not parse, reads [veraPDFDefaultHeaderVersion].
func veraPDFHeaderVersion(line []byte) float32 {
	if i := bytes.Index(line, []byte("%PDF-")); i > 0 {
		line = line[i:]
	}
	if bytes.HasPrefix(line, []byte("%PDF-")) && !headerExactRe.Match(line) {
		if len(line) < headerVersionWindow {
			return veraPDFDefaultHeaderVersion
		}
		line = line[:headerVersionWindow]
	}
	parts := strings.Split(string(line), "-")
	if len(parts) != 2 {
		return veraPDFDefaultHeaderVersion
	}
	if v, ok := javaParseFloat(parts[1]); ok {
		return v
	}
	return veraPDFDefaultHeaderVersion
}

// veraPDFCatalogVersion reads the catalog /Version as veraPDF's
// PDCatalog.getFloatVersion does: a name, direct or indirect, whose text
// parses as a Java float. ok is false for any other value, which veraPDF
// ignores.
func veraPDFCatalogVersion(ctx context.Context, doc *document.Document, obj ast.PdfObject) (float32, bool) {
	if obj == nil {
		return 0, false
	}
	name, err := document.ResolveName(ctx, doc, obj)
	if err != nil || name == "" {
		return 0, false
	}
	return javaParseFloat(string(name))
}

// javaParseFloat parses s as Java's Float.parseFloat does for the inputs a
// version can hold: surrounding control characters and spaces trimmed, one
// trailing f, F, d or D type suffix allowed, and the spellings NaN and
// Infinity alone among the names Go would also accept.
func javaParseFloat(s string) (float32, bool) {
	s = strings.TrimFunc(s, func(r rune) bool { return r <= ' ' })
	if n := len(s); n > 1 && strings.IndexByte("fFdD", s[n-1]) >= 0 {
		s = s[:n-1]
	}
	// Go also reads "inf", "infinity" and "nan" in any case, and digits
	// separated by underscores, none of which Java reads.
	if bare := strings.TrimLeft(s, "+-"); bare != "NaN" && bare != "Infinity" && strings.ContainsAny(s, "_iInN") {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0, false
	}
	return float32(v), true
}
