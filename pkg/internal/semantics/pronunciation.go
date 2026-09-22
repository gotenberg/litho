package semantics

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// maxPronunciationLexiconBytes bounds the decoded PLS payload a document may
// carry across /PronunciationLexicon. Anti-DoS guard, not spec-mandated.
//
// The array names one stream per lexicon and each decodes under its own
// decompression cap, which says nothing about their sum: without a ceiling the
// buffer grows with the number of entries a file names rather than with
// anything a document could legitimately pronounce. Measured on an array of 20
// compressed streams, 328 KB of input decoded to 160 MB.
//
// Unlike maxPDFXPacketBytes this value rests on no measured maximum: no file in
// any corpus carries /PronunciationLexicon at all, so there is nothing to size
// it against. A real lexicon is kilobytes of XML. It matches
// maxRawInfoScanBytes so this package keeps one generous scale rather than a
// third arbitrary number, and even at 10 MB the growth is bounded rather than
// open-ended.
const maxPronunciationLexiconBytes = 10 * 1024 * 1024

// ExtractPronunciationLexicons extracts W3C PLS XML documents from the
// catalog's /PronunciationLexicon array (PDF 2.0).
func ExtractPronunciationLexicons(ctx context.Context, doc *document.Document) ([]models.PronunciationLexicon, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	plObj, ok := catalog.Dict["PronunciationLexicon"]
	if !ok {
		return nil, nil
	}

	plArr, err := document.ResolveArray(ctx, doc, plObj)
	if err != nil {
		return nil, err
	}

	if plArr == nil {
		return nil, nil
	}

	var lexicons []models.PronunciationLexicon
	total := 0

	for _, entry := range plArr {
		stream, err := document.ResolveStream(ctx, doc, entry)
		if err != nil {
			continue
		}

		if stream.Dict == nil {
			continue
		}

		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			continue
		}

		total += len(data)
		if total > maxPronunciationLexiconBytes {
			return nil, fmt.Errorf("pronunciation lexicons exceed %d MB limit", maxPronunciationLexiconBytes>>20)
		}

		xml := string(data)
		if xml != "" {
			lexicons = append(lexicons, models.PronunciationLexicon{XML: xml})
		}
	}

	if len(lexicons) == 0 {
		return nil, nil
	}
	return lexicons, nil
}
