package mutations

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
)

// applyEncryption encrypts every pending object and installs the /Encrypt
// dictionary and the matching /ID in the trailer. cfg must be non-nil.
//
// Order is load-bearing twice over. Every object the output will hold must
// already be in the allocator when this runs: [EncryptPendingObjects] walks
// what is pending, so an object allocated afterwards is written in plaintext
// inside a file whose trailer declares encryption, which no conforming reader
// can recover. And the /Encrypt dictionary is allocated after that sweep
// because ISO 32000-1, 7.6.1 exempts it from encryption: encrypting it would
// leave the handler unable to read the parameters it needs to decrypt.
//
// The trailer /ID is replaced rather than carried from the source. The
// standard security handler derives the file encryption key from the first
// /ID element (ISO 32000-1, 7.6.3.3), so a preserved /ID and a freshly
// generated key disagree and every string and stream decrypts to garbage.
//
// version is the header version the output is written with. When the output
// declares PDF 2.0, bit 10 of the P entry is set whatever mask cfg carries
// (see [crypto.AccessibilityPermission]).
func applyEncryption(ctx context.Context, alloc *writer.ObjAllocator, trailer ast.PdfDict, cfg *crypto.EncryptConfig, version string) error {
	enc := *cfg
	// ISO 32000-2, 7.6.4.2, Table 22, bit 10: "Not used. This bit was
	// previously used to determine whether content could be extracted for the
	// purposes of accessibility, however, that restriction has been deprecated
	// in PDF 2.0. PDF readers shall ignore this bit and PDF writers shall
	// always set this bit to 1 to ensure compatibility with PDF readers
	// following earlier specifications." The PDF Association errata for ISO
	// 32000-2:2020 leave that row as it is. Below 2.0, ISO 32000-1, 7.6.3.2,
	// Table 22 keeps the bit a permission a file may withhold, so the caller's
	// mask stands. The bit goes in before the encryptor is built: P enters the
	// file key (ISO 32000-1, 7.6.3.3, Algorithm 2) and /Perms (ISO 32000-2,
	// 7.6.4.4.9), so it must be final before either is computed.
	if writer.DeclaresPDF2(alloc, trailer, version) {
		enc.Permissions |= crypto.AccessibilityPermission
	}
	docID := GenerateDocumentID()
	encryptor, encDict, err := crypto.NewEncryptor(enc, docID)
	if err != nil {
		return fmt.Errorf("creating encryptor: %w", err)
	}

	// A stream written inside another object moves into one of its own before
	// the sweep keys its data to an object number (ISO 32000-1, 7.3.8.1 and
	// 7.6.2, Algorithm 1): moved afterwards, it would decrypt to noise.
	if _, err := writer.HoistDirectStreams(ctx, alloc, trailer); err != nil {
		return fmt.Errorf("hoisting direct streams: %w", err)
	}
	if err := EncryptPendingObjects(ctx, alloc, encryptor); err != nil {
		return fmt.Errorf("encrypting objects: %w", err)
	}

	trailer["Encrypt"] = alloc.Alloc(encDict)
	trailer["ID"] = ast.PdfArray{
		ast.PdfString(string(docID)),
		ast.PdfString(string(docID)),
	}
	return nil
}

// EncryptPendingObjects encrypts all string and stream objects in the allocator.
func EncryptPendingObjects(ctx context.Context, alloc *writer.ObjAllocator, enc *crypto.Encryptor) error {
	for objNum, obj := range alloc.Pending() {
		if err := ctx.Err(); err != nil {
			return err
		}
		encrypted, err := encryptObject(ctx, obj, enc, objNum, 0)
		if err != nil {
			return fmt.Errorf("encrypting object %d: %w", objNum, err)
		}
		alloc.AllocAt(objNum, encrypted)
	}
	return nil
}

// encryptObject recursively encrypts strings and streams within an object.
func encryptObject(ctx context.Context, obj ast.PdfObject, enc *crypto.Encryptor, objNum, genNum int) (ast.PdfObject, error) {
	encFn := enc.EncryptFunc(objNum, genNum)

	switch v := obj.(type) {
	case ast.PdfString:
		encrypted, err := encFn([]byte(v))
		if err != nil {
			return nil, err
		}
		return ast.PdfString(string(encrypted)), nil
	case ast.PdfStream:
		// Encrypt stream data.
		data, err := v.Bytes(ctx)
		if err != nil {
			return nil, err
		}
		encrypted, err := encFn(data)
		if err != nil {
			return nil, err
		}
		// Encrypt strings in the dict (but not /Length, /Filter, /DecodeParms).
		newDict, err := encryptDict(ctx, v.Dict, enc, objNum, genNum)
		if err != nil {
			return nil, err
		}
		return ast.NewStream(newDict, encrypted), nil
	case ast.PdfDict:
		return encryptDict(ctx, v, enc, objNum, genNum)
	case ast.PdfArray:
		result := make(ast.PdfArray, len(v))
		for i, elem := range v {
			encrypted, err := encryptObject(ctx, elem, enc, objNum, genNum)
			if err != nil {
				return nil, err
			}
			result[i] = encrypted
		}
		return result, nil
	default:
		return obj, nil // scalars, refs, null: not encrypted
	}
}

// encryptDict encrypts string values within a dictionary.
//
// The /Contents of a signature dictionary stays in the clear: ISO 32000-2,
// 7.6.2 lists it among the strings encryption does not apply to, since the
// signature value fills the hole its ByteRange leaves after the digest is
// computed. Encrypted, it reached every reader as noise, the parser included,
// which reads it in the clear (see [parser.IsSignatureDict], the test both
// sides share).
//
// # Corpus evidence
//
//   - All corpora: 162 signed documents, the 61 corpus files that carry a
//     signature and 101 outputs litho signed, encrypted at AES-256. litho read
//     none of the 142 signature values it finds back as signed, and reads all
//     142 now. qpdf reads 159 of 164 back, from 2. It decrypts a document
//     timestamp's value, which litho leaves in the clear as it does a /Sig's:
//     pdfjs issue8047.pdf and issue15813.pdf.
func encryptDict(ctx context.Context, d ast.PdfDict, enc *crypto.Encryptor, objNum, genNum int) (ast.PdfDict, error) {
	signature := parser.IsSignatureDict(d)
	result := make(ast.PdfDict, len(d))
	for k, v := range d {
		if signature && k == "Contents" {
			result[k] = v
			continue
		}
		encrypted, err := encryptObject(ctx, v, enc, objNum, genNum)
		if err != nil {
			return nil, err
		}
		result[k] = encrypted
	}
	return result, nil
}
