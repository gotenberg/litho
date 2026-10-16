package mutations

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // ISO 32000-2, 12.8.4.4 keys a VRI dictionary by the SHA-1 of the signature; nothing is signed with it.
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxDSSCompared bounds the streams of an existing document security store
// read to keep the update from adding a second copy of one. Anti-DoS guard,
// not spec-mandated: a store gains a few certificates, responses and CRLs per
// signature, and past this many the update adds its data without comparing.
const maxDSSCompared = 1024

// DSSData is the validation data an update adds to a document's security
// store (ISO 32000-2, 12.8.4.3): DER certificates, OCSP responses and CRLs,
// and the signature they validate, by its /Contents.
type DSSData struct {
	Certs, OCSPs, CRLs [][]byte
	// Signature is the /Contents of the signature the data validates, whose
	// SHA-1 keys its VRI dictionary (ISO 32000-2, 12.8.4.4).
	Signature []byte
}

// VRIKey returns the key of the VRI dictionary of the signature whose
// /Contents is contents: the uppercase hexadecimal SHA-1 of the signature
// (ISO 32000-2, 12.8.4.4, Table 263), taken over the /Contents string as it
// stands, its zero padding included, as iText and PDFBox take it and pyHanko
// documents (DocumentSecurityStore.add_dss: "including any padding"). ETSI
// EN 319 142-1 V1.2.1, 5.4.2.2, requirement a) hashes the same bytes.
func VRIKey(contents []byte) string {
	sum := sha1.Sum(contents) //nolint:gosec // ISO 32000-2, 12.8.4.4 names SHA-1.
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// WriteDSS writes doc to dst with an incremental update that adds data to its
// document security store (ISO 32000-2, 12.8.4.3; ETSI EN 319 142-1, 5.4.2):
// a stream per certificate, OCSP response and CRL the store does not hold
// already, listed in its /Certs, /OCSPs and /CRLs and in the VRI dictionary of
// the signature, and the catalog's /DSS naming the store. A document older
// than PDF 2.0 also declares the ESIC extension (see [DeclareESIC]).
//
// The update holds validation data alone, which ISO 32000-2, 12.8.2.2, Table
// 257 exempts from every signature's modification detection.
func WriteDSS(ctx context.Context, dst io.Writer, doc *document.Document, data DSSData, xrefStream bool) error {
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("numbering the security store's objects: %w", err)
	}
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root")
	}
	catalogDict := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return fmt.Errorf("resolving catalog")
	}

	dss := ast.PdfDict{"Type": ast.PdfName("DSS")}
	dssObj, hasDSS := catalogDict["DSS"]
	if hasDSS {
		current, err := resolveDictAllocFirst(ctx, alloc, doc, dssObj)
		if err != nil || current == nil {
			// A /DSS that is not a dictionary holds no validation data, and
			// ISO 32000-2, Table 29 types it one, so the update writes one.
			doc.Logger.Debug("signing: the catalog's /DSS is no dictionary; writing a new one", slog.Any("err", err))
		} else {
			dss = maps.Clone(current)
		}
	}

	compared := 0
	appendStreams := func(key ast.PdfName, values [][]byte) (ast.PdfArray, error) {
		existing, err := resolveArrayAllocFirst(ctx, alloc, doc, dss[key])
		if err != nil {
			existing = nil
		}
		list := append(ast.PdfArray(nil), existing...)
		var refs ast.PdfArray
		for _, v := range values {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("writing the security store: %w", err)
			}
			ref, found := ast.PdfIndirectRef{}, false
			for _, e := range existing {
				if compared >= maxDSSCompared {
					break
				}
				r, ok := e.(ast.PdfIndirectRef)
				if !ok {
					continue
				}
				compared++
				stream, err := document.ResolveStream(ctx, doc, r)
				if err != nil {
					continue
				}
				have, err := doc.DecodeStream(ctx, stream)
				if err == nil && bytes.Equal(have, v) {
					ref, found = r, true
					break
				}
			}
			if !found {
				ref = alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(v))}, v))
				list = append(list, ref)
			}
			refs = append(refs, ref)
		}
		if len(list) > 0 {
			dss[key] = list
		}
		return refs, nil
	}
	certs, err := appendStreams("Certs", data.Certs)
	if err != nil {
		return err
	}
	ocsps, err := appendStreams("OCSPs", data.OCSPs)
	if err != nil {
		return err
	}
	crls, err := appendStreams("CRLs", data.CRLs)
	if err != nil {
		return err
	}

	if len(data.Signature) > 0 {
		vri := ast.PdfDict{}
		if current, err := resolveDictAllocFirst(ctx, alloc, doc, dss["VRI"]); err == nil && current != nil {
			vri = maps.Clone(current)
		}
		// ISO 32000-2, 12.8.4.4, Table 263: the VRI dictionary of one
		// signature names the store's streams that validate it.
		entry := ast.PdfDict{"Type": ast.PdfName("VRI")}
		for key, refs := range map[ast.PdfName]ast.PdfArray{"Cert": certs, "OCSP": ocsps, "CRL": crls} {
			if len(refs) > 0 {
				entry[key] = refs
			}
		}
		vri[ast.PdfName(VRIKey(data.Signature))] = alloc.Alloc(entry)
		dss["VRI"] = alloc.Alloc(vri)
	}

	newCatalog := maps.Clone(catalogDict)
	if ref, ok := dssObj.(ast.PdfIndirectRef); hasDSS && ok && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, dss)
	} else {
		newCatalog["DSS"] = alloc.Alloc(dss)
	}
	if err := DeclareESIC(ctx, alloc, doc, newCatalog, ESICLevelLTV); err != nil {
		return err
	}
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	cfg, err := incrementalConfig(doc, alloc, xrefStream)
	if err != nil {
		return err
	}
	if err := writer.WriteIncremental(ctx, dst, cfg); err != nil {
		return fmt.Errorf("writing the security store update: %w", err)
	}
	return nil
}
