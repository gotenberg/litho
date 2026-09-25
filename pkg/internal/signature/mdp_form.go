package signature

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// withoutFormData returns objects without the definitions that fill in a form
// and that the type-based classifier cannot tell from other changes, and a
// note for MDPInfo when they could not be told apart. policy is 2 or 3.
//
// ISO 32000-1, 12.8.2.2, Table 254 permits "filling in forms" and "signing"
// from P=2 up, and P=3 adds annotation changes. Two kinds of objects serve
// those changes whatever type their producer gives them.
//
// An XFA form keeps the values a user enters in its datasets packet
// ([xfaDatasets]), so a stream an update wrote is form data when the /XFA
// array names it as that packet. The signed revision's array is the one the
// certifier approved, so a stream it names is form data. The current array may
// name another stream, which is form data only when it is new: a stream the
// signed revision already held in another role has not become the form's data
// by being renamed. Nothing else the /XFA array names is exempt, since the
// template and configuration packets define the form rather than fill it in.
//
// Filling in a field or signing redraws a widget (12.5.6.19, 12.7.3.3), and
// its appearance stream draws with XObjects, fonts and the other
// [appearanceTypes]. Such an object is exempt when an annotation the update
// rewrote within policy, a widget at P=2 and any annotation at P=3, reaches it
// from its /AP ([drawnObjects]), and it is new. An object the signed revision
// already held is exempt only when it was that annotation's own appearance
// stream there ([ownAppearances]): a font or an image an appearance shares with
// the page would otherwise let an update redraw the page through it.
//
// An object is new when the signed revision neither defines it nor refers to
// it ([signedRevision.held]): filling in a number the signed document already
// refers to changes what that reference means.
func withoutFormData(
	ctx context.Context,
	doc *document.Document,
	signed *signedRevision,
	objects []appendedObject,
	policy int,
) (_ []appendedObject, note string) {
	var streams, drawn bool
	var annots []int
	for _, obj := range objects {
		if permitted(obj, policy) {
			if obj.IsAnnot {
				annots = append(annots, obj.ObjNum)
			}
			continue
		}
		streams = streams || obj.IsStream
		drawn = drawn || (!obj.IsAnnot && appearanceTypes[obj.Type])
	}
	drawn = drawn && len(annots) > 0
	if !streams && !drawn {
		return objects, ""
	}

	rev, err := signed.revision(ctx)
	if err != nil {
		return objects, fmt.Sprintf("; form data not recognized: %v", err)
	}
	var signedData, currentData, reached, own map[int]bool
	if streams {
		if signedData, err = xfaDatasets(ctx, rev); err != nil {
			return objects, fmt.Sprintf("; form data not recognized: %v", err)
		}
		if currentData, err = xfaDatasets(ctx, doc); err != nil {
			return objects, fmt.Sprintf("; form data not recognized: %v", err)
		}
	}
	if drawn {
		if reached, err = drawnObjects(ctx, doc, annots); err != nil {
			return objects, fmt.Sprintf("; form data not recognized: %v", err)
		}
		own = ownAppearances(ctx, rev, annots)
	}

	kept := make([]appendedObject, 0, len(objects))
	for _, obj := range objects {
		num := obj.ObjNum
		isNew := !signed.held(ctx, rev, num)
		if obj.IsStream && (signedData[num] || (currentData[num] && isNew)) {
			continue
		}
		if !obj.IsAnnot && appearanceTypes[obj.Type] && reached[num] && (isNew || own[num]) {
			continue
		}
		kept = append(kept, obj)
	}
	return kept, ""
}

// permitted reports whether the classifier passes obj at policy 2 or 3.
func permitted(obj appendedObject, policy int) bool {
	switch policy {
	case 2:
		return isAllowedP2(obj)
	case 3:
		return isAllowedP3(obj)
	}
	return false
}

// xfaDatasets returns the object numbers of the streams doc's interactive form
// names as its XFA datasets packet.
//
// ISO 32000-1, 12.7.2, Table 218 lets /XFA be a stream holding the whole XML
// Data Package or an array of text string and stream pairs, one per packet,
// each named by its string (12.7.8). The datasets packet carries the data a
// user fills in, while the others hold the template, the configuration and the
// rest of the form's definition. Only the array form separates the data from
// the definition, so a single /XFA stream names no datasets packet.
func xfaDatasets(ctx context.Context, doc *document.Document) (map[int]bool, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("reading the catalog: %w", err)
	}
	// A form or an /XFA entry that cannot be read names no packet, so every
	// stream an update wrote is then classified by its type.
	form, err := document.ResolveDict(ctx, doc, catalog.Dict["AcroForm"])
	if err != nil {
		doc.Logger.Debug("resolving /AcroForm for the XFA datasets packet", slog.Any("err", err))
		return nil, nil
	}
	if form == nil {
		return nil, nil
	}
	xfaObj, err := document.ResolveObject(ctx, doc, form["XFA"])
	if err != nil {
		doc.Logger.Debug("resolving /XFA for the datasets packet", slog.Any("err", err))
		return nil, nil
	}
	xfa, ok := xfaObj.(ast.PdfArray)
	if !ok {
		return nil, nil
	}
	datasets := make(map[int]bool)
	for i := 0; i+1 < len(xfa); i += 2 {
		name, err := document.ResolveString(ctx, doc, xfa[i])
		if err != nil || document.DecodePdfString(name) != "datasets" {
			continue
		}
		if ref, ok := xfa[i+1].(ast.PdfIndirectRef); ok {
			datasets[ref.ObjectNum] = true
		}
	}
	return datasets, nil
}
