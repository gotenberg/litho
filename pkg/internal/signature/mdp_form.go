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
//
// The current document reaches only the appearances the last update to each
// annotation gave it, so the appearance objects left over are judged in the
// revision the update that wrote them made ([withoutEarlierAppearances]).
// updates are the file offsets where each incremental update after the signed
// revision ends.
func withoutFormData(
	ctx context.Context,
	doc *document.Document,
	signed *signedRevision,
	objects []appendedObject,
	updates []int64,
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
	if drawn {
		return withoutEarlierAppearances(ctx, doc, signed, rev, objects, kept, updates, policy)
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
// names as its XFA datasets packet ([xfaPackets]). The datasets packet carries
// the data a user fills in, while the others hold the template, the
// configuration and the rest of the form's definition.
func xfaDatasets(ctx context.Context, doc *document.Document) (map[int]bool, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("reading the catalog: %w", err)
	}
	packets, err := xfaPackets(ctx, doc, catalog)
	if err != nil {
		return nil, err
	}
	datasets := make(map[int]bool)
	for _, p := range packets {
		if ref, ok := p.value.(ast.PdfIndirectRef); ok && p.name == "datasets" {
			datasets[ref.ObjectNum] = true
		}
	}
	return datasets, nil
}

// xfaPacket is one packet of an XFA array: its name and the stream holding it.
type xfaPacket struct {
	name  string
	value ast.PdfObject
}

// xfaPackets returns the packets of the /XFA array of the interactive form
// catalog names, in order.
//
// ISO 32000-1, 12.7.2, Table 218 lets /XFA be a stream holding the whole XML
// Data Package or an array of text string and stream pairs, one per packet,
// each named by its string (12.7.8). Only the array form separates the packets,
// so a single /XFA stream, like a form or an /XFA entry that cannot be read,
// holds none.
func xfaPackets(ctx context.Context, doc *document.Document, catalog *document.Catalog) ([]xfaPacket, error) {
	form, err := document.ResolveDict(ctx, doc, catalog.Dict["AcroForm"])
	if err != nil {
		doc.Logger.Debug("resolving /AcroForm for the XFA packets", slog.Any("err", err))
		return nil, nil
	}
	if form == nil {
		return nil, nil
	}
	xfaObj, err := document.ResolveObject(ctx, doc, form["XFA"])
	if err != nil {
		doc.Logger.Debug("resolving /XFA for its packets", slog.Any("err", err))
		return nil, ctx.Err()
	}
	xfa, ok := xfaObj.(ast.PdfArray)
	if !ok {
		return nil, nil
	}
	packets := make([]xfaPacket, 0, len(xfa)/2)
	for i := 0; i+1 < len(xfa); i += 2 {
		name, err := document.ResolveString(ctx, doc, xfa[i])
		if err != nil {
			continue
		}
		packets = append(packets, xfaPacket{name: document.DecodePdfString(name), value: xfa[i+1]})
	}
	return packets, nil
}
