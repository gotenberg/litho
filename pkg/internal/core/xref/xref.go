package xref

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// XRef entry type constants (ISO 32000-1, Table 18).
const (
	XRefTypeFree       = 0 // Type 0: free object
	XRefTypeNormal     = 1 // Type 1: object at a byte offset
	XRefTypeCompressed = 2 // Type 2: object in an object stream
)

// XRefEntry represents a single cross-reference entry describing the location
// and status of a PDF object (byte offset, generation number, and entry type).
type XRefEntry struct {
	Offset      int64
	Generation  int
	Free        bool
	Type        int // XRefTypeFree, XRefTypeNormal, or XRefTypeCompressed
	ObjStmNum   int // Only for XRefTypeCompressed
	ObjStmIndex int // Only for XRefTypeCompressed
}

// Index is an aggregated cross-reference index containing a map of object
// numbers to their XRefEntry values and the associated trailer dictionary.
type Index struct {
	Entries map[int]XRefEntry
	Trailer ast.PdfDict

	// LastTrailer is the trailer of the oldest section on the /Prev chain. In
	// a linearized file that is the main trailer at the end, while Trailer is
	// the first-page trailer at the top. ISO 19005-1, 6.1.3 test 4 compares
	// the two /ID values, which Trailer alone cannot answer. It is nil when no
	// trailer was parsed and equal to Trailer when the file has one section.
	LastTrailer ast.PdfDict
}

func readInt(data []byte, width int) int64 {
	var result int64 = 0
	for i := range width {
		if i < len(data) {
			result = (result << 8) | int64(data[i])
		}
	}
	return result
}

func decodeStreamData(ctx context.Context, stream ast.PdfStream) ([]byte, error) {
	data, err := filter.DecodeStream(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedXRef, err)
	}
	return data, nil
}

// ParseXRefStream parses a PDF 1.5+ compressed cross-reference stream and
// populates indexMap with the decoded entries. It returns the byte offset of
// the previous cross-reference section (from the /Prev key), or 0 if none.
func ParseXRefStream(ctx context.Context, stream ast.PdfStream, indexMap map[int]XRefEntry) (int64, error) {
	wObj, ok := stream.Dict["W"]
	if !ok {
		return 0, fmt.Errorf("%w: missing /W array", ErrMalformedXRef)
	}
	wArray, ok := wObj.(ast.PdfArray)
	if !ok || len(wArray) < 3 {
		return 0, fmt.Errorf("%w: invalid /W array", ErrMalformedXRef)
	}

	w0Int, ok := wArray[0].(ast.PdfInt)
	if !ok {
		return 0, fmt.Errorf("%w: /W[0] is not an integer", ErrMalformedXRef)
	}
	w1Int, ok := wArray[1].(ast.PdfInt)
	if !ok {
		return 0, fmt.Errorf("%w: /W[1] is not an integer", ErrMalformedXRef)
	}
	w2Int, ok := wArray[2].(ast.PdfInt)
	if !ok {
		return 0, fmt.Errorf("%w: /W[2] is not an integer", ErrMalformedXRef)
	}
	w0 := int(w0Int)
	w1 := int(w1Int)
	w2 := int(w2Int)
	entrySize := w0 + w1 + w2

	sizeObj, ok := stream.Dict["Size"]
	if !ok {
		return 0, fmt.Errorf("%w: missing /Size", ErrMalformedXRef)
	}
	sizeInt, ok := sizeObj.(ast.PdfInt)
	if !ok {
		return 0, fmt.Errorf("%w: /Size is not an integer", ErrMalformedXRef)
	}
	size := int(sizeInt)

	var indexArray ast.PdfArray
	if indexObj, ok := stream.Dict["Index"]; ok {
		indexArray, ok = indexObj.(ast.PdfArray)
		if !ok {
			return 0, fmt.Errorf("%w: invalid /Index array", ErrMalformedXRef)
		}
	} else {
		indexArray = ast.PdfArray{ast.PdfInt(0), ast.PdfInt(size)}
	}

	data, err := decodeStreamData(ctx, stream)
	if err != nil {
		return 0, fmt.Errorf("%w: failed to decode stream: %v", ErrMalformedXRef, err)
	}

	offset := 0
	for i := 0; i < len(indexArray); i += 2 {
		startInt, ok := indexArray[i].(ast.PdfInt)
		if !ok {
			return 0, fmt.Errorf("%w: /Index start is not an integer", ErrMalformedXRef)
		}
		countInt, ok := indexArray[i+1].(ast.PdfInt)
		if !ok {
			return 0, fmt.Errorf("%w: /Index count is not an integer", ErrMalformedXRef)
		}
		startObj := int(startInt)
		count := int(countInt)

		for j := range count {
			if offset+entrySize > len(data) {
				return 0, fmt.Errorf("%w: stream data too short", ErrMalformedXRef)
			}

			entryData := data[offset : offset+entrySize]
			offset += entrySize

			var field1 int64 = 1 // Default Type 1
			if w0 > 0 {
				field1 = readInt(entryData[0:w0], w0)
			}

			var field2 int64 = 0
			if w1 > 0 {
				field2 = readInt(entryData[w0:w0+w1], w1)
			}

			var field3 int64 = 0
			if w2 > 0 {
				field3 = readInt(entryData[w0+w1:w0+w1+w2], w2)
			}

			entry := XRefEntry{
				Type: int(field1),
			}

			switch entry.Type {
			case XRefTypeFree:
				entry.Free = true
				entry.Offset = field2
				entry.Generation = int(field3)
			case XRefTypeNormal:
				entry.Free = false
				entry.Offset = field2
				entry.Generation = int(field3)
			case XRefTypeCompressed:
				entry.Free = false
				entry.ObjStmNum = int(field2)
				entry.ObjStmIndex = int(field3)
				entry.Generation = 0
			}

			// Don't overwrite existing entries (incremental updates are read back-to-front)
			if _, exists := indexMap[startObj+j]; !exists {
				indexMap[startObj+j] = entry
			}
		}
	}

	var prev int64 = 0
	if prevObj, ok := stream.Dict["Prev"]; ok {
		if prevInt, ok := prevObj.(ast.PdfInt); ok {
			prev = int64(prevInt)
		}
	}

	return prev, nil
}

// ParseXRefTable parses a traditional plain-text cross-reference table and its
// trailer dictionary. It populates indexMap with the parsed entries and returns
// the trailer dictionary, the byte offset of the previous xref section, and
// any error encountered.
// freeListHeadGeneration is the generation number ISO 32000-1, 7.5.4 gives the
// head of the free list, which is always object 0.
const freeListHeadGeneration = 65535

func ParseXRefTable(ctx context.Context, lex *lexer.Lexer, indexMap map[int]XRefEntry) (ast.PdfDict, int64, error) {
	tok, err := lex.NextToken()
	if err != nil {
		return nil, 0, err
	}

	if tok.Type != lexer.TokenKeyword || tok.Value != "xref" {
		return nil, 0, fmt.Errorf("%w: expected 'xref', got %v", ErrMalformedXRef, tok.Value)
	}

	entryCount := 0
	for {
		tok1, err := lex.NextToken()
		if err != nil {
			return nil, 0, err
		}

		if tok1.Type == lexer.TokenKeyword && tok1.Value == "trailer" {
			break
		}

		if tok1.Type != lexer.TokenNumber {
			return nil, 0, fmt.Errorf("%w: expected object number or trailer, got %v %s", ErrMalformedXRef, tok1.Type, tok1.Value)
		}

		tok2, err := lex.NextToken()
		if err != nil {
			return nil, 0, err
		}
		if tok2.Type != lexer.TokenNumber {
			return nil, 0, fmt.Errorf("%w: expected count, got %v", ErrMalformedXRef, tok2.Type)
		}

		startObj, err := strconv.Atoi(tok1.Value)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: invalid start object number %q: %v", ErrMalformedXRef, tok1.Value, err)
		}
		count, err := strconv.Atoi(tok2.Value)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: invalid entry count %q: %v", ErrMalformedXRef, tok2.Value, err)
		}

		for i := range count {
			if entryCount%100 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, 0, fmt.Errorf("parsing xref table: %w", err)
				}
			}
			entryCount++

			tokOff, err := lex.NextToken()
			if err != nil {
				return nil, 0, err
			}
			tokGen, err := lex.NextToken()
			if err != nil {
				return nil, 0, err
			}
			tokType, err := lex.NextToken()
			if err != nil {
				return nil, 0, err
			}

			if tokOff.Type != lexer.TokenNumber || tokGen.Type != lexer.TokenNumber || tokType.Type != lexer.TokenKeyword {
				return nil, 0, fmt.Errorf("%w: invalid entry", ErrMalformedXRef)
			}

			offset, err := strconv.ParseInt(tokOff.Value, 10, 64)
			if err != nil {
				return nil, 0, fmt.Errorf("%w: invalid offset %q: %v", ErrMalformedXRef, tokOff.Value, err)
			}
			gen, err := strconv.Atoi(tokGen.Value)
			if err != nil {
				return nil, 0, fmt.Errorf("%w: invalid generation %q: %v", ErrMalformedXRef, tokGen.Value, err)
			}
			isFree := tokType.Value == "f"
			isNormal := tokType.Value == "n"

			if !isFree && !isNormal {
				return nil, 0, fmt.Errorf("%w: invalid entry type: %s", ErrMalformedXRef, tokType.Value)
			}

			// ISO 32000-1, 7.5.4: the free-list head, offset 0 with
			// generation 65535, is object 0 and only object 0. A subsection
			// that declares it as object 1 has labelled every one of its
			// entries one too high, which silently maps each object onto its
			// predecessor's bytes and marks the real object 1 free. Producers
			// do emit this (pdfjs issue7229.pdf, where it cost page 1 its only
			// image), and each entry then lands exactly on the object header
			// one lower, so the intended start is 0.
			if i == 0 && startObj == 1 && offset == 0 && gen == freeListHeadGeneration && isFree {
				startObj = 0
			}

			if _, exists := indexMap[startObj+i]; !exists {
				entry := XRefEntry{
					Offset:     offset,
					Generation: gen,
					Free:       isFree,
					Type:       0,
				}
				if isNormal {
					entry.Type = XRefTypeNormal
				}
				indexMap[startObj+i] = entry
			}
		}
	}

	p := parser.NewParser(lex)
	trailerObj, err := p.ParseObject(ctx)
	if err != nil {
		return nil, 0, err
	}

	trailerDict, ok := trailerObj.(ast.PdfDict)
	if !ok {
		return nil, 0, fmt.Errorf("%w: expected trailer dictionary, got %T", ErrMalformedXRef, trailerObj)
	}

	var prev int64
	if prevObj, ok := trailerDict["Prev"]; ok {
		switch v := prevObj.(type) {
		case ast.PdfInt:
			prev = int64(v)
		case ast.PdfReal:
			prev = int64(v)
		}
	}

	return trailerDict, prev, nil
}
