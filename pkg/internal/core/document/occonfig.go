package document

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// OCConfig holds the state of Optional Content Groups (Layers).
type OCConfig struct {
	On        map[int]bool
	Off       map[int]bool
	BaseState string // "ON", "OFF", or "Unchanged"; defaults to "ON" per PDF spec
}

// initOCConfig extracts the Optional Content Properties to determine which layers are ON/OFF.
func (d *Document) initOCConfig(ctx context.Context) {
	d.OCConfig = &OCConfig{
		On:  make(map[int]bool),
		Off: make(map[int]bool),
	}

	// Resolved through ResolveCatalog, which honours the trailer /Root
	// when it resolves to a catalog and recovers by scanning when it does not.
	// Per ISO 32000-1 §7.5.5 Table 15 /Root shall be the catalog, so a file
	// pointing it elsewhere is malformed. Reading the raw /Root left On, Off and
	// BaseState empty on such a file, so every optional-content group fell back
	// to visible and hidden layers leaked into extraction.
	resolved, err := ResolveCatalog(ctx, d)
	if err != nil {
		d.Logger.Debug("resolving catalog for OC config", slog.Any("err", err))
		return
	}
	if resolved == nil || resolved.Dict == nil {
		return
	}
	catalog := resolved.Dict

	ocpObj, ok := catalog["OCProperties"]
	if !ok {
		return
	}
	ocp, err := ResolveDict(ctx, d, ocpObj)
	if err != nil {
		d.Logger.Debug("resolving OCProperties dict", slog.Any("err", err))
	}
	if ocp == nil {
		return
	}

	dObj, ok := ocp["D"]
	if !ok {
		return
	}
	dDict, err := ResolveDict(ctx, d, dObj)
	if err != nil {
		d.Logger.Debug("resolving OC default config dict", slog.Any("err", err))
	}
	if dDict == nil {
		return
	}

	// BaseState determines the default state before applying ON/OFF arrays
	baseState := "ON"
	if bsObj, ok := dDict["BaseState"]; ok {
		if bsName, ok := bsObj.(ast.PdfName); ok {
			baseState = string(bsName)
		}
	}

	// Parse ON array
	if onObj, ok := dDict["ON"]; ok {
		onArr, err := ResolveArray(ctx, d, onObj)
		if err != nil {
			d.Logger.Debug("resolving OC ON array", slog.Any("err", err))
		}
		for _, item := range onArr {
			if ref, ok := item.(ast.PdfIndirectRef); ok {
				d.OCConfig.On[ref.ObjectNum] = true
			}
		}
	}

	// Parse OFF array
	if offObj, ok := dDict["OFF"]; ok {
		offArr, err := ResolveArray(ctx, d, offObj)
		if err != nil {
			d.Logger.Debug("resolving OC OFF array", slog.Any("err", err))
		}
		for _, item := range offArr {
			if ref, ok := item.(ast.PdfIndirectRef); ok {
				d.OCConfig.Off[ref.ObjectNum] = true
			}
		}
	}

	d.OCConfig.BaseState = baseState
}
