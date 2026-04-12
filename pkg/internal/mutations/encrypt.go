package mutations

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
)

// EncryptPendingObjects encrypts all string and stream objects in the allocator.
func EncryptPendingObjects(ctx context.Context, alloc *writer.ObjAllocator, enc *crypto.Encryptor) error {
	for objNum, obj := range alloc.Pending() {
		if err := ctx.Err(); err != nil {
			return err
		}
		encrypted, err := encryptObject(obj, enc, objNum, 0)
		if err != nil {
			return fmt.Errorf("encrypting object %d: %w", objNum, err)
		}
		alloc.AllocAt(objNum, encrypted)
	}
	return nil
}

// encryptObject recursively encrypts strings and streams within an object.
func encryptObject(obj ast.PdfObject, enc *crypto.Encryptor, objNum, genNum int) (ast.PdfObject, error) {
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
		data, err := v.Bytes()
		if err != nil {
			return nil, err
		}
		encrypted, err := encFn(data)
		if err != nil {
			return nil, err
		}
		// Encrypt strings in the dict (but not /Length, /Filter, /DecodeParms).
		newDict, err := encryptDict(v.Dict, enc, objNum, genNum)
		if err != nil {
			return nil, err
		}
		return ast.NewStream(newDict, encrypted), nil
	case ast.PdfDict:
		return encryptDict(v, enc, objNum, genNum)
	case ast.PdfArray:
		result := make(ast.PdfArray, len(v))
		for i, elem := range v {
			encrypted, err := encryptObject(elem, enc, objNum, genNum)
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
func encryptDict(d ast.PdfDict, enc *crypto.Encryptor, objNum, genNum int) (ast.PdfDict, error) {
	result := make(ast.PdfDict, len(d))
	for k, v := range d {
		encrypted, err := encryptObject(v, enc, objNum, genNum)
		if err != nil {
			return nil, err
		}
		result[k] = encrypted
	}
	return result, nil
}
