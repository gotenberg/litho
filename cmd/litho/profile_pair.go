package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
)

// profileFlags collects the repeated --profile values of remediate, validate
// and the pipe's remediate stage: one profile, or two that pair (see
// [parseProfiles]).
type profileFlags []string

func (p *profileFlags) String() string { return strings.Join(*p, ", ") }
func (p *profileFlags) Set(val string) error {
	*p = append(*p, val)
	return nil
}

// profileRequest is what a --profile list asks for: one profile, or a PDF/A,
// Factur-X or ZUGFeRD profile paired with the PDF/UA profile of its PDF
// version.
type profileRequest struct {
	// names are the profiles in the order the caller gave them.
	names []string
	// target is the lone profile, or the PDF/A-based one of a pair. Every
	// decision a profile drives but the PDF/UA claim keys on it: packing,
	// encryption, and the remediation method.
	target string
	// pdfua is the PDF/UA profile paired with target, "" for none.
	pdfua string
}

// parseProfiles reads the --profile values names. One name is the target,
// whatever it is: the command refuses a profile it does not know as it did
// before pairs existed. Two names, in either order, must pair (see
// [pairedPDFUA]). Any other list returns an error naming the valid pairs.
func parseProfiles(names []string) (profileRequest, error) {
	switch len(names) {
	case 0:
		return profileRequest{}, errors.New("--profile is required")
	case 1:
		return profileRequest{names: names, target: names[0]}, nil
	case 2:
		target, pdfua := names[0], names[1]
		if pdfuaProfiles[target] != "" {
			target, pdfua = pdfua, target
		}
		if pdfuaProfiles[pdfua] != "" {
			switch paired := pairedPDFUA(target); paired {
			case pdfua:
				return profileRequest{names: names, target: target, pdfua: pdfua}, nil
			case "":
			default:
				return profileRequest{}, fmt.Errorf("--profile %s: %s pairs with %s, not %s; the pairs are %s",
					strings.Join(names, ", "), target, paired, pdfua, validPairs())
			}
		}
	}
	return profileRequest{}, fmt.Errorf("--profile %s: a pair is a PDF/A, Factur-X or ZUGFeRD profile and the PDF/UA profile of its PDF version; the pairs are %s",
		strings.Join(names, ", "), validPairs())
}

// pairedPDFUA returns the PDF/UA profile that pairs with profile, "" for a
// profile that pairs with none. PDF/UA-1 requires a PDF version from 1.0 to
// 1.7 (ISO 14289-1, 6.1), the versions PDF/A-1 to PDF/A-3 build on, Factur-X
// and every ZUGFeRD generation among them on their PDF/A-3 base. PDF/UA-2
// requires PDF 2.0 (ISO 14289-2, 6.1), and PDF/A-4 a 2.x version (ISO 19005-4,
// 6.1.2). The library refuses any other pair with
// [pdf.ErrIncompatibleProfiles].
func pairedPDFUA(profile string) string {
	if _, ok := facturxVariants[profile]; ok {
		return string(models.PDFUA1)
	}
	switch pdfaProfiles[profile] {
	case models.PDFA1a, models.PDFA1b, models.PDFA2a, models.PDFA2b, models.PDFA2u, models.PDFA3a, models.PDFA3b, models.PDFA3u:
		return string(models.PDFUA1)
	case models.PDFA4, models.PDFA4e, models.PDFA4f:
		return string(models.PDFUA2)
	}
	return ""
}

// validPairs lists, for a usage or an error message, the profiles each PDF/UA
// profile pairs with.
func validPairs() string {
	candidates := slices.Concat(sortedKeys(pdfaProfiles), sortedKeys(facturxVariants))
	pairs := make([]string, 0, len(pdfuaProfiles))
	for _, pdfua := range sortedKeys(pdfuaProfiles) {
		var with []string
		for _, profile := range candidates {
			if pairedPDFUA(profile) == pdfua {
				with = append(with, profile)
			}
		}
		pairs = append(pairs, joinOr(with)+" with "+pdfua)
	}
	return strings.Join(pairs, ", and ")
}

// joinOr joins items as "a, b or c".
func joinOr(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}

// isPair reports whether p pairs two profiles.
func (p profileRequest) isPair() bool {
	return p.pdfua != ""
}

// profileOf returns the profile of p a violation of the standard spec, a
// [pdf.Violation] Specification, belongs to. ISO 14289 states the PDF/UA
// requirements, and ISO 32005 the structure rules PDF/UA-2 applies; every
// other standard belongs to the target, the Factur-X rules included.
func (p profileRequest) profileOf(spec string) string {
	if p.isPair() && (strings.HasPrefix(spec, "ISO_14289") || spec == "ISO_32005") {
		return p.pdfua
	}
	return p.target
}

// profileNames is the profile field of the remediate and validate JSON
// reports: one profile as a JSON string, the shape it had before pairs
// existed, and a pair as an array in the order the caller gave it.
type profileNames []string

// MarshalJSON encodes one profile as a JSON string and a pair as an array.
func (p profileNames) MarshalJSON() ([]byte, error) {
	if len(p) < 2 {
		return json.Marshal(strings.Join(p, ""))
	}
	return json.Marshal([]string(p))
}

// UnmarshalJSON decodes a JSON string as one profile and an array as a pair.
func (p *profileNames) UnmarshalJSON(data []byte) error {
	var one string
	if json.Unmarshal(data, &one) == nil {
		*p = profileNames{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("profile: want a string or an array of strings: %w", err)
	}
	*p = many
	return nil
}
