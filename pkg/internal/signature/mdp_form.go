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
// ISO 32000-1, 12.8.2.2, Table 254 permits "filling in forms" from P=2 up. An
// XFA form keeps the values a user enters in its datasets packet
// ([xfaDatasets]), a stream the producer may type as it likes, so a stream an
// update wrote is form data when the /XFA array names it as that packet. The signed revision's
// array is the one the certifier approved, so a stream it names is form data.
// The current array may name another stream, which is form data only when it
// is new: a stream the signed revision already held in another role has not
// become the form's data by being renamed. Nothing else the /XFA array names
// is exempt, since the template and configuration packets define the form
// rather than fill it in.
func withoutFormData(
	ctx context.Context,
	doc *document.Document,
	signedRevision func() (*document.Document, error),
	objects []appendedObject,
	policy int,
) (_ []appendedObject, note string) {
	candidate := false
	for _, obj := range objects {
		candidate = candidate || (obj.IsStream && !permitted(obj, policy))
	}
	if !candidate {
		return objects, ""
	}

	rev, err := signedRevision()
	if err != nil {
		return objects, fmt.Sprintf("; XFA form data not recognized: %v", err)
	}
	signed, err := xfaDatasets(ctx, rev)
	if err != nil {
		return objects, fmt.Sprintf("; XFA form data not recognized: %v", err)
	}
	current, err := xfaDatasets(ctx, doc)
	if err != nil {
		return objects, fmt.Sprintf("; XFA form data not recognized: %v", err)
	}

	kept := make([]appendedObject, 0, len(objects))
	for _, obj := range objects {
		num := obj.ObjNum
		if obj.IsStream && (signed[num] || (current[num] && !rev.DefinesObject(num))) {
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
