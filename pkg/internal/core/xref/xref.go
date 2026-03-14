package xref

import (
	"fmt"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// XRefEntry represents a single cross-reference entry describing the location
// and status of a PDF object (byte offset, generation number, and entry type).
type XRefEntry struct {
	Offset      int64
	Generation  int
	Free        bool
	Type        int // 0=free, 1=normal, 2=compressed in ObjStm
	ObjStmNum   int // Only for Type 2
	ObjStmIndex int // Only for Type 2
}

// Index is an aggregated cross-reference index containing a map of object
// numbers to their XRefEntry values and the associated trailer dictionary.
type Index struct {
	Entries map[int]XRefEntry
	Trailer ast.PdfDict
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

func decodeStreamData(stream ast.PdfStream) ([]byte, error) {
	data, err := filter.DecodeStream(stream)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedXRef, err)
	}
	return data, nil
}

// ParseXRefStream parses a PDF 1.5+ compressed cross-reference stream and
// populates indexMap with the decoded entries. It returns the byte offset of
// the previous cross-reference section (from the /Prev key), or 0 if none.
func ParseXRefStream(stream ast.PdfStream, indexMap map[int]XRefEntry) (int64, error) {
	wObj, ok := stream.Dict["W"]
	if !ok {
		return 0, fmt.Errorf("%w: missing /W array", ErrMalformedXRef)
	}
	wArray, ok := wObj.(ast.PdfArray)
	if !ok || len(wArray) < 3 {
		return 0, fmt.Errorf("%w: invalid /W array", ErrMalformedXRef)
	}

	w0 := int(wArray[0].(ast.PdfInt))
	w1 := int(wArray[1].(ast.PdfInt))
	w2 := int(wArray[2].(ast.PdfInt))
	entrySize := w0 + w1 + w2

	sizeObj, ok := stream.Dict["Size"]
	if !ok {
		return 0, fmt.Errorf("%w: missing /Size", ErrMalformedXRef)
	}
	size := int(sizeObj.(ast.PdfInt))

	var indexArray ast.PdfArray
	if indexObj, ok := stream.Dict["Index"]; ok {
		indexArray, ok = indexObj.(ast.PdfArray)
		if !ok {
			return 0, fmt.Errorf("%w: invalid /Index array", ErrMalformedXRef)
		}
	} else {
		indexArray = ast.PdfArray{ast.PdfInt(0), ast.PdfInt(size)}
	}

	data, err := decodeStreamData(stream)
	if err != nil {
		return 0, fmt.Errorf("%w: failed to decode stream: %v", ErrMalformedXRef, err)
	}

	offset := 0
	for i := 0; i < len(indexArray); i += 2 {
		startObj := int(indexArray[i].(ast.PdfInt))
		count := int(indexArray[i+1].(ast.PdfInt))

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
			case 0:
				entry.Free = true
				entry.Offset = field2
				entry.Generation = int(field3)
			case 1:
				entry.Free = false
				entry.Offset = field2
				entry.Generation = int(field3)
			case 2:
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
		prev = int64(prevObj.(ast.PdfInt))
	}

	return prev, nil
}

// ParseXRefTable parses a traditional plain-text cross-reference table and its
// trailer dictionary. It populates indexMap with the parsed entries and returns
// the trailer dictionary, the byte offset of the previous xref section, and
// any error encountered.
func ParseXRefTable(lex *lexer.Lexer, indexMap map[int]XRefEntry) (ast.PdfDict, int64, error) {
	tok, err := lex.NextToken()
	if err != nil {
		return nil, 0, err
	}

	if tok.Type != lexer.TokenKeyword || tok.Value != "xref" {
		return nil, 0, fmt.Errorf("%w: expected 'xref', got %v", ErrMalformedXRef, tok.Value)
	}

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

		startObj, _ := strconv.Atoi(tok1.Value)
		count, _ := strconv.Atoi(tok2.Value)

		for i := range count {
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

			offset, _ := strconv.ParseInt(tokOff.Value, 10, 64)
			gen, _ := strconv.Atoi(tokGen.Value)
			isFree := tokType.Value == "f"
			isNormal := tokType.Value == "n"

			if !isFree && !isNormal {
				return nil, 0, fmt.Errorf("%w: invalid entry type: %s", ErrMalformedXRef, tokType.Value)
			}

			if _, exists := indexMap[startObj+i]; !exists {
				entry := XRefEntry{
					Offset:     offset,
					Generation: gen,
					Free:       isFree,
					Type:       0,
				}
				if isNormal {
					entry.Type = 1
				}
				indexMap[startObj+i] = entry
			}
		}
	}

	p := parser.NewParser(lex)
	trailerObj, err := p.ParseObject()
	if err != nil {
		return nil, 0, err
	}

	trailerDict, ok := trailerObj.(ast.PdfDict)
	if !ok {
		return nil, 0, fmt.Errorf("%w: expected trailer dictionary, got %T", ErrMalformedXRef, trailerObj)
	}

	var prev int64 = 0
	if prevObj, ok := trailerDict["Prev"]; ok {
		prev = int64(prevObj.(ast.PdfInt))
	}

	return trailerDict, prev, nil
}
