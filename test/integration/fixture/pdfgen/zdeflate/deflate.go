//nolint:gosec // A line-for-line port of C zlib, whose unsigned narrowings are the algorithm: every value is bounded by the window, hash or code-length sizes zlib fixes.
package zdeflate

// Port of zlib 1.2.12 deflate.c, restricted to what zlib.compress uses:
// zlib wrapper, windowBits 15, memLevel 8, Z_DEFAULT_STRATEGY, and the
// Z_NO_FLUSH and Z_FINISH flush modes. Names follow the C sources.

const (
	zNoFlush = 0
	zFinish  = 4

	zOK        = 0
	zStreamEnd = 1
	zBufError  = -5

	zFixed = 4

	initState   = 42
	busyState   = 113
	finishState = 666

	maxWBits    = 15
	defMemLevel = 8

	minLookahead = maxMatch + minMatch + 1
	winInit      = maxMatch
	maxStored    = 65535
	tooFar       = 4096
	presetDict   = 0x20
	nilPos       = 0
)

type blockState int

const (
	needMore blockState = iota
	blockDone
	finishStarted
	finishDone
)

type config struct {
	goodLength, maxLazy, niceLength, maxChain int
	fn                                        func(*state, int) blockState
}

var configurationTable [10]config

func init() {
	configurationTable = [10]config{
		{0, 0, 0, 0, (*state).deflateStored},
		{4, 4, 8, 4, (*state).deflateFast},
		{4, 5, 16, 8, (*state).deflateFast},
		{4, 6, 32, 32, (*state).deflateFast},
		{4, 4, 16, 16, (*state).deflateSlow},
		{8, 16, 32, 32, (*state).deflateSlow},
		{8, 16, 128, 128, (*state).deflateSlow},
		{8, 32, 128, 256, (*state).deflateSlow},
		{32, 128, 258, 1024, (*state).deflateSlow},
		{32, 258, 258, 4096, (*state).deflateSlow},
	}
}

// state merges z_stream and deflate_state.
type state struct {
	// z_stream
	in       []byte
	nextIn   int
	availIn  int
	totalIn  int
	out      []byte
	nextOut  int
	availOut int
	adler    uint32

	status         int
	pendingBuf     []byte
	pendingBufSize int
	pendingOut     int
	pending        int
	wrap           int
	lastFlush      int

	wSize      int
	wBits      int
	wMask      int
	window     []byte
	windowSize int
	prev       []uint16
	head       []uint16

	insH      uint
	hashSize  int
	hashBits  uint
	hashMask  uint
	hashShift uint

	blockStart int

	matchLength    int
	prevMatch      uint32
	matchAvailable bool
	strstart       int
	matchStart     uint32 // uInt in C: fill_window may wrap it below zero
	lookahead      int
	prevLength     int

	maxChainLength int
	maxLazyMatch   int
	level          int
	strategy       int
	goodMatch      int
	niceMatch      int

	dynLtree [heapSize]ctData
	dynDtree [2*dCodes + 1]ctData
	blTree   [2*blCodes + 1]ctData

	lDesc  treeDesc
	dDesc  treeDesc
	blDesc treeDesc

	blCount [maxBits + 1]uint16
	heap    [2*lCodes + 1]int
	heapLen int
	heapMax int
	depth   [2*lCodes + 1]uint8

	// sym_buf overlays pending_buf in C; zlib proves the overlay never
	// clobbers unread symbols, so a separate buffer is equivalent.
	symBuf     []byte
	litBufsize int
	symNext    int
	symEnd     int

	optLen    uint64
	staticLen uint64
	matches   int
	insert    int

	biBuf   uint16
	biValid int

	highWater int
}

func adler32Update(adler uint32, buf []byte) uint32 {
	const base = 65521
	const nmax = 5552
	s1, s2 := adler&0xffff, adler>>16
	for len(buf) > 0 {
		n := min(len(buf), nmax)
		for _, b := range buf[:n] {
			s1 += uint32(b)
			s2 += s1
		}
		s1 %= base
		s2 %= base
		buf = buf[n:]
	}
	return s2<<16 | s1
}

// newState is deflateInit2 with method Z_DEFLATED, windowBits 15, memLevel 8
// and Z_DEFAULT_STRATEGY, followed by deflateReset.
func newState(level int) *state {
	if level == -1 {
		level = 6
	}
	s := &state{}
	s.wrap = 1
	s.wBits = maxWBits
	s.wSize = 1 << s.wBits
	s.wMask = s.wSize - 1

	s.hashBits = defMemLevel + 7
	s.hashSize = 1 << s.hashBits
	s.hashMask = uint(s.hashSize - 1)
	s.hashShift = (s.hashBits + minMatch - 1) / minMatch

	s.window = make([]byte, 2*s.wSize)
	s.prev = make([]uint16, s.wSize)
	s.head = make([]uint16, s.hashSize)
	s.highWater = 0

	s.litBufsize = 1 << (defMemLevel + 6)
	s.pendingBufSize = s.litBufsize * 4
	s.pendingBuf = make([]byte, s.pendingBufSize)
	s.symBuf = make([]byte, s.litBufsize*3)
	s.symEnd = (s.litBufsize - 1) * 3

	s.level = level
	s.strategy = 0

	// deflateResetKeep
	s.pending = 0
	s.pendingOut = 0
	s.status = initState
	s.adler = 1
	s.lastFlush = -2
	s.trInit()

	s.lmInit()
	return s
}

func (s *state) lmInit() {
	s.windowSize = 2 * s.wSize
	for i := range s.head {
		s.head[i] = nilPos
	}
	c := configurationTable[s.level]
	s.maxLazyMatch = c.maxLazy
	s.goodMatch = c.goodLength
	s.niceMatch = c.niceLength
	s.maxChainLength = c.maxChain

	s.strstart = 0
	s.blockStart = 0
	s.lookahead = 0
	s.insert = 0
	s.matchLength = minMatch - 1
	s.prevLength = minMatch - 1
	s.matchAvailable = false
	s.insH = 0
}

func (s *state) updateHash(h uint, c byte) uint {
	return ((h << s.hashShift) ^ uint(c)) & s.hashMask
}

// insertString is INSERT_STRING; it returns the previous head of the chain.
func (s *state) insertString(str int) int {
	s.insH = s.updateHash(s.insH, s.window[str+minMatch-1])
	h := s.head[s.insH]
	s.prev[str&s.wMask] = h
	s.head[s.insH] = uint16(str)
	return int(h)
}

func (s *state) slideHash() {
	wsize := s.wSize
	for i, m := range s.head {
		if int(m) >= wsize {
			s.head[i] = uint16(int(m) - wsize)
		} else {
			s.head[i] = nilPos
		}
	}
	for i, m := range s.prev {
		if int(m) >= wsize {
			s.prev[i] = uint16(int(m) - wsize)
		} else {
			s.prev[i] = nilPos
		}
	}
}

func (s *state) putShortMSB(b uint32) {
	s.putByte(byte(b >> 8))
	s.putByte(byte(b))
}

func (s *state) flushPending() {
	s.trFlushBits()
	n := min(s.pending, s.availOut)
	if n == 0 {
		return
	}
	copy(s.out[s.nextOut:], s.pendingBuf[s.pendingOut:s.pendingOut+n])
	s.nextOut += n
	s.pendingOut += n
	s.availOut -= n
	s.pending -= n
	if s.pending == 0 {
		s.pendingOut = 0
	}
}

func rank(f int) int {
	r := f * 2
	if f > 4 {
		r -= 9
	}
	return r
}

// deflate is deflate() for flush in {Z_NO_FLUSH, Z_FINISH}.
func (s *state) deflate(flush int) int {
	if s.availOut == 0 {
		return zBufError
	}
	oldFlush := s.lastFlush
	s.lastFlush = flush

	if s.pending != 0 {
		s.flushPending()
		if s.availOut == 0 {
			s.lastFlush = -1
			return zOK
		}
	} else if s.availIn == 0 && rank(flush) <= rank(oldFlush) && flush != zFinish {
		return zBufError
	}

	if s.status == finishState && s.availIn != 0 {
		return zBufError
	}

	if s.status == initState {
		header := uint32(8+((s.wBits-8)<<4)) << 8
		var levelFlags uint32
		switch {
		case s.level < 2:
			levelFlags = 0
		case s.level < 6:
			levelFlags = 1
		case s.level == 6:
			levelFlags = 2
		default:
			levelFlags = 3
		}
		header |= levelFlags << 6
		if s.strstart != 0 {
			header |= presetDict
		}
		header += 31 - (header % 31)
		s.putShortMSB(header)
		s.adler = 1
		s.status = busyState
		s.flushPending()
		if s.pending != 0 {
			s.lastFlush = -1
			return zOK
		}
	}

	if s.availIn != 0 || s.lookahead != 0 || (flush != zNoFlush && s.status != finishState) {
		bstate := configurationTable[s.level].fn(s, flush)
		if bstate == finishStarted || bstate == finishDone {
			s.status = finishState
		}
		if bstate == needMore || bstate == finishStarted {
			if s.availOut == 0 {
				s.lastFlush = -1
			}
			return zOK
		}
		// block_done only follows Z_PARTIAL_FLUSH, Z_SYNC_FLUSH,
		// Z_FULL_FLUSH or Z_BLOCK, which this port does not support.
	}

	if flush != zFinish {
		return zOK
	}
	if s.wrap <= 0 {
		return zStreamEnd
	}
	s.putShortMSB(s.adler >> 16)
	s.putShortMSB(s.adler & 0xffff)
	s.flushPending()
	if s.wrap > 0 {
		s.wrap = -s.wrap
	}
	if s.pending != 0 {
		return zOK
	}
	return zStreamEnd
}

func (s *state) readBuf(dst []byte, size int) int {
	n := min(s.availIn, size)
	if n == 0 {
		return 0
	}
	s.availIn -= n
	copy(dst[:n], s.in[s.nextIn:s.nextIn+n])
	if s.wrap == 1 {
		s.adler = adler32Update(s.adler, dst[:n])
	}
	s.nextIn += n
	s.totalIn += n
	return n
}

func (s *state) maxDist() int {
	return s.wSize - minLookahead
}

func (s *state) longestMatch(curMatch int) int {
	chainLength := s.maxChainLength
	win := s.window
	scan := s.strstart
	bestLen := s.prevLength
	niceMatch := s.niceMatch
	limit := nilPos
	if s.strstart > s.maxDist() {
		limit = s.strstart - s.maxDist()
	}
	wmask := s.wMask

	scanEnd1 := win[scan+bestLen-1]
	scanEnd := win[scan+bestLen]

	if s.prevLength >= s.goodMatch {
		chainLength >>= 2
	}
	if niceMatch > s.lookahead {
		niceMatch = s.lookahead
	}

	for {
		match := curMatch
		if win[match+bestLen] == scanEnd && win[match+bestLen-1] == scanEnd1 &&
			win[match] == win[scan] && win[match+1] == win[scan+1] {
			// scan[2] == match[2] is implied by the hash; C compares from
			// offset 3 up to offset 258 and the result is the same as this
			// loop's.
			l := 3
			for l < maxMatch && win[scan+l] == win[match+l] {
				l++
			}
			if l > bestLen {
				s.matchStart = uint32(curMatch)
				bestLen = l
				if l >= niceMatch {
					break
				}
				scanEnd1 = win[scan+bestLen-1]
				scanEnd = win[scan+bestLen]
			}
		}
		curMatch = int(s.prev[curMatch&wmask])
		if curMatch <= limit {
			break
		}
		chainLength--
		if chainLength == 0 {
			break
		}
	}

	if bestLen <= s.lookahead {
		return bestLen
	}
	return s.lookahead
}

func (s *state) fillWindow() {
	wsize := s.wSize
	for {
		more := s.windowSize - s.lookahead - s.strstart

		if s.strstart >= wsize+s.maxDist() {
			copy(s.window[:wsize-more], s.window[wsize:wsize+wsize-more])
			s.matchStart -= uint32(wsize)
			s.strstart -= wsize
			s.blockStart -= wsize
			if s.insert > s.strstart {
				s.insert = s.strstart
			}
			s.slideHash()
			more += wsize
		}
		if s.availIn == 0 {
			break
		}

		n := s.readBuf(s.window[s.strstart+s.lookahead:], more)
		s.lookahead += n

		if s.lookahead+s.insert >= minMatch {
			str := s.strstart - s.insert
			s.insH = uint(s.window[str])
			s.insH = s.updateHash(s.insH, s.window[str+1])
			for s.insert != 0 {
				s.insH = s.updateHash(s.insH, s.window[str+minMatch-1])
				s.prev[str&s.wMask] = s.head[s.insH]
				s.head[s.insH] = uint16(str)
				str++
				s.insert--
				if s.lookahead+s.insert < minMatch {
					break
				}
			}
		}

		if s.lookahead >= minLookahead || s.availIn == 0 {
			break
		}
	}

	if s.highWater < s.windowSize {
		curr := s.strstart + s.lookahead
		if s.highWater < curr {
			init := min(s.windowSize-curr, winInit)
			clear(s.window[curr : curr+init])
			s.highWater = curr + init
		} else if s.highWater < curr+winInit {
			init := min(curr+winInit-s.highWater, s.windowSize-s.highWater)
			clear(s.window[s.highWater : s.highWater+init])
			s.highWater += init
		}
	}
}

func (s *state) flushBlockOnly(last int) {
	if s.blockStart >= 0 {
		s.trFlushBlock(s.window[s.blockStart:], true, s.strstart-s.blockStart, last)
	} else {
		s.trFlushBlock(nil, false, s.strstart-s.blockStart, last)
	}
	s.blockStart = s.strstart
	s.flushPending()
}

func (s *state) deflateStored(flush int) blockState {
	minBlock := min(s.pendingBufSize-5, s.wSize)
	var length, left, have int
	last := 0
	used := s.availIn
	for {
		length = maxStored
		have = (s.biValid + 42) >> 3
		if s.availOut < have {
			break
		}
		have = s.availOut - have
		left = s.strstart - s.blockStart
		if length > left+s.availIn {
			length = left + s.availIn
		}
		if length > have {
			length = have
		}
		if length < minBlock && ((length == 0 && flush != zFinish) ||
			flush == zNoFlush ||
			length != left+s.availIn) {
			break
		}
		last = 0
		if flush == zFinish && length == left+s.availIn {
			last = 1
		}
		s.trStoredBlock(nil, 0, last)
		s.pendingBuf[s.pending-4] = byte(length)
		s.pendingBuf[s.pending-3] = byte(length >> 8)
		s.pendingBuf[s.pending-2] = ^byte(length)
		s.pendingBuf[s.pending-1] = ^byte(length >> 8)
		s.flushPending()
		if left != 0 {
			if left > length {
				left = length
			}
			copy(s.out[s.nextOut:], s.window[s.blockStart:s.blockStart+left])
			s.nextOut += left
			s.availOut -= left
			s.blockStart += left
			length -= left
		}
		if length != 0 {
			s.readBuf(s.out[s.nextOut:], length)
			s.nextOut += length
			s.availOut -= length
		}
		if last != 0 {
			break
		}
	}

	used -= s.availIn
	if used != 0 {
		if used >= s.wSize {
			s.matches = 2
			copy(s.window[:s.wSize], s.in[s.nextIn-s.wSize:s.nextIn])
			s.strstart = s.wSize
			s.insert = s.strstart
		} else {
			if s.windowSize-s.strstart <= used {
				s.strstart -= s.wSize
				copy(s.window[:s.strstart], s.window[s.wSize:s.wSize+s.strstart])
				if s.matches < 2 {
					s.matches++
				}
				if s.insert > s.strstart {
					s.insert = s.strstart
				}
			}
			copy(s.window[s.strstart:], s.in[s.nextIn-used:s.nextIn])
			s.strstart += used
			s.insert += min(used, s.wSize-s.insert)
		}
		s.blockStart = s.strstart
	}
	if s.highWater < s.strstart {
		s.highWater = s.strstart
	}

	if last != 0 {
		return finishDone
	}

	// The block_done case needs a flush other than Z_NO_FLUSH or Z_FINISH.

	have = s.windowSize - s.strstart
	if s.availIn > have && s.blockStart >= s.wSize {
		s.blockStart -= s.wSize
		s.strstart -= s.wSize
		copy(s.window[:s.strstart], s.window[s.wSize:s.wSize+s.strstart])
		if s.matches < 2 {
			s.matches++
		}
		have += s.wSize
		if s.insert > s.strstart {
			s.insert = s.strstart
		}
	}
	if have > s.availIn {
		have = s.availIn
	}
	if have != 0 {
		s.readBuf(s.window[s.strstart:], have)
		s.strstart += have
		s.insert += min(have, s.wSize-s.insert)
	}
	if s.highWater < s.strstart {
		s.highWater = s.strstart
	}

	have = (s.biValid + 42) >> 3
	have = min(s.pendingBufSize-have, maxStored)
	minBlock = min(have, s.wSize)
	left = s.strstart - s.blockStart
	if left >= minBlock ||
		((left != 0 || flush == zFinish) && flush != zNoFlush &&
			s.availIn == 0 && left <= have) {
		length = min(left, have)
		last = 0
		if flush == zFinish && s.availIn == 0 && length == left {
			last = 1
		}
		s.trStoredBlock(s.window[s.blockStart:], length, last)
		s.blockStart += length
		s.flushPending()
	}
	if last != 0 {
		return finishStarted
	}
	return needMore
}

func (s *state) deflateFast(flush int) blockState {
	for {
		if s.lookahead < minLookahead {
			s.fillWindow()
			if s.lookahead < minLookahead && flush == zNoFlush {
				return needMore
			}
			if s.lookahead == 0 {
				break
			}
		}

		hashHead := nilPos
		if s.lookahead >= minMatch {
			hashHead = s.insertString(s.strstart)
		}

		if hashHead != nilPos && s.strstart-hashHead <= s.maxDist() {
			s.matchLength = s.longestMatch(hashHead)
		}
		var bflush bool
		if s.matchLength >= minMatch {
			bflush = s.tallyDist(int(uint32(s.strstart)-s.matchStart), s.matchLength-minMatch)
			s.lookahead -= s.matchLength

			if s.matchLength <= s.maxLazyMatch && s.lookahead >= minMatch {
				s.matchLength--
				for {
					s.strstart++
					s.insertString(s.strstart)
					s.matchLength--
					if s.matchLength == 0 {
						break
					}
				}
				s.strstart++
			} else {
				s.strstart += s.matchLength
				s.matchLength = 0
				s.insH = uint(s.window[s.strstart])
				s.insH = s.updateHash(s.insH, s.window[s.strstart+1])
			}
		} else {
			bflush = s.tallyLit(s.window[s.strstart])
			s.lookahead--
			s.strstart++
		}
		if bflush {
			s.flushBlockOnly(0)
			if s.availOut == 0 {
				return needMore
			}
		}
	}
	s.insert = min(s.strstart, minMatch-1)
	if flush == zFinish {
		s.flushBlockOnly(1)
		if s.availOut == 0 {
			return finishStarted
		}
		return finishDone
	}
	if s.symNext != 0 {
		s.flushBlockOnly(0)
		if s.availOut == 0 {
			return needMore
		}
	}
	return blockDone
}

func (s *state) deflateSlow(flush int) blockState {
	for {
		if s.lookahead < minLookahead {
			s.fillWindow()
			if s.lookahead < minLookahead && flush == zNoFlush {
				return needMore
			}
			if s.lookahead == 0 {
				break
			}
		}

		hashHead := nilPos
		if s.lookahead >= minMatch {
			hashHead = s.insertString(s.strstart)
		}

		s.prevLength = s.matchLength
		s.prevMatch = s.matchStart
		s.matchLength = minMatch - 1

		if hashHead != nilPos && s.prevLength < s.maxLazyMatch &&
			s.strstart-hashHead <= s.maxDist() {
			s.matchLength = s.longestMatch(hashHead)
			if s.matchLength <= 5 && (s.strategy == 1 ||
				(s.matchLength == minMatch && uint32(s.strstart)-s.matchStart > tooFar)) {
				s.matchLength = minMatch - 1
			}
		}

		switch {
		case s.prevLength >= minMatch && s.matchLength <= s.prevLength:
			maxInsert := s.strstart + s.lookahead - minMatch
			bflush := s.tallyDist(int(uint32(s.strstart)-1-s.prevMatch), s.prevLength-minMatch)
			s.lookahead -= s.prevLength - 1
			s.prevLength -= 2
			for {
				s.strstart++
				if s.strstart <= maxInsert {
					s.insertString(s.strstart)
				}
				s.prevLength--
				if s.prevLength == 0 {
					break
				}
			}
			s.matchAvailable = false
			s.matchLength = minMatch - 1
			s.strstart++
			if bflush {
				s.flushBlockOnly(0)
				if s.availOut == 0 {
					return needMore
				}
			}
		case s.matchAvailable:
			if s.tallyLit(s.window[s.strstart-1]) {
				s.flushBlockOnly(0)
			}
			s.strstart++
			s.lookahead--
			if s.availOut == 0 {
				return needMore
			}
		default:
			s.matchAvailable = true
			s.strstart++
			s.lookahead--
		}
	}
	if s.matchAvailable {
		s.tallyLit(s.window[s.strstart-1])
		s.matchAvailable = false
	}
	s.insert = min(s.strstart, minMatch-1)
	if flush == zFinish {
		s.flushBlockOnly(1)
		if s.availOut == 0 {
			return finishStarted
		}
		return finishDone
	}
	if s.symNext != 0 {
		s.flushBlockOnly(0)
		if s.availOut == 0 {
			return needMore
		}
	}
	return blockDone
}
