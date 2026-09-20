package remediation

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchInfoDate matches ISO 19005-1 §6.7.3 and ISO 19005-4 §6.7.3
// violations that report /Info CreationDate or ModDate formatted as
// ISO 8601 rather than the required "D:YYYYMMDDHHmmSSOHH'mm'" form.
func matchInfoDate(v compliance.Violation) bool {
	if !clausePrefix(v, "6.7.3") {
		return false
	}
	return (contextContains(v, "/CreationDate=\"") || contextContains(v, "/ModDate=\"")) &&
		!contextContains(v, "/CreationDate=\"D:") && !contextContains(v, "/ModDate=\"D:") ||
		contextContains(v, "malformed")
}

// fixInfoDate rewrites /Info /CreationDate and /Info /ModDate so each
// conforms to the PDF/A-required "D:" prefix encoding. Values already
// in D: form are left unchanged; values in ISO 8601 (commonly
// introduced by authoring tools that mis-flag an XMP date into /Info)
// are converted via [isoToPdfDate].
func fixInfoDate(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		return nil, nil
	}
	// Read through the allocator. trailer-id trims /Info to /ModDate alone for
	// ISO 19005-4 6.1.3 and queues the trimmed dictionary at this same object
	// number, and info-date is the very next registry entry: rebuilding from
	// the on-disk copy below would restore the keys that trim just removed and
	// put forbidden /Info entries back into the output.
	infoDict := liveInfoDict(ctx, alloc, doc)
	if infoDict == nil {
		return nil, nil
	}

	changed := false
	newInfo := make(ast.PdfDict, len(infoDict))
	maps.Copy(newInfo, infoDict)
	fixes := []string{}
	for _, key := range []ast.PdfName{"CreationDate", "ModDate"} {
		raw, ok := infoDict[key]
		if !ok {
			continue
		}
		s, ok := raw.(ast.PdfString)
		if !ok {
			continue
		}
		text := document.DecodePdfString(s)
		if strings.HasPrefix(text, "D:") {
			continue
		}
		converted := isoToPdfDate(text)
		if converted == "" {
			continue
		}
		newInfo[key] = ast.PdfString(converted)
		changed = true
		fixes = append(fixes, fmt.Sprintf("converted /Info /%s to D: format", string(key)))
	}
	if !changed {
		return nil, nil
	}
	if ref, isRef := infoObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newInfo)
	} else {
		doc.Index.Trailer["Info"] = newInfo
	}
	return fixes, nil
}

// isoToPdfDate converts an ISO 8601 string (e.g.
// "2015-03-10T17:19:21+01:00") into the PDF D: encoding required by
// ISO 32000-1 §7.9.4. Returns an empty string on parse failure.
func isoToPdfDate(iso string) string {
	iso = strings.TrimSpace(iso)
	if len(iso) < 10 {
		return ""
	}
	year := iso[0:4]
	if err := parseDigits(year); err != nil {
		return ""
	}
	if iso[4] != '-' || iso[7] != '-' {
		return ""
	}
	month := iso[5:7]
	day := iso[8:10]
	hour, minute, sec := "00", "00", "00"
	tz := "Z"
	if len(iso) >= 19 && iso[10] == 'T' {
		hour = iso[11:13]
		minute = iso[14:16]
		sec = iso[17:19]
		if iso[13] != ':' || iso[16] != ':' {
			return ""
		}
		rest := iso[19:]
		switch {
		case rest == "" || rest == "Z":
			tz = "Z"
		case len(rest) >= 6 && (rest[0] == '+' || rest[0] == '-') && rest[3] == ':':
			tz = fmt.Sprintf("%s%s'%s'", string(rest[0]), rest[1:3], rest[4:6])
		case len(rest) >= 5 && (rest[0] == '+' || rest[0] == '-'):
			tz = fmt.Sprintf("%s%s'%s'", string(rest[0]), rest[1:3], rest[3:5])
		}
	}
	return fmt.Sprintf("D:%s%s%s%s%s%s%s", year, month, day, hour, minute, sec, tz)
}

// parseDigits returns an error when s contains any non-digit byte.
func parseDigits(s string) error {
	for _, r := range s {
		if r < '0' || r > '9' {
			return fmt.Errorf("non-digit %q", r)
		}
	}
	return nil
}
