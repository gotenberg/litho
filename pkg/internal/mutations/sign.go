package mutations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"maps"

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

// MutationFunc is the signature of a deferred mutation applied during writing.
type MutationFunc func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error

// WriteWithSignature performs the two-pass incremental signing flow:
// write the incremental update with a placeholder /Contents hex string,
// compute SHA-256 over the ByteRange (everything except the placeholder),
// call Signer.Sign to get the PKCS#7 blob, then patch the placeholder
// with the actual signature bytes.
func WriteWithSignature(ctx context.Context, dst io.Writer, doc *document.Document, signer Signer, mutations []MutationFunc, xrefStream bool) error {
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

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
	// We use actual zero bytes (not ASCII '0') so the serializer produces
	// a hex string <0000...0000> that we can locate and patch.
	placeholderBytes := make([]byte, estimatedSize)
	sigDict := ast.PdfDict{
		"Type":      ast.PdfName("Sig"),
		"Filter":    ast.PdfName("Adobe.PPKLite"),
		"SubFilter": ast.PdfName("adbe.pkcs7.detached"),
		"Contents":  ast.PdfString(string(placeholderBytes)),
		"ByteRange": ast.PdfArray{ast.PdfInt(0), ast.PdfInt(9999999999), ast.PdfInt(9999999999), ast.PdfInt(9999999999)},
	}
	sigRef := alloc.Alloc(sigDict)

	// Create a signature field and add it to AcroForm.
	sigFieldDict := ast.PdfDict{
		"Type":    ast.PdfName("Annot"),
		"Subtype": ast.PdfName("Widget"),
		"FT":      ast.PdfName("Sig"),
		"T":       ast.PdfString("Signature1"),
		"V":       sigRef,
		"Rect":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)},
		"F":       ast.PdfInt(132), // Hidden + Print
	}
	sigFieldRef := alloc.Alloc(sigFieldDict)

	// Update or create AcroForm with the signature field.
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
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

	if acroFormDict == nil {
		acroFormDict = ast.PdfDict{}
		acroFormRef = alloc.Alloc(ast.PdfNull{}) // placeholder
	}

	// Add sig field to /Fields.
	var fields ast.PdfArray
	if fieldsObj, ok := acroFormDict["Fields"]; ok {
		if arr, ok := fieldsObj.(ast.PdfArray); ok {
			fields = arr
		} else if arr, err := document.ResolveArray(ctx, doc, fieldsObj); err == nil {
			fields = arr
		}
	}
	fields = append(fields, sigFieldRef)
	acroFormDict["Fields"] = fields
	acroFormDict["SigFlags"] = ast.PdfInt(3) // SignaturesExist | AppendOnly

	alloc.AllocAt(acroFormRef.ObjectNum, acroFormDict)

	// Update catalog.
	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["AcroForm"] = acroFormRef
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	// Pass 1: write to a buffer so we can patch afterwards.
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

	// Find the placeholder in the output to determine ByteRange.
	// The serializer writes binary PdfString as hex: <0000...0000>.
	// Search for the unique zero-filled hex string.
	placeholderHex := bytes.Repeat([]byte("00"), estimatedSize)
	sigContentsMarker := append([]byte{'<'}, placeholderHex...)
	sigContentsMarker = append(sigContentsMarker, '>')
	sigStart := bytes.Index(pdfBytes, sigContentsMarker)
	if sigStart < 0 {
		return fmt.Errorf("signature placeholder not found in output")
	}
	sigEnd := sigStart + len(sigContentsMarker)

	// ByteRange: [0, sigStart, sigEnd, totalLen-sigEnd]
	totalLen := len(pdfBytes)
	byteRange := [4]int{0, sigStart, sigEnd, totalLen - sigEnd}

	// Pass 2: compute the SHA-256 hash over the ByteRange regions.
	h := sha256.New()
	h.Write(pdfBytes[byteRange[0] : byteRange[0]+byteRange[1]])
	h.Write(pdfBytes[byteRange[2] : byteRange[2]+byteRange[3]])
	digest := h.Sum(nil)

	// Get the PKCS#7 signature.
	sigBytes, err := signer.Sign(digest)
	if err != nil {
		return fmt.Errorf("signing: %w", err)
	}

	// Hex-encode the signature and pad to fill the placeholder.
	sigHex := hex.EncodeToString(sigBytes)
	if len(sigHex) > placeholderHexLen {
		return fmt.Errorf("signature (%d bytes) exceeds estimated size (%d bytes)", len(sigBytes), estimatedSize)
	}
	// Pad with zeros.
	sigHex += string(bytes.Repeat([]byte("0"), placeholderHexLen-len(sigHex)))

	// Patch the placeholder.
	copy(pdfBytes[sigStart+1:sigEnd-1], []byte(sigHex))

	// Patch the ByteRange array. The placeholder uses 10-digit values
	// (9999999999) so we can replace with zero-padded values of the same width.
	brOld := []byte("/ByteRange [0 9999999999 9999999999 9999999999]")
	brNew := fmt.Appendf(nil, "/ByteRange [0 %010d %010d %010d]", byteRange[1], byteRange[2], byteRange[3])
	pdfBytes = bytes.Replace(pdfBytes, brOld, brNew, 1)

	_, err = dst.Write(pdfBytes)
	return err
}
