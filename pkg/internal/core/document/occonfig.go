package document

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// OCConfig holds the state of Optional Content Groups (Layers)
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

	rootObj, ok := d.Index.Trailer["Root"]
	if !ok {
		return
	}
	catalog, ok := d.resolveAny(ctx, rootObj).(ast.PdfDict)
	if !ok {
		return
	}

	ocpObj, ok := catalog["OCProperties"]
	if !ok {
		return
	}
	ocp, ok := d.resolveAny(ctx, ocpObj).(ast.PdfDict)
	if !ok {
		return
	}

	dObj, ok := ocp["D"]
	if !ok {
		return
	}
	dDict, ok := d.resolveAny(ctx, dObj).(ast.PdfDict)
	if !ok {
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
		if onArr, ok := d.resolveAny(ctx, onObj).(ast.PdfArray); ok {
			for _, item := range onArr {
				if ref, ok := item.(ast.PdfIndirectRef); ok {
					d.OCConfig.On[ref.ObjectNum] = true
				}
			}
		}
	}

	// Parse OFF array
	if offObj, ok := dDict["OFF"]; ok {
		if offArr, ok := d.resolveAny(ctx, offObj).(ast.PdfArray); ok {
			for _, item := range offArr {
				if ref, ok := item.(ast.PdfIndirectRef); ok {
					d.OCConfig.Off[ref.ObjectNum] = true
				}
			}
		}
	}

	d.OCConfig.BaseState = baseState
}
