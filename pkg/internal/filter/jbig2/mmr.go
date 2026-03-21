package jbig2

// MMR (Modified Modified READ) decompressor for JBIG2 generic regions.
// Implements the 2D MMR/Group 4 fax decoding as used in JBIG2.

// MMR mode codes.
const (
	mmrCodeP   = 0
	mmrCodeH   = 1
	mmrCodeV0  = 2
	mmrCodeVR1 = 3
	mmrCodeVR2 = 4
	mmrCodeVR3 = 5
	mmrCodeVL1 = 6
	mmrCodeVL2 = 7
	mmrCodeVL3 = 8
	mmrEOL     = -1
	mmrEOF     = -3
)

// mmrCode represents a code in the MMR lookup tables.
type mmrCode struct {
	bitLength int
	codeWord  int
	runLength int
	subTable  []mmrCode
}

const (
	mmrFirstLevelSize  = 8
	mmrFirstLevelMask  = (1 << mmrFirstLevelSize) - 1
	mmrSecondLevelSize = 5
	mmrSecondLevelMask = (1 << mmrSecondLevelSize) - 1
	mmrCodeOffset      = 24
)

var (
	mmrWhiteTable []mmrCode
	mmrBlackTable []mmrCode
	mmrModeTable  []mmrCode
)

func initMMRTables() {
	if mmrWhiteTable != nil {
		return
	}
	mmrWhiteTable = createMMRTable(whiteCodes)
	mmrBlackTable = createMMRTable(blackCodes)
	mmrModeTable = createMMRTable(modeCodes)
}

// MMRDecompressor decompresses MMR-encoded JBIG2 data.
type MMRDecompressor struct {
	width      int
	height     int
	data       []byte
	offset     int // bit offset
	lastOffset int
}

// NewMMRDecompressor creates a new MMR decompressor.
func NewMMRDecompressor(width, height int, data []byte) *MMRDecompressor {
	initMMRTables()
	return &MMRDecompressor{
		width:      width,
		height:     height,
		data:       data,
		lastOffset: 1,
	}
}

// Uncompress decodes the MMR data and returns the resulting bitmap.
func (m *MMRDecompressor) Uncompress() *Bitmap {
	result := NewBitmap(m.width, m.height)

	currentOffsets := make([]int, m.width+5)
	referenceOffsets := make([]int, m.width+5)
	referenceOffsets[0] = m.width
	refRunLength := 1

	for line := 0; line < m.height; line++ {
		count := m.uncompress2D(referenceOffsets, refRunLength, currentOffsets)
		if count == mmrEOF {
			break
		}
		if count > 0 {
			m.fillBitmap(result, line, currentOffsets, count)
		}

		// Swap lines.
		referenceOffsets, currentOffsets = currentOffsets, referenceOffsets
		refRunLength = count
	}

	// Skip trailing EOL markers.
	m.detectAndSkipEOL()

	// Align to byte boundary.
	m.offset = ((m.offset + 7) >> 3) << 3

	return result
}

func (m *MMRDecompressor) uncompress2D(refOffsets []int, refRunLen int, runOffsets []int) int {
	refBufOff := 0
	curBufOff := 0
	bitPos := 0
	whiteRun := true

	refOffsets[refRunLen] = m.width
	refOffsets[refRunLen+1] = m.width
	if refRunLen+2 < len(refOffsets) {
		refOffsets[refRunLen+2] = m.width + 1
	}
	if refRunLen+3 < len(refOffsets) {
		refOffsets[refRunLen+3] = m.width + 1
	}

	for bitPos < m.width {
		code := m.getCode(mmrModeTable)
		if code == nil {
			m.offset++
			break
		}
		m.offset += code.bitLength

		switch code.runLength {
		case mmrCodeV0:
			bitPos = refOffsets[refBufOff]
		case mmrCodeVR1:
			bitPos = refOffsets[refBufOff] + 1
		case mmrCodeVL1:
			bitPos = refOffsets[refBufOff] - 1
		case mmrCodeVR2:
			bitPos = refOffsets[refBufOff] + 2
		case mmrCodeVL2:
			bitPos = refOffsets[refBufOff] - 2
		case mmrCodeVR3:
			bitPos = refOffsets[refBufOff] + 3
		case mmrCodeVL3:
			bitPos = refOffsets[refBufOff] - 3
		case mmrCodeH:
			// First half of horizontal mode.
			var table []mmrCode
			if whiteRun {
				table = mmrWhiteTable
			} else {
				table = mmrBlackTable
			}
			for {
				code = m.getCode(table)
				if code == nil {
					return mmrEOL
				}
				m.offset += code.bitLength
				if code.runLength < 64 {
					if code.runLength < 0 {
						runOffsets[curBufOff] = bitPos
						return mmrEOL
					}
					bitPos += code.runLength
					runOffsets[curBufOff] = bitPos
					curBufOff++
					break
				}
				bitPos += code.runLength
			}

			// Second half.
			firstHalfBitPos := bitPos
			if whiteRun {
				table = mmrBlackTable
			} else {
				table = mmrWhiteTable
			}
			for {
				code = m.getCode(table)
				if code == nil {
					return mmrEOL
				}
				m.offset += code.bitLength
				if code.runLength < 64 {
					if code.runLength < 0 {
						runOffsets[curBufOff] = bitPos
						return mmrEOL
					}
					bitPos += code.runLength
					if bitPos < m.width || bitPos != firstHalfBitPos {
						runOffsets[curBufOff] = bitPos
						curBufOff++
					}
					break
				}
				bitPos += code.runLength
			}

			for bitPos < m.width && refOffsets[refBufOff] <= bitPos {
				refBufOff += 2
			}
			continue

		case mmrCodeP:
			refBufOff++
			bitPos = refOffsets[refBufOff]
			refBufOff++
			continue

		default:
			// EOL or unknown
			bitPos = m.width
			continue
		}

		// Only vertical modes reach here.
		if bitPos <= m.width {
			whiteRun = !whiteRun
			runOffsets[curBufOff] = bitPos
			curBufOff++

			if refBufOff > 0 {
				refBufOff--
			} else {
				refBufOff++
			}

			for bitPos < m.width && refOffsets[refBufOff] <= bitPos {
				refBufOff += 2
			}
		}
	}

	if curBufOff < len(runOffsets) && runOffsets[curBufOff] != m.width {
		runOffsets[curBufOff] = m.width
	}

	return curBufOff
}

func (m *MMRDecompressor) getCode(table []mmrCode) *mmrCode {
	codeWord := m.peekBits(24)
	idx := (codeWord >> (mmrCodeOffset - mmrFirstLevelSize)) & mmrFirstLevelMask
	if idx >= len(table) {
		return nil
	}
	entry := &table[idx]
	if entry.subTable != nil {
		subIdx := (codeWord >> (mmrCodeOffset - mmrFirstLevelSize - mmrSecondLevelSize)) & mmrSecondLevelMask
		if subIdx >= len(entry.subTable) {
			return nil
		}
		return &entry.subTable[subIdx]
	}
	if entry.bitLength == 0 && entry.runLength == 0 {
		return nil
	}
	return entry
}

func (m *MMRDecompressor) peekBits(n int) int {
	byteOff := m.offset >> 3
	bitOff := m.offset & 7

	result := 0
	if byteOff < len(m.data) {
		result = int(m.data[byteOff]&0xff) << 16
	}
	if byteOff+1 < len(m.data) {
		result |= int(m.data[byteOff+1]&0xff) << 8
	}
	if byteOff+2 < len(m.data) {
		result |= int(m.data[byteOff+2] & 0xff)
	}

	result <<= bitOff
	return result & 0xffffff
}

func (m *MMRDecompressor) fillBitmap(result *Bitmap, line int, offsets []int, count int) {
	x := 0
	targetByte := result.ByteIndex(0, line)
	var targetByteValue byte

	for index := range count {
		offset := offsets[index]
		var value byte
		if index&1 != 0 {
			value = 1
		}

		for x < offset {
			targetByteValue = (targetByteValue << 1) | value
			x++
			if x&7 == 0 {
				result.SetByte(targetByte, targetByteValue)
				targetByte++
				targetByteValue = 0
			}
		}
	}

	if x&7 != 0 {
		targetByteValue <<= uint(8 - (x & 7))
		result.SetByte(targetByte, targetByteValue)
	}
}

func (m *MMRDecompressor) detectAndSkipEOL() {
	for {
		code := m.getCode(mmrModeTable)
		if code != nil && code.runLength == mmrEOL {
			m.offset += code.bitLength
		} else {
			break
		}
	}
}

func createMMRTable(codes [][]int) []mmrCode {
	table := make([]mmrCode, mmrFirstLevelMask+1)
	for _, codeData := range codes {
		code := mmrCode{
			bitLength: codeData[0],
			codeWord:  codeData[1],
			runLength: codeData[2],
		}

		if code.bitLength <= mmrFirstLevelSize {
			variantLen := mmrFirstLevelSize - code.bitLength
			baseWord := code.codeWord << uint(variantLen)

			for variant := (1 << uint(variantLen)) - 1; variant >= 0; variant-- {
				idx := baseWord | variant
				if idx < len(table) {
					table[idx] = code
				}
			}
		} else {
			firstLevelIdx := code.codeWord >> uint(code.bitLength-mmrFirstLevelSize)
			if firstLevelIdx < len(table) {
				if table[firstLevelIdx].subTable == nil {
					table[firstLevelIdx].subTable = make([]mmrCode, mmrSecondLevelMask+1)
				}

				if code.bitLength <= mmrFirstLevelSize+mmrSecondLevelSize {
					variantLen := mmrFirstLevelSize + mmrSecondLevelSize - code.bitLength
					baseWord := (code.codeWord << uint(variantLen)) & mmrSecondLevelMask

					for variant := (1 << uint(variantLen)) - 1; variant >= 0; variant-- {
						idx := baseWord | variant
						if idx < len(table[firstLevelIdx].subTable) {
							table[firstLevelIdx].subTable[idx] = code
						}
					}
				}
			}
		}
	}
	return table
}
