package semantics

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractPronunciationLexicons extracts W3C PLS XML documents from the
// catalog's /PronunciationLexicon array (PDF 2.0).
func ExtractPronunciationLexicons(ctx context.Context, doc *document.Document) ([]models.PronunciationLexicon, error) {
	catalog, err := document.GetCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	plObj, ok := catalog.Dict["PronunciationLexicon"]
	if !ok {
		return nil, nil
	}

	plRes, err := document.ResolveObject(ctx, doc, plObj)
	if err != nil {
		return nil, err
	}

	plArr, ok := plRes.(ast.PdfArray)
	if !ok {
		return nil, nil
	}

	var lexicons []models.PronunciationLexicon

	for _, entry := range plArr {
		entryRes, err := document.ResolveObject(ctx, doc, entry)
		if err != nil {
			continue
		}

		stream, ok := entryRes.(ast.PdfStream)
		if !ok {
			continue
		}

		data, err := filter.DecodeStream(stream)
		if err != nil {
			continue
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
