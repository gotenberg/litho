package jbig2

import "fmt"

// GenericRegion decodes a generic region segment (6.2, 7.4.6).
type GenericRegion struct {
	RegionInfo *RegionSegmentInfo

	// Flags.
	UseExtTemplates bool
	IsTPGDon        bool
	GBTemplate      int
	IsMMREncoded    bool

	// Adaptive template pixels.
	GBAtX []int16
	GBAtY []int16

	// Optional parameters set externally (for use by SymbolDictionary etc.).
	DataOffset int64
	DataLength int64
	ArithDec   *ArithmeticDecoder
	Cx         *CX
	UseSkip    bool
	HSkip      *Bitmap

	bitmap *Bitmap
	data   []byte
}

// DecodeGenericRegion parses and decodes a generic region from the given data.
func DecodeGenericRegion(data []byte) (*Bitmap, error) {
	r := NewReader(data)
	gr := &GenericRegion{data: data}

	ri, err := ParseRegionSegmentInfo(r)
	if err != nil {
		return nil, fmt.Errorf("generic region info: %w", err)
	}
	gr.RegionInfo = ri

	// Parse generic region flags (7.4.6.2).
	if _, err := r.ReadBits(3); err != nil { // reserved
		return nil, err
	}
	extBit, err := r.ReadBit()
	if err != nil {
		return nil, err
	}
	gr.UseExtTemplates = extBit == 1

	tpgdBit, err := r.ReadBit()
	if err != nil {
		return nil, err
	}
	gr.IsTPGDon = tpgdBit == 1

	tmplBits, err := r.ReadBits(2)
	if err != nil {
		return nil, err
	}
	gr.GBTemplate = int(tmplBits)

	mmrBit, err := r.ReadBit()
	if err != nil {
		return nil, err
	}
	gr.IsMMREncoded = mmrBit == 1

	// Read AT pixels.
	if !gr.IsMMREncoded {
		numAT := 1
		if gr.GBTemplate == 0 {
			if gr.UseExtTemplates {
				numAT = 12
			} else {
				numAT = 4
			}
		}
		gr.GBAtX = make([]int16, numAT)
		gr.GBAtY = make([]int16, numAT)
		for i := 0; i < numAT; i++ {
			xb, err := r.ReadSignedByte()
			if err != nil {
				return nil, err
			}
			yb, err := r.ReadSignedByte()
			if err != nil {
				return nil, err
			}
			gr.GBAtX[i] = int16(xb)
			gr.GBAtY[i] = int16(yb)
		}
	}

	// Decode bitmap.
	gr.DataOffset = r.Position()
	gr.DataLength = r.Length() - r.Position()
	gr.data = data

	return gr.GetRegionBitmap()
}

// GetRegionBitmap decodes and returns the region's bitmap.
func (gr *GenericRegion) GetRegionBitmap() (*Bitmap, error) {
	if gr.bitmap != nil {
		return gr.bitmap, nil
	}

	w := gr.RegionInfo.Width
	h := gr.RegionInfo.Height

	if gr.IsMMREncoded {
		subData := gr.data[gr.DataOffset : gr.DataOffset+gr.DataLength]
		mmr := NewMMRDecompressor(w, h, subData)
		gr.bitmap = mmr.Uncompress()
		return gr.bitmap, nil
	}

	// Arithmetic decoding.
	if gr.ArithDec == nil {
		subReader := SubReader(gr.data, gr.DataOffset, gr.DataLength)
		var err error
		gr.ArithDec, err = NewArithmeticDecoder(subReader)
		if err != nil {
			return nil, err
		}
	}
	if gr.Cx == nil {
		gr.Cx = NewCX(65536, 1)
	}

	gr.bitmap = NewBitmap(w, h)
	paddedWidth := (w + 7) & ^7

	ltp := 0
	for line := range h {
		if gr.IsTPGDon {
			sltp, err := gr.decodeSLTP()
			if err != nil {
				return nil, err
			}
			ltp ^= sltp
		}

		if ltp == 1 {
			if line > 0 {
				gr.copyLineAbove(line)
			}
		} else {
			if err := gr.decodeLine(line, w, gr.bitmap.RowStride(), paddedWidth); err != nil {
				return nil, err
			}
		}
	}

	return gr.bitmap, nil
}

// ResetBitmap clears the cached bitmap to force re-decoding.
func (gr *GenericRegion) ResetBitmap() {
	gr.bitmap = nil
}

func (gr *GenericRegion) decodeSLTP() (int, error) {
	switch gr.GBTemplate {
	case 0:
		gr.Cx.SetIndex(0x9b25)
	case 1:
		gr.Cx.SetIndex(0x795)
	case 2:
		gr.Cx.SetIndex(0xe5)
	case 3:
		gr.Cx.SetIndex(0x195)
	}
	return gr.ArithDec.Decode(gr.Cx)
}

func (gr *GenericRegion) copyLineAbove(line int) {
	targetIdx := line * gr.bitmap.RowStride()
	sourceIdx := targetIdx - gr.bitmap.RowStride()
	for i := 0; i < gr.bitmap.RowStride(); i++ {
		gr.bitmap.SetByte(targetIdx+i, gr.bitmap.GetByte(sourceIdx+i))
	}
}

func (gr *GenericRegion) decodeLine(lineNumber, width, rowStride, paddedWidth int) error {
	switch gr.GBTemplate {
	case 0:
		return gr.decodeTemplate0(lineNumber, width, rowStride, paddedWidth)
	case 1:
		return gr.decodeTemplate1(lineNumber, width, rowStride, paddedWidth)
	case 2:
		return gr.decodeTemplate2(lineNumber, width, rowStride, paddedWidth)
	case 3:
		return gr.decodeTemplate3(lineNumber, width, rowStride, paddedWidth)
	default:
		return fmt.Errorf("%w: unknown template %d", ErrUnsupportedFeature, gr.GBTemplate)
	}
}

func (gr *GenericRegion) decodeTemplate0(lineNumber, width, rowStride, paddedWidth int) error {
	bm := gr.bitmap
	byteIndex := bm.ByteIndex(0, lineNumber)
	idx := byteIndex - rowStride

	var line1, line2 int
	if lineNumber >= 1 {
		line1 = bm.GetByteAsInt(idx)
	}
	if lineNumber >= 2 {
		line2 = bm.GetByteAsInt(idx-rowStride) << 6
	}

	context := (line1 & 0xf0) | (line2 & 0x3800)

	for x := 0; x < paddedWidth; x += 8 {
		var result byte
		nextByte := x + 8
		minorWidth := min(width-x, 8)

		if lineNumber > 0 {
			nextVal := 0
			if nextByte < width {
				nextVal = bm.GetByteAsInt(idx + 1)
			}
			line1 = (line1 << 8) | nextVal
		}
		if lineNumber > 1 {
			nextVal := 0
			if nextByte < width {
				nextVal = bm.GetByteAsInt(idx-rowStride+1) << 6
			}
			line2 = (line2 << 8) | nextVal
		}

		for minorX := range minorWidth {
			toShift := 7 - minorX
			gr.Cx.SetIndex(context)

			var bit int
			var err error
			if gr.UseSkip && gr.HSkip != nil && gr.HSkip.GetPixel(x+minorX, lineNumber) == 1 {
				bit = 0
			} else {
				bit, err = gr.ArithDec.Decode(gr.Cx)
				if err != nil {
					return err
				}
			}

			shift := uint(toShift) //nolint:gosec // toShift is 0-7
			result |= byte(bit) << shift
			context = ((context & 0x7bf7) << 1) | bit |
				((line1 >> shift) & 0x10) |
				((line2 >> shift) & 0x800)
		}

		bm.SetByte(byteIndex, result)
		byteIndex++
		idx++
	}
	return nil
}

//nolint:dupl // Templates 1 and 2 share structure but differ in shift constants per spec.
func (gr *GenericRegion) decodeTemplate1(lineNumber, width, rowStride, paddedWidth int) error {
	bm := gr.bitmap
	byteIndex := bm.ByteIndex(0, lineNumber)
	idx := byteIndex - rowStride

	var line1, line2 int
	if lineNumber >= 1 {
		line1 = bm.GetByteAsInt(idx)
	}
	if lineNumber >= 2 {
		line2 = bm.GetByteAsInt(idx-rowStride) << 5
	}

	context := ((line1 >> 1) & 0x1f8) | ((line2 >> 1) & 0x1e00)

	for x := 0; x < paddedWidth; x += 8 {
		var result byte
		nextByte := x + 8
		minorWidth := min(width-x, 8)

		if lineNumber >= 1 {
			nextVal := 0
			if nextByte < width {
				nextVal = bm.GetByteAsInt(idx + 1)
			}
			line1 = (line1 << 8) | nextVal
		}
		if lineNumber >= 2 {
			nextVal := 0
			if nextByte < width {
				nextVal = bm.GetByteAsInt(idx-rowStride+1) << 5
			}
			line2 = (line2 << 8) | nextVal
		}

		for minorX := range minorWidth {
			gr.Cx.SetIndex(context)

			var bit int
			var err error
			if gr.UseSkip && gr.HSkip != nil && gr.HSkip.GetPixel(x+minorX, lineNumber) == 1 {
				bit = 0
			} else {
				bit, err = gr.ArithDec.Decode(gr.Cx)
				if err != nil {
					return err
				}
			}

			shift := uint(7 - minorX) //nolint:gosec // minorX is 0-7
			result |= byte(bit) << shift
			toShift := uint(8 - minorX) //nolint:gosec // minorX is 0-7
			context = ((context & 0xefb) << 1) | bit |
				((line1 >> toShift) & 0x8) |
				((line2 >> toShift) & 0x200)
		}

		bm.SetByte(byteIndex, result)
		byteIndex++
		idx++
	}
	return nil
}

//nolint:dupl // Templates 1 and 2 share structure but differ in shift constants per spec.
func (gr *GenericRegion) decodeTemplate2(lineNumber, width, rowStride, paddedWidth int) error {
	bm := gr.bitmap
	byteIndex := bm.ByteIndex(0, lineNumber)
	idx := byteIndex - rowStride

	var line1, line2 int
	if lineNumber >= 1 {
		line1 = bm.GetByteAsInt(idx)
	}
	if lineNumber >= 2 {
		line2 = bm.GetByteAsInt(idx-rowStride) << 4
	}

	context := ((line1 >> 3) & 0x7c) | ((line2 >> 3) & 0x380)

	for x := 0; x < paddedWidth; x += 8 {
		var result byte
		nextByte := x + 8
		minorWidth := min(width-x, 8)

		if lineNumber >= 1 {
			nextVal := 0
			if nextByte < width {
				nextVal = bm.GetByteAsInt(idx + 1)
			}
			line1 = (line1 << 8) | nextVal
		}
		if lineNumber >= 2 {
			nextVal := 0
			if nextByte < width {
				nextVal = bm.GetByteAsInt(idx-rowStride+1) << 4
			}
			line2 = (line2 << 8) | nextVal
		}

		for minorX := range minorWidth {
			gr.Cx.SetIndex(context)

			var bit int
			var err error
			if gr.UseSkip && gr.HSkip != nil && gr.HSkip.GetPixel(x+minorX, lineNumber) == 1 {
				bit = 0
			} else {
				bit, err = gr.ArithDec.Decode(gr.Cx)
				if err != nil {
					return err
				}
			}

			shift := uint(7 - minorX) //nolint:gosec // minorX is 0-7
			result |= byte(bit) << shift
			toShift := uint(10 - minorX) //nolint:gosec // minorX is 0-7
			context = ((context & 0x1bd) << 1) | bit |
				((line1 >> toShift) & 0x4) |
				((line2 >> toShift) & 0x80)
		}

		bm.SetByte(byteIndex, result)
		byteIndex++
		idx++
	}
	return nil
}

func (gr *GenericRegion) decodeTemplate3(lineNumber, width, rowStride, paddedWidth int) error {
	bm := gr.bitmap
	byteIndex := bm.ByteIndex(0, lineNumber)
	idx := byteIndex - rowStride

	var line1 int
	if lineNumber >= 1 {
		line1 = bm.GetByteAsInt(idx)
	}

	context := (line1 >> 1) & 0x70

	for x := 0; x < paddedWidth; x += 8 {
		var result byte
		nextByte := x + 8
		minorWidth := min(width-x, 8)

		if lineNumber >= 1 {
			nextVal := 0
			if nextByte < width {
				nextVal = bm.GetByteAsInt(idx + 1)
			}
			line1 = (line1 << 8) | nextVal
		}

		for minorX := range minorWidth {
			gr.Cx.SetIndex(context)

			var bit int
			var err error
			if gr.UseSkip && gr.HSkip != nil && gr.HSkip.GetPixel(x+minorX, lineNumber) == 1 {
				bit = 0
			} else {
				bit, err = gr.ArithDec.Decode(gr.Cx)
				if err != nil {
					return err
				}
			}

			shift := uint(7 - minorX) //nolint:gosec // minorX is 0-7
			result |= byte(bit) << shift
			context = ((context & 0x1f7) << 1) | bit |
				((line1 >> uint(8-minorX)) & 0x010) //nolint:gosec // minorX is 0-7
		}

		bm.SetByte(byteIndex, result)
		byteIndex++
		idx++
	}
	return nil
}

// SetParametersMMR configures the region for MMR decoding with external parameters.
func (gr *GenericRegion) SetParametersMMR(dataOffset, dataLength int64, w, h int, allData []byte) {
	gr.IsMMREncoded = true
	gr.DataOffset = dataOffset
	gr.DataLength = dataLength
	gr.RegionInfo = &RegionSegmentInfo{Width: w, Height: h}
	gr.data = allData
	gr.bitmap = nil
}

// SetParametersArith configures the region for arithmetic decoding with external parameters.
func (gr *GenericRegion) SetParametersArith(template int, isTPGDon bool,
	useSkip bool, atX, atY []int16, w, h int,
	cx *CX, arithDec *ArithmeticDecoder, allData []byte,
	dataOffset, dataLength int64,
) {
	gr.IsMMREncoded = false
	gr.GBTemplate = template
	gr.IsTPGDon = isTPGDon
	gr.GBAtX = atX
	gr.GBAtY = atY
	gr.RegionInfo = &RegionSegmentInfo{Width: w, Height: h}
	gr.UseSkip = useSkip
	gr.Cx = cx
	gr.ArithDec = arithDec
	gr.data = allData
	gr.DataOffset = dataOffset
	gr.DataLength = dataLength
	gr.bitmap = nil
}
