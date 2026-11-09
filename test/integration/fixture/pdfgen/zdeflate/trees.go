//nolint:gosec // A line-for-line port of C zlib, whose unsigned narrowings are the algorithm: every value is bounded by the window, hash or code-length sizes zlib fixes.
package zdeflate

// Port of zlib 1.2.12 trees.c: Huffman tree construction, block emission and
// the 16-bit bit buffer. Names follow the C sources.

const (
	maxBits     = 15
	maxBLBits   = 7
	lengthCodes = 29
	literals    = 256
	lCodes      = literals + 1 + lengthCodes
	dCodes      = 30
	blCodes     = 19
	heapSize    = 2*lCodes + 1
	endBlock    = 256
	rep3_6      = 16
	repz3_10    = 17
	repz11_138  = 18
	bufSize     = 16

	storedBlock = 0
	staticTrees = 1
	dynTrees    = 2

	minMatch = 3
	maxMatch = 258

	smallest = 1
)

var extraLbits = [lengthCodes]int{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0}

var extraDbits = [dCodes]int{0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13}

var extraBlbits = [blCodes]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 3, 7}

var blOrder = [blCodes]int{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}

// ctData mirrors zlib's ct_data, whose two fields are unions: fc holds Freq
// or Code, dl holds Dad or Len. The aliasing is kept on purpose.
type ctData struct {
	fc uint16
	dl uint16
}

type staticTreeDesc struct {
	staticTree []ctData
	extraBits  []int
	extraBase  int
	elems      int
	maxLength  int
}

type treeDesc struct {
	dynTree  []ctData
	maxCode  int
	statDesc *staticTreeDesc
}

var (
	staticLtree [lCodes + 2]ctData
	staticDtree [dCodes]ctData
	distCode    [512]uint8
	lengthCode  [maxMatch - minMatch + 1]uint8
	baseLength  [lengthCodes]int
	baseDist    [dCodes]int

	staticLDesc  = staticTreeDesc{staticLtree[:], extraLbits[:], literals + 1, lCodes, maxBits}
	staticDDesc  = staticTreeDesc{staticDtree[:], extraDbits[:], 0, dCodes, maxBits}
	staticBLDesc = staticTreeDesc{nil, extraBlbits[:], 0, blCodes, maxBLBits}
)

// init is tr_static_init.
func init() {
	length := 0
	var code int
	for code = 0; code < lengthCodes-1; code++ {
		baseLength[code] = length
		for n := 0; n < 1<<extraLbits[code]; n++ {
			lengthCode[length] = uint8(code)
			length++
		}
	}
	lengthCode[length-1] = uint8(code)

	dist := 0
	for code = 0; code < 16; code++ {
		baseDist[code] = dist
		for n := 0; n < 1<<extraDbits[code]; n++ {
			distCode[dist] = uint8(code)
			dist++
		}
	}
	dist >>= 7
	for ; code < dCodes; code++ {
		baseDist[code] = dist << 7
		for n := 0; n < 1<<(extraDbits[code]-7); n++ {
			distCode[256+dist] = uint8(code)
			dist++
		}
	}

	var blCount [maxBits + 1]uint16
	n := 0
	for n <= 143 {
		staticLtree[n].dl = 8
		n++
		blCount[8]++
	}
	for n <= 255 {
		staticLtree[n].dl = 9
		n++
		blCount[9]++
	}
	for n <= 279 {
		staticLtree[n].dl = 7
		n++
		blCount[7]++
	}
	for n <= 287 {
		staticLtree[n].dl = 8
		n++
		blCount[8]++
	}
	genCodes(staticLtree[:], lCodes+1, blCount[:])

	for n := 0; n < dCodes; n++ {
		staticDtree[n].dl = 5
		staticDtree[n].fc = uint16(biReverse(uint(n), 5))
	}
}

func dCode(dist int) int {
	if dist < 256 {
		return int(distCode[dist])
	}
	return int(distCode[256+(dist>>7)])
}

func (s *state) trInit() {
	s.lDesc = treeDesc{dynTree: s.dynLtree[:], statDesc: &staticLDesc}
	s.dDesc = treeDesc{dynTree: s.dynDtree[:], statDesc: &staticDDesc}
	s.blDesc = treeDesc{dynTree: s.blTree[:], statDesc: &staticBLDesc}
	s.biBuf = 0
	s.biValid = 0
	s.initBlock()
}

func (s *state) initBlock() {
	for n := 0; n < lCodes; n++ {
		s.dynLtree[n].fc = 0
	}
	for n := 0; n < dCodes; n++ {
		s.dynDtree[n].fc = 0
	}
	for n := 0; n < blCodes; n++ {
		s.blTree[n].fc = 0
	}
	s.dynLtree[endBlock].fc = 1
	s.optLen = 0
	s.staticLen = 0
	s.symNext = 0
	s.matches = 0
}

func (s *state) putByte(c byte) {
	s.pendingBuf[s.pending] = c
	s.pending++
}

func (s *state) putShort(w uint16) {
	s.putByte(byte(w))
	s.putByte(byte(w >> 8))
}

func (s *state) sendBits(value, length int) {
	if s.biValid > bufSize-length {
		s.biBuf |= uint16(value) << uint(s.biValid)
		s.putShort(s.biBuf)
		s.biBuf = uint16(value) >> uint(bufSize-s.biValid)
		s.biValid += length - bufSize
	} else {
		s.biBuf |= uint16(value) << uint(s.biValid)
		s.biValid += length
	}
}

func (s *state) sendCode(c int, tree []ctData) {
	s.sendBits(int(tree[c].fc), int(tree[c].dl))
}

func smaller(tree []ctData, n, m int, depth []uint8) bool {
	return tree[n].fc < tree[m].fc || (tree[n].fc == tree[m].fc && depth[n] <= depth[m])
}

func (s *state) pqdownheap(tree []ctData, k int) {
	v := s.heap[k]
	j := k << 1
	for j <= s.heapLen {
		if j < s.heapLen && smaller(tree, s.heap[j+1], s.heap[j], s.depth[:]) {
			j++
		}
		if smaller(tree, v, s.heap[j], s.depth[:]) {
			break
		}
		s.heap[k] = s.heap[j]
		k = j
		j <<= 1
	}
	s.heap[k] = v
}

func (s *state) genBitlen(desc *treeDesc) {
	tree := desc.dynTree
	maxCode := desc.maxCode
	stree := desc.statDesc.staticTree
	extra := desc.statDesc.extraBits
	base := desc.statDesc.extraBase
	maxLength := desc.statDesc.maxLength
	overflow := 0

	for bits := 0; bits <= maxBits; bits++ {
		s.blCount[bits] = 0
	}

	tree[s.heap[s.heapMax]].dl = 0

	var h int
	for h = s.heapMax + 1; h < heapSize; h++ {
		n := s.heap[h]
		bits := int(tree[tree[n].dl].dl) + 1
		if bits > maxLength {
			bits = maxLength
			overflow++
		}
		tree[n].dl = uint16(bits)
		if n > maxCode {
			continue
		}
		s.blCount[bits]++
		xbits := 0
		if n >= base {
			xbits = extra[n-base]
		}
		f := tree[n].fc
		s.optLen += uint64(f) * uint64(bits+xbits)
		if stree != nil {
			s.staticLen += uint64(f) * uint64(int(stree[n].dl)+xbits)
		}
	}
	if overflow == 0 {
		return
	}

	for {
		bits := maxLength - 1
		for s.blCount[bits] == 0 {
			bits--
		}
		s.blCount[bits]--
		s.blCount[bits+1] += 2
		s.blCount[maxLength]--
		overflow -= 2
		if overflow <= 0 {
			break
		}
	}

	for bits := maxLength; bits != 0; bits-- {
		n := int(s.blCount[bits])
		for n != 0 {
			h--
			m := s.heap[h]
			if m > maxCode {
				continue
			}
			if int(tree[m].dl) != bits {
				s.optLen += uint64(int64(bits)-int64(tree[m].dl)) * uint64(tree[m].fc)
				tree[m].dl = uint16(bits)
			}
			n--
		}
	}
}

func genCodes(tree []ctData, maxCode int, blCount []uint16) {
	var nextCode [maxBits + 1]uint16
	code := uint(0)
	for bits := 1; bits <= maxBits; bits++ {
		code = (code + uint(blCount[bits-1])) << 1
		nextCode[bits] = uint16(code)
	}
	for n := 0; n <= maxCode; n++ {
		l := int(tree[n].dl)
		if l == 0 {
			continue
		}
		tree[n].fc = uint16(biReverse(uint(nextCode[l]), l))
		nextCode[l]++
	}
}

func (s *state) buildTree(desc *treeDesc) {
	tree := desc.dynTree
	stree := desc.statDesc.staticTree
	elems := desc.statDesc.elems
	maxCode := -1

	s.heapLen = 0
	s.heapMax = heapSize

	for n := 0; n < elems; n++ {
		if tree[n].fc != 0 {
			s.heapLen++
			s.heap[s.heapLen] = n
			maxCode = n
			s.depth[n] = 0
		} else {
			tree[n].dl = 0
		}
	}

	for s.heapLen < 2 {
		var node int
		if maxCode < 2 {
			maxCode++
			node = maxCode
		} else {
			node = 0
		}
		s.heapLen++
		s.heap[s.heapLen] = node
		tree[node].fc = 1
		s.depth[node] = 0
		s.optLen--
		if stree != nil {
			s.staticLen -= uint64(stree[node].dl)
		}
	}
	desc.maxCode = maxCode

	for n := s.heapLen / 2; n >= 1; n-- {
		s.pqdownheap(tree, n)
	}

	node := elems
	for {
		// pqremove
		n := s.heap[smallest]
		s.heap[smallest] = s.heap[s.heapLen]
		s.heapLen--
		s.pqdownheap(tree, smallest)

		m := s.heap[smallest]

		s.heapMax--
		s.heap[s.heapMax] = n
		s.heapMax--
		s.heap[s.heapMax] = m

		tree[node].fc = tree[n].fc + tree[m].fc
		dn, dm := s.depth[n], s.depth[m]
		if dn >= dm {
			s.depth[node] = dn + 1
		} else {
			s.depth[node] = dm + 1
		}
		tree[n].dl = uint16(node)
		tree[m].dl = uint16(node)

		s.heap[smallest] = node
		node++
		s.pqdownheap(tree, smallest)

		if s.heapLen < 2 {
			break
		}
	}

	s.heapMax--
	s.heap[s.heapMax] = s.heap[smallest]

	s.genBitlen(desc)
	genCodes(tree, maxCode, s.blCount[:])
}

func (s *state) scanTree(tree []ctData, maxCode int) {
	prevlen := -1
	nextlen := int(tree[0].dl)
	count := 0
	maxCount := 7
	minCount := 4
	if nextlen == 0 {
		maxCount, minCount = 138, 3
	}
	tree[maxCode+1].dl = 0xffff
	for n := 0; n <= maxCode; n++ {
		curlen := nextlen
		nextlen = int(tree[n+1].dl)
		count++
		switch {
		case count < maxCount && curlen == nextlen:
			continue
		case count < minCount:
			s.blTree[curlen].fc += uint16(count)
		case curlen != 0:
			if curlen != prevlen {
				s.blTree[curlen].fc++
			}
			s.blTree[rep3_6].fc++
		case count <= 10:
			s.blTree[repz3_10].fc++
		default:
			s.blTree[repz11_138].fc++
		}
		count = 0
		prevlen = curlen
		switch {
		case nextlen == 0:
			maxCount, minCount = 138, 3
		case curlen == nextlen:
			maxCount, minCount = 6, 3
		default:
			maxCount, minCount = 7, 4
		}
	}
}

func (s *state) sendTree(tree []ctData, maxCode int) {
	prevlen := -1
	nextlen := int(tree[0].dl)
	count := 0
	maxCount := 7
	minCount := 4
	if nextlen == 0 {
		maxCount, minCount = 138, 3
	}
	for n := 0; n <= maxCode; n++ {
		curlen := nextlen
		nextlen = int(tree[n+1].dl)
		count++
		switch {
		case count < maxCount && curlen == nextlen:
			continue
		case count < minCount:
			for {
				s.sendCode(curlen, s.blTree[:])
				count--
				if count == 0 {
					break
				}
			}
		case curlen != 0:
			if curlen != prevlen {
				s.sendCode(curlen, s.blTree[:])
				count--
			}
			s.sendCode(rep3_6, s.blTree[:])
			s.sendBits(count-3, 2)
		case count <= 10:
			s.sendCode(repz3_10, s.blTree[:])
			s.sendBits(count-3, 3)
		default:
			s.sendCode(repz11_138, s.blTree[:])
			s.sendBits(count-11, 7)
		}
		count = 0
		prevlen = curlen
		switch {
		case nextlen == 0:
			maxCount, minCount = 138, 3
		case curlen == nextlen:
			maxCount, minCount = 6, 3
		default:
			maxCount, minCount = 7, 4
		}
	}
}

func (s *state) buildBLTree() int {
	s.scanTree(s.dynLtree[:], s.lDesc.maxCode)
	s.scanTree(s.dynDtree[:], s.dDesc.maxCode)
	s.buildTree(&s.blDesc)
	maxBlindex := blCodes - 1
	for ; maxBlindex >= 3; maxBlindex-- {
		if s.blTree[blOrder[maxBlindex]].dl != 0 {
			break
		}
	}
	s.optLen += 3*(uint64(maxBlindex)+1) + 5 + 5 + 4
	return maxBlindex
}

func (s *state) sendAllTrees(lcodes, dcodes, blcodes int) {
	s.sendBits(lcodes-257, 5)
	s.sendBits(dcodes-1, 5)
	s.sendBits(blcodes-4, 4)
	for rank := 0; rank < blcodes; rank++ {
		s.sendBits(int(s.blTree[blOrder[rank]].dl), 3)
	}
	s.sendTree(s.dynLtree[:], lcodes-1)
	s.sendTree(s.dynDtree[:], dcodes-1)
}

// trStoredBlock is _tr_stored_block. buf may be nil when storedLen is 0.
func (s *state) trStoredBlock(buf []byte, storedLen int, last int) {
	s.sendBits((storedBlock<<1)+last, 3)
	s.biWindup()
	s.putShort(uint16(storedLen))
	s.putShort(^uint16(storedLen))
	if storedLen != 0 {
		copy(s.pendingBuf[s.pending:], buf[:storedLen])
	}
	s.pending += storedLen
}

func (s *state) trFlushBits() {
	s.biFlush()
}

// trFlushBlock is _tr_flush_block. haveBuf is false when C passes a NULL buf
// (block_start negative, the block data is no longer in the window).
func (s *state) trFlushBlock(buf []byte, haveBuf bool, storedLen int, last int) {
	var optLenb, staticLenb uint64
	maxBlindex := 0

	if s.level > 0 {
		// detect_data_type only sets strm->data_type; it does not affect
		// the output.
		s.buildTree(&s.lDesc)
		s.buildTree(&s.dDesc)
		maxBlindex = s.buildBLTree()
		optLenb = (s.optLen + 3 + 7) >> 3
		staticLenb = (s.staticLen + 3 + 7) >> 3
		if staticLenb <= optLenb {
			optLenb = staticLenb
		}
	} else {
		optLenb = uint64(storedLen) + 5
		staticLenb = optLenb
	}

	switch {
	case uint64(storedLen)+4 <= optLenb && haveBuf:
		s.trStoredBlock(buf, storedLen, last)
	case s.strategy == zFixed || staticLenb == optLenb:
		s.sendBits((staticTrees<<1)+last, 3)
		s.compressBlock(staticLtree[:], staticDtree[:])
	default:
		s.sendBits((dynTrees<<1)+last, 3)
		s.sendAllTrees(s.lDesc.maxCode+1, s.dDesc.maxCode+1, maxBlindex+1)
		s.compressBlock(s.dynLtree[:], s.dynDtree[:])
	}
	s.initBlock()
	if last != 0 {
		s.biWindup()
	}
}

func (s *state) tallyLit(c byte) bool {
	s.symBuf[s.symNext] = 0
	s.symBuf[s.symNext+1] = 0
	s.symBuf[s.symNext+2] = c
	s.symNext += 3
	s.dynLtree[c].fc++
	return s.symNext == s.symEnd
}

func (s *state) tallyDist(distance, length int) bool {
	l := uint8(length)
	dist := uint16(distance)
	s.symBuf[s.symNext] = byte(dist)
	s.symBuf[s.symNext+1] = byte(dist >> 8)
	s.symBuf[s.symNext+2] = l
	s.symNext += 3
	dist--
	s.dynLtree[int(lengthCode[l])+literals+1].fc++
	s.dynDtree[dCode(int(dist))].fc++
	return s.symNext == s.symEnd
}

func (s *state) compressBlock(ltree, dtree []ctData) {
	sx := 0
	if s.symNext != 0 {
		for {
			dist := int(s.symBuf[sx]) | int(s.symBuf[sx+1])<<8
			lc := int(s.symBuf[sx+2])
			sx += 3
			if dist == 0 {
				s.sendCode(lc, ltree)
			} else {
				code := int(lengthCode[lc])
				s.sendCode(code+literals+1, ltree)
				extra := extraLbits[code]
				if extra != 0 {
					lc -= baseLength[code]
					s.sendBits(lc, extra)
				}
				dist--
				code = dCode(dist)
				s.sendCode(code, dtree)
				extra = extraDbits[code]
				if extra != 0 {
					dist -= baseDist[code]
					s.sendBits(dist, extra)
				}
			}
			if sx >= s.symNext {
				break
			}
		}
	}
	s.sendCode(endBlock, ltree)
}

func biReverse(code uint, length int) uint {
	res := uint(0)
	for {
		res |= code & 1
		code >>= 1
		res <<= 1
		length--
		if length <= 0 {
			break
		}
	}
	return res >> 1
}

func (s *state) biFlush() {
	if s.biValid == 16 {
		s.putShort(s.biBuf)
		s.biBuf = 0
		s.biValid = 0
	} else if s.biValid >= 8 {
		s.putByte(byte(s.biBuf))
		s.biBuf >>= 8
		s.biValid -= 8
	}
}

func (s *state) biWindup() {
	if s.biValid > 8 {
		s.putShort(s.biBuf)
	} else if s.biValid > 0 {
		s.putByte(byte(s.biBuf))
	}
	s.biBuf = 0
	s.biValid = 0
}
