package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// metadataFeature compares document-level metadata (Info dict fields,
// linearization flag, form presence) against the PDF.js reference.
type metadataFeature struct {
	mu      sync.RWMutex
	goldens map[string]*pdfjsReference
}

func newMetadataFeature() *metadataFeature {
	return &metadataFeature{goldens: make(map[string]*pdfjsReference)}
}

func (f *metadataFeature) Name() string         { return "metadata" }
func (f *metadataFeature) GoldenSource() string { return "pdfjs" }

func (f *metadataFeature) LoadGoldens(dir string) error {
	return loadPDFJSReferences(dir, f.goldens, &f.mu)
}

func (f *metadataFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.goldens[fixtureID]
	return ok
}

type metadataOutput struct {
	Info pdfjsRefInfo `json:"info"`
}

func (f *metadataFeature) Run(ctx context.Context, pdfPath, _ string) ([]byte, error) {
	info, err := extractMetadata(ctx, pdfPath)
	if err != nil {
		return nil, err
	}
	return json.Marshal(metadataOutput{Info: info})
}

func (f *metadataFeature) Compare(fixtureID string, output []byte) Verdict {
	f.mu.RLock()
	golden := f.goldens[fixtureID]
	f.mu.RUnlock()

	if golden == nil {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no golden"}
	}

	var got metadataOutput
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}

	mismatches := compareMetadata(golden.File, golden.Metadata.Info, got.Info)
	match := len(mismatches) == 0
	score := 0.0
	if match {
		score = 1.0
	}

	rawMismatches := make([]json.RawMessage, 0, len(mismatches))
	for _, m := range mismatches {
		raw, _ := json.Marshal(m)
		rawMismatches = append(rawMismatches, raw)
	}

	return Verdict{
		FixtureID:  fixtureID,
		Match:      match,
		Score:      score,
		Mismatches: rawMismatches,
	}
}

// metadataMismatch records one field-level disagreement between litho
// and the PDF.js reference.
type metadataMismatch struct {
	File   string `json:"file"`
	Field  string `json:"field"`
	Litho  string `json:"litho"`
	Golden string `json:"golden"`
}

// compareMetadata emits one mismatch per differing field. Date fields go
// through a canonicalising parser so timezone-formatted and raw PDF date
// strings compare equal.
func compareMetadata(file string, golden, litho pdfjsRefInfo) []metadataMismatch {
	var mm []metadataMismatch

	addStr := func(field, lithoVal, goldenVal string) {
		if goldenVal != "" && lithoVal != goldenVal {
			mm = append(mm, metadataMismatch{File: file, Field: field, Litho: lithoVal, Golden: goldenVal})
		}
	}
	addBool := func(field string, lithoVal, goldenVal bool) {
		if lithoVal != goldenVal {
			mm = append(mm, metadataMismatch{
				File: file, Field: field,
				Litho:  fmt.Sprintf("%v", lithoVal),
				Golden: fmt.Sprintf("%v", goldenVal),
			})
		}
	}

	addStr("PDFFormatVersion", litho.PDFFormatVersion, golden.PDFFormatVersion)
	addStr("Title", litho.Title, golden.Title)
	addStr("Author", litho.Author, golden.Author)
	addStr("Creator", litho.Creator, golden.Creator)
	addStr("Producer", litho.Producer, golden.Producer)
	addStr("CreationDate", normalizePDFDate(litho.CreationDate), normalizePDFDate(golden.CreationDate))
	addStr("ModDate", normalizePDFDate(litho.ModDate), normalizePDFDate(golden.ModDate))
	addStr("Keywords", litho.Keywords, golden.Keywords)
	addStr("Language", litho.Language, golden.Language)
	addBool("IsLinearized", litho.IsLinearized, golden.IsLinearized)
	addBool("IsAcroFormPresent", litho.IsAcroFormPresent, golden.IsAcroFormPresent)
	addBool("IsCollectionPresent", litho.IsCollectionPresent, golden.IsCollectionPresent)

	return mm
}

func extractMetadata(ctx context.Context, pdfPath string) (pdfjsRefInfo, error) {
	f, err := os.Open(pdfPath)
	if err != nil {
		return pdfjsRefInfo{}, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return pdfjsRefInfo{}, fmt.Errorf("stat: %w", err)
	}

	r, err := pdf.Open(ctx, f, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return pdfjsRefInfo{}, fmt.Errorf("pdf.Open: %w", err)
	}

	out := pdfjsRefInfo{
		PDFFormatVersion: r.Version(),
		IsLinearized:     r.IsLinearized(),
	}
	if meta, err := r.Metadata(ctx); err == nil {
		out.Author = meta["Author"]
		out.Title = meta["Title"]
		out.Creator = meta["Creator"]
		out.Producer = meta["Producer"]
		out.CreationDate = meta["CreationDate"]
		out.ModDate = meta["ModDate"]
		out.Keywords = meta["Keywords"]
		out.Language = meta["Language"]
	}
	out.IsAcroFormPresent = r.HasAcroForm(ctx)
	if isPortfolio, err := r.IsPortfolio(ctx); err == nil {
		out.IsCollectionPresent = isPortfolio
	}
	return out, nil
}
