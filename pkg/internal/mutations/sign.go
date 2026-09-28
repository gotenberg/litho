package mutations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// Signer produces a PKCS#7/CMS signature for the given digest. Implementations
// control key material and signing algorithms.
type Signer interface {
	// Sign computes a PKCS#7/CMS signature over the given SHA-256 digest.
	Sign(digest []byte) ([]byte, error)
	// Certificate returns the signing certificate for embedding in the
	// signature field's /Cert entry.
	Certificate() *x509.Certificate
	// EstimatedSize returns the maximum expected signature byte count.
	EstimatedSize() int
}

// SignatureInfo holds the optional entries of the signature dictionary (ISO
// 32000-1, Table 252) and the field the signature goes in. An empty string
// leaves its entry out, and a zero Time leaves /M out.
type SignatureInfo struct {
	// Name is the /Name of the signer.
	Name string
	// Reason is the /Reason for signing.
	Reason string
	// Location is the /Location of the signing.
	Location string
	// ContactInfo is the /ContactInfo a verifier may use to reach the signer.
	ContactInfo string
	// Time is the /M signing time.
	Time time.Time
	// Field is the fully qualified name of the signature field. An unsigned
	// signature field of that name is signed in place, and a new root field
	// takes it otherwise. Empty picks Signature1, Signature2, ...
	Field string
}

// entries adds the entries info sets to the signature dictionary sigDict.
func (info SignatureInfo) entries(sigDict ast.PdfDict) {
	for key, value := range map[ast.PdfName]string{
		"Name": info.Name, "Reason": info.Reason, "Location": info.Location, "ContactInfo": info.ContactInfo,
	} {
		if value != "" {
			sigDict[key] = document.EncodePdfString(value)
		}
	}
	if !info.Time.IsZero() {
		sigDict["M"] = ast.PdfString(PDFDate(info.Time))
	}
}

// PDFDate formats t as a PDF date (ISO 32000-1, 7.9.4), D:YYYYMMDDHHmmSS
// followed by the offset of t's zone from UTC: Z, or +HH'mm' or -HH'mm'.
func PDFDate(t time.Time) string {
	_, offset := t.Zone()
	if offset == 0 {
		return t.Format("D:20060102150405") + "Z"
	}
	sign := '+'
	if offset < 0 {
		sign, offset = '-', -offset
	}
	return fmt.Sprintf("%s%c%02d'%02d'", t.Format("D:20060102150405"), sign, offset/3600, offset%3600/60)
}

// MutationFunc is the signature of a deferred mutation applied during writing.
type MutationFunc func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error

// WriteWithSignature performs the two-pass incremental signing flow:
// write the incremental update with placeholder /Contents and /ByteRange
// values, fill in the ByteRange, compute SHA-256 over the bytes it covers
// (everything but the /Contents hex string), call [Signer.Sign] to get the
// PKCS#7 blob, then patch the /Contents placeholder with it.
//
// overrides holds objects an earlier stage replaced or added, keyed by their
// number in doc, such as the repairs of a remediation. They are queued before
// the mutations, so the update carries them and a mutation builds on them, the
// way the unsigned incremental write orders them. Signing used to start from
// an empty allocator, and a document signed after a remediation lost every
// repair the remediation had kept in overrides.
//
// info sets the signature dictionary's optional entries and the field the
// signature goes in (see [SignatureInfo]). A field named there that exists and
// is not an unsigned signature field is refused with
// [ErrSignatureFieldUnavailable], before anything is written.
//
// # Corpus evidence
//
//   - All corpora: of 110 files sampled across them, 42 could be signed after
//     a PDF/A-2b remediation, and 39 of those validated worse signed than
//     unsigned. pdfjs 160F-2019.pdf went from 7 violations to 671.
//   - All corpora: of another 110 files sampled across them, remediated at
//     PDF/A-2b and signed, poppler's pdfsig reported a digest mismatch on all
//     58 signed while the ByteRange was filled in after the digest.
func WriteWithSignature(ctx context.Context, dst io.Writer, doc *document.Document, signer Signer, info SignatureInfo, overrides map[int]ast.PdfObject, mutations []MutationFunc, xrefStream bool, media *document.MediaBoxDefault) error {
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	// A done ctx can stop the raw-byte augmentation short, and the allocator
	// then starts below objects the file defines.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("numbering the signature's objects: %w", err)
	}
	// Visited in order so the same overrides always drive the same sequence of
	// edits.
	for _, objNum := range slices.Sorted(maps.Keys(overrides)) {
		alloc.AllocAt(objNum, overrides[objNum])
	}

	// Apply non-signature mutations first.
	for _, m := range mutations {
		if err := m(ctx, alloc, doc); err != nil {
			return fmt.Errorf("applying mutation: %w", err)
		}
	}

	// Reserve space for the signature. The /Contents value is a hex string
	// of estimatedSize bytes, which requires estimatedSize*2 hex chars.
	estimatedSize := max(signer.EstimatedSize(), 8192)
	placeholderHexLen := estimatedSize * 2

	// Create the signature dictionary with a placeholder /Contents.
	// Actual zero bytes (not ASCII '0') make the serializer produce a hex
	// string <0000...0000> that the second pass can locate and patch.
	placeholderBytes := make([]byte, estimatedSize)
	sigDict := ast.PdfDict{
		"Type":      ast.PdfName("Sig"),
		"Filter":    ast.PdfName("Adobe.PPKLite"),
		"SubFilter": ast.PdfName("adbe.pkcs7.detached"),
		"Contents":  ast.PdfString(string(placeholderBytes)),
		"ByteRange": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(9999999999), ast.PdfInt(9999999999), ast.PdfInt(9999999999)},
	}
	info.entries(sigDict)

	// Update or create AcroForm with the signature field.
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root")
	}
	catalogDict := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return fmt.Errorf("resolving catalog")
	}

	// Get or create AcroForm.
	var acroFormDict ast.PdfDict
	var acroFormRef ast.PdfIndirectRef
	if afObj, ok := catalogDict["AcroForm"]; ok {
		if ref, ok := afObj.(ast.PdfIndirectRef); ok {
			acroFormRef = ref
			if d := ResolveFromAllocOrDoc(ctx, alloc, doc, ref); d != nil {
				acroFormDict = make(ast.PdfDict, len(d)+1)
				maps.Copy(acroFormDict, d)
			}
		}
		if acroFormDict == nil {
			if d, err := document.ResolveDict(ctx, doc, afObj); err == nil && d != nil {
				acroFormDict = make(ast.PdfDict, len(d)+1)
				maps.Copy(acroFormDict, d)
			}
		}
	}

	var fields ast.PdfArray
	if acroFormDict != nil {
		if arr, err := resolveArrayAllocFirst(ctx, alloc, doc, acroFormDict["Fields"]); err == nil {
			fields = slices.Clone(arr)
		}
	}
	if len(fields) == 0 {
		// A form that lists no field, or no form at all, describes none of
		// the widgets the pages already present. Once /Fields names the
		// signature, those widgets fall outside the form it defines (ISO
		// 32000-1, 12.7.1), so their fields are listed first, as an insertion
		// lists them ([mergeInsertedFields]).
		fields = targetRootFields(ctx, alloc, doc, signedPageRefs(ctx, alloc, doc))
	}
	existing, err := signatureField(ctx, alloc, doc, fields, info.Field)
	if err != nil {
		return err
	}
	fieldName := info.Field
	if fieldName == "" {
		if fieldName, err = signatureFieldName(ctx, alloc, doc, fields); err != nil {
			return err
		}
	}

	sigRef := alloc.Alloc(sigDict)
	if existing.dict != nil {
		// ISO 32000-1, 12.7.4.5: a signature field is signed by setting its
		// /V to the signature dictionary. Its widgets, on the pages already,
		// stay as they are, and the form lists it already.
		signed := make(ast.PdfDict, len(existing.dict)+1)
		maps.Copy(signed, existing.dict)
		signed["V"] = sigRef
		alloc.AllocAt(existing.ref.ObjectNum, signed)
	} else {
		fields = append(fields, alloc.Alloc(ast.PdfDict{
			"Type":    ast.PdfName("Annot"),
			"Subtype": ast.PdfName("Widget"),
			"FT":      ast.PdfName("Sig"),
			"T":       document.EncodePdfString(fieldName),
			"V":       sigRef,
			"Rect":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)},
			"F":       ast.PdfInt(132), // Print + Locked
		}))
	}

	if acroFormDict == nil {
		acroFormDict = ast.PdfDict{}
		acroFormRef = alloc.Alloc(ast.PdfNull{}) // placeholder
	}
	acroFormDict["Fields"] = fields
	acroFormDict["SigFlags"] = ast.PdfInt(3) // SignaturesExist | AppendOnly

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	if acroFormRef.ObjectNum > 0 {
		alloc.AllocAt(acroFormRef.ObjectNum, acroFormDict)
		newCatalog["AcroForm"] = acroFormRef
	} else {
		// The form dictionary was written inline in the catalog, and stays
		// there: ISO 32000-1, Table 28 does not ask for an indirect one. It
		// used to be queued as object 0, the head of the free list, with the
		// catalog pointing at 0 0 R.
		newCatalog["AcroForm"] = acroFormDict
	}
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	// The update writes the catalog, and the pages the edits before it wrote.
	if err := CompleteWrittenPageTree(ctx, alloc, doc, media); err != nil {
		return err
	}

	// The update is written to a buffer first, so the placeholder can be
	// patched before anything reaches dst.
	startXRef, err := document.FindStartXRef(doc.RawReader(), doc.FileSize())
	if err != nil {
		return fmt.Errorf("finding startxref: %w", err)
	}

	trailer := make(ast.PdfDict, len(doc.Index.Trailer))
	for k, v := range doc.Index.Trailer {
		if k == "Prev" || k == "Size" {
			continue
		}
		trailer[k] = v
	}

	useXRefStream := xrefStream
	if _, ok := doc.Index.Trailer["Type"]; ok {
		useXRefStream = true
	}

	var buf bytes.Buffer
	if err := writer.WriteIncremental(ctx, &buf, writer.IncrementalConfig{
		Source:          doc.RawReader(),
		SourceSize:      doc.FileSize(),
		StartXRefOffset: startXRef,
		Allocator:       alloc,
		Trailer:         trailer,
		UseXRefStream:   useXRefStream,
		Generation:      doc.ObjectGeneration,
	}); err != nil {
		return fmt.Errorf("writing incremental for signing: %w", err)
	}

	pdfBytes := buf.Bytes()

	// Both placeholders are looked for in the update alone. The source's bytes
	// come first in the buffer, and a source holding a hex string or a
	// ByteRange of the same shape, the placeholder of a signature another tool
	// prepared, would otherwise take the patch meant for this one.
	srcSize := doc.FileSize()
	if srcSize < 0 || srcSize > int64(len(pdfBytes)) {
		return fmt.Errorf("the %d-byte output cannot hold its %d-byte source", len(pdfBytes), srcSize)
	}
	update := pdfBytes[srcSize:]
	updateStart := len(pdfBytes) - len(update)

	// The serializer writes a binary PdfString as hex: <0000...0000>.
	placeholderHex := bytes.Repeat([]byte("00"), estimatedSize)
	sigContentsMarker := append([]byte{'<'}, placeholderHex...)
	sigContentsMarker = append(sigContentsMarker, '>')
	at := bytes.Index(update, sigContentsMarker)
	if at < 0 {
		return fmt.Errorf("signature placeholder not found in output")
	}
	sigStart := updateStart + at
	sigEnd := sigStart + len(sigContentsMarker)

	// ByteRange: [0, sigStart, sigEnd, totalLen-sigEnd]
	totalLen := len(pdfBytes)
	byteRange := [4]int{0, sigStart, sigEnd, totalLen - sigEnd}

	// The ByteRange array lies in the signature dictionary, inside the bytes
	// it covers (ISO 32000-1, 12.8.1), so it takes its final values before
	// the digest is computed. Hashing it with the placeholder values left
	// every signature failing verification. The placeholder uses ten-digit
	// values so the zero-padded offsets replace it in place, and no offset
	// computed above moves.
	brOld := []byte("/ByteRange [0 9999999999 9999999999 9999999999]")
	brNew := fmt.Appendf(nil, "/ByteRange [0 %010d %010d %010d]", byteRange[1], byteRange[2], byteRange[3])
	brAt := bytes.Index(update, brOld)
	if brAt < 0 || len(brNew) != len(brOld) {
		return fmt.Errorf("signature ByteRange placeholder not found in output")
	}
	copy(update[brAt:], brNew)

	h := sha256.New()
	h.Write(pdfBytes[byteRange[0] : byteRange[0]+byteRange[1]])
	h.Write(pdfBytes[byteRange[2] : byteRange[2]+byteRange[3]])
	digest := h.Sum(nil)

	sigBytes, err := signer.Sign(digest)
	if err != nil {
		return fmt.Errorf("signing: %w", err)
	}

	// Hex-encode the signature and pad it with zeros to fill the placeholder.
	sigHex := hex.EncodeToString(sigBytes)
	if len(sigHex) > placeholderHexLen {
		return fmt.Errorf("signature (%d bytes) exceeds estimated size (%d bytes)", len(sigBytes), estimatedSize)
	}
	sigHex += string(bytes.Repeat([]byte("0"), placeholderHexLen-len(sigHex)))
	copy(pdfBytes[sigStart+1:sigEnd-1], []byte(sigHex))

	if _, err := dst.Write(pdfBytes); err != nil {
		return fmt.Errorf("writing the signed document: %w", err)
	}
	return nil
}

// signatureFieldName returns the first of Signature1, Signature2, ... that no
// field of the interactive form holds as its fully qualified name, fields
// being the form's root fields.
//
// A field's fully qualified name joins the partial names of it and its
// ancestors with periods, and a field with no partial name adds none (ISO
// 32000-1, 12.7.3.2). The signature field goes at the root, so its partial
// name is its fully qualified name, and only a name without a period can equal
// it: the name of a root field, or of a field whose ancestors carry no /T. The
// walk descends through those ancestors alone. Two fields with one fully
// qualified name are two widgets of the same field (12.7.3.2), so a signature
// named after an existing field would not be a field of its own.
//
// Each indirect field and /Kids array is visited once, which ends a cycle and
// bounds the walk by the objects the file holds.
//
// The widgets on each page's /Annots count too (see [pageWidgetFieldNames]):
// the /Fields tree does not reach every field a reader sees.
func signatureFieldName(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fields ast.PdfArray) (string, error) {
	taken := make(map[string]bool)
	seen := make(map[int]bool)
	firstVisit := func(obj ast.PdfObject) bool {
		ref, ok := obj.(ast.PdfIndirectRef)
		if !ok {
			return true
		}
		if seen[ref.ObjectNum] {
			return false
		}
		seen[ref.ObjectNum] = true
		return true
	}

	stack := slices.Clone(fields)
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("naming the signature field: %w", err)
		}
		obj := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !firstVisit(obj) {
			continue
		}
		field, err := resolveDictAllocFirst(ctx, alloc, doc, obj)
		if err != nil || field == nil {
			continue
		}
		if t, ok := field["T"]; ok {
			if s, err := document.ResolveString(ctx, doc, t); err == nil && s != "" {
				taken[document.DecodePdfString(s)] = true
				continue
			}
		}
		kidsObj, ok := field["Kids"]
		if !ok || !firstVisit(kidsObj) {
			continue
		}
		if kids, err := resolveArrayAllocFirst(ctx, alloc, doc, kidsObj); err == nil {
			stack = append(stack, kids...)
		}
	}

	if err := pageWidgetFieldNames(ctx, alloc, doc, taken); err != nil {
		return "", err
	}

	// At most len(taken) candidates are held, so the loop ends by then.
	for i := 1; ; i++ {
		if name := fmt.Sprintf("Signature%d", i); !taken[name] {
			return name, nil
		}
	}
}

// signedPageRefs returns the pages of the document being signed: the ones an
// earlier deletion or insertion staged ([stagedPageKids]), or else the
// document's own. A page tree that cannot be read yields none.
func signedPageRefs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) []ast.PdfIndirectRef {
	if staged, ok := stagedPageKids(ctx, alloc, doc); ok {
		return staged
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		doc.Logger.Debug("signing: resolving the catalog for the page widgets failed", slog.Any("err", err))
		return nil
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		doc.Logger.Debug("signing: resolving the pages for their widgets failed", slog.Any("err", err))
		return nil
	}
	refs := make([]ast.PdfIndirectRef, 0, len(pages))
	for _, page := range pages {
		refs = append(refs, page.Ref)
	}
	return refs
}

// pageWidgetFieldNames adds to taken the fully qualified name of the field of
// every widget annotation on a page, pages and annotations read as alloc
// stages them.
//
// ISO 32000-1, 12.7.2 has the form's /Fields reach every field, but a widget a
// producer left out of it is still a field to the readers that build the form
// from each page's annotations: pdf.js names a widget's field from the widget
// and its /Parent chain, whatever /Fields holds, and qpdf reports such a widget
// and keeps it. A signature named after it would share its name there, and two
// fields sharing a fully qualified name are one field (12.7.3.2).
//
// A widget's name joins the partial names of it and its ancestors, reached
// through /Parent. Each field's name is worked out once and kept, and a chain
// that comes back to a field it passed ends there, so the walk is bounded by
// the objects the file holds.
//
// # Corpus evidence
//
//   - All corpora: 6 files carry a widget the /Fields tree does not reach, and
//     one of them names its field Signature2 (verapdf
//     PDF_A-2b__veraPDF_test_suite_6-1-12-t01-pass-a.pdf), with Signature1
//     free, so no file reaches the collision: over 114 files sampled, the
//     six included, signing picks the same name as before.
func pageWidgetFieldNames(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, taken map[string]bool) error {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		// A document whose pages cannot be read has no page widgets to name;
		// the /Fields walk already named what the form holds.
		doc.Logger.Debug("naming the signature field: resolving the catalog failed", slog.Any("err", err))
		return nil
	}
	readerPages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		doc.Logger.Debug("naming the signature field: resolving the pages failed", slog.Any("err", err))
		return nil
	}

	// names holds the fully qualified name of every field worked out so far,
	// by object number.
	names := make(map[int]string)
	fieldName := func(widget ast.PdfObject) string {
		// Climb from the widget to its root field, or to the first ancestor
		// already named, then name each field on the way back down.
		var chain []ast.PdfDict
		var nums []int
		seen := make(map[int]bool)
		name := ""
		for cur := widget; cur != nil; {
			ref, isRef := cur.(ast.PdfIndirectRef)
			if isRef {
				if known, done := names[ref.ObjectNum]; done {
					name = known
					break
				}
				if seen[ref.ObjectNum] {
					break
				}
				seen[ref.ObjectNum] = true
			}
			field, err := resolveDictAllocFirst(ctx, alloc, doc, cur)
			if err != nil || field == nil {
				break
			}
			chain = append(chain, field)
			nums = append(nums, ref.ObjectNum)
			cur = field["Parent"]
		}
		for i := len(chain) - 1; i >= 0; i-- {
			if t, hasT := chain[i]["T"]; hasT {
				if s, err := document.ResolveString(ctx, doc, t); err == nil && s != "" {
					if partial := document.DecodePdfString(s); name == "" {
						name = partial
					} else {
						name += "." + partial
					}
				}
			}
			if nums[i] > 0 {
				names[nums[i]] = name
			}
		}
		return name
	}

	for _, page := range StagedPages(ctx, alloc, doc, readerPages) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("naming the signature field: %w", err)
		}
		annots, err := resolveArrayAllocFirst(ctx, alloc, doc, latestPageDict(alloc, page)["Annots"])
		if err != nil {
			continue
		}
		for _, annotObj := range annots {
			annot, err := resolveDictAllocFirst(ctx, alloc, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}
			if subtype, _ := annot["Subtype"].(ast.PdfName); subtype != "Widget" {
				continue
			}
			if name := fieldName(annotObj); name != "" {
				taken[name] = true
			}
		}
	}
	return nil
}
