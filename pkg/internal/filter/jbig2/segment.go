package jbig2

import "fmt"

// Segment types as defined in ISO 14492 section 7.3.
const (
	SegTypeSymbolDictionary      = 0
	SegTypeTextRegion            = 4
	SegTypeImmTextRegion         = 6
	SegTypeImmLosslessTextRegion = 7
	SegTypePatternDictionary     = 16
	SegTypeHalftoneRegion        = 20
	SegTypeImmHalftoneRegion     = 22
	SegTypeImmLosslessHalftone   = 23
	SegTypeGenericRegion         = 36
	SegTypeImmGenericRegion      = 38
	SegTypeImmLosslessGeneric    = 39
	SegTypeRefinementRegion      = 40
	SegTypeImmRefinementRegion   = 42
	SegTypeImmLosslessRefinement = 43
	SegTypePageInformation       = 48
	SegTypeEndOfStripe           = 50
	SegTypeEndOfPage             = 49
	SegTypeEndOfFile             = 51
	SegTypeProfiles              = 52
	SegTypeTable                 = 53
)

// SegmentHeader represents a parsed JBIG2 segment header.
type SegmentHeader struct {
	SegmentNumber   int
	SegmentType     int
	PageAssociation int
	ReferredTo      []*SegmentHeader
	DataOffset      int64
	DataLength      int64
	HeaderLength    int64
}

// RegionSegmentInfo holds the common region segment information field (7.4.1).
type RegionSegmentInfo struct {
	Width  int
	Height int
	XLoc   int
	YLoc   int
	CombOp CombinationOperator
}

// ParseRegionSegmentInfo reads the region segment information field from the reader.
func ParseRegionSegmentInfo(r *Reader) (*RegionSegmentInfo, error) {
	w, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	h, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	x, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	y, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}

	// Skip reserved bits (5 bits).
	if _, err := r.ReadBits(5); err != nil {
		return nil, err
	}

	// Combination operator (3 bits).
	opBits, err := r.ReadBits(3)
	if err != nil {
		return nil, err
	}

	return &RegionSegmentInfo{
		Width:  int(w),
		Height: int(h),
		XLoc:   int(x),
		YLoc:   int(y),
		CombOp: CombinationOperator(opBits),
	}, nil
}

// PageInfo holds the page information segment data (7.4.8).
type PageInfo struct {
	Width             int
	Height            int
	ResolutionX       int
	ResolutionY       int
	DefaultPixelValue int
	CombOp            CombinationOperator
	IsStriped         bool
	MaxStripeSize     int
	CombOpOverrideOK  bool
}

// ParsePageInfo reads a page information segment.
func ParsePageInfo(r *Reader) (*PageInfo, error) {
	w, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	h, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	rx, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	ry, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}

	// Flags byte.
	if _, err := r.ReadBit(); err != nil { // bit 7: reserved
		return nil, err
	}
	combOpOverrideBit, err := r.ReadBit()
	if err != nil {
		return nil, err
	}
	if _, err := r.ReadBit(); err != nil { // bit 5: requires aux buffer
		return nil, err
	}
	combOpBits, err := r.ReadBits(2) // bits 3-4
	if err != nil {
		return nil, err
	}
	defPixBit, err := r.ReadBit() // bit 2
	if err != nil {
		return nil, err
	}
	if _, err := r.ReadBit(); err != nil { // bit 1: might contain refinements
		return nil, err
	}
	if _, err := r.ReadBit(); err != nil { // bit 0: is lossless
		return nil, err
	}

	// Striping info.
	stripedBit, err := r.ReadBit()
	if err != nil {
		return nil, err
	}
	maxStripeBits, err := r.ReadBits(15)
	if err != nil {
		return nil, err
	}

	return &PageInfo{
		Width:             int(w),
		Height:            int(h),
		ResolutionX:       int(rx),
		ResolutionY:       int(ry),
		DefaultPixelValue: defPixBit,
		CombOp:            CombinationOperator(combOpBits),
		IsStriped:         stripedBit == 1,
		MaxStripeSize:     int(maxStripeBits),
		CombOpOverrideOK:  combOpOverrideBit == 1,
	}, nil
}

// ParseSegmentHeaders parses all segment headers from a JBIG2 data stream.
// For PDF embedding, the data has no file header — it's raw segment data.
func ParseSegmentHeaders(data []byte) ([]*SegmentHeader, error) {
	r := NewReader(data)
	var segments []*SegmentHeader
	segmentMap := make(map[int]*SegmentHeader)

	for r.Position() < r.Length()-4 {
		seg, err := parseOneSegmentHeader(r, segmentMap)
		if err != nil {
			// If we can't parse more headers, stop.
			break
		}

		segmentMap[seg.SegmentNumber] = seg
		segments = append(segments, seg)

		if seg.SegmentType == SegTypeEndOfFile {
			break
		}

		// For sequential organization, skip the data part.
		r.SeekTo(seg.DataOffset + seg.DataLength)
	}

	return segments, nil
}

func parseOneSegmentHeader(r *Reader, segMap map[int]*SegmentHeader) (*SegmentHeader, error) {
	startPos := r.Position()

	// 7.2.2 Segment number (32 bits).
	segNum, err := r.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("reading segment number: %w", err)
	}

	// 7.2.3 Segment header flags (8 bits).
	if _, err := r.ReadBit(); err != nil { // bit 7: retain flag
		return nil, err
	}
	pageAssocSizeBit, err := r.ReadBit() // bit 6: page association field size
	if err != nil {
		return nil, err
	}
	segTypeBits, err := r.ReadBits(6) // bits 0-5: segment type
	if err != nil {
		return nil, err
	}

	// 7.2.4 Referred-to segment count.
	countBits, err := r.ReadBits(3)
	if err != nil {
		return nil, err
	}
	countOfRTS := int(countBits)

	if countOfRTS <= 4 {
		// Short format: skip 5 retain bits.
		if _, err := r.ReadBits(5); err != nil {
			return nil, err
		}
	} else {
		// Long format: read 29 more bits for the count.
		longCount, err := r.ReadBits(29)
		if err != nil {
			return nil, err
		}
		countOfRTS = int(longCount)

		// Skip retain bits.
		arrayLen := (countOfRTS + 8) >> 3
		for i := 0; i < arrayLen<<3; i++ {
			if _, err := r.ReadBit(); err != nil {
				return nil, err
			}
		}
	}

	// 7.2.5 Referred-to segment numbers.
	rtsSize := 1
	if segNum > 256 {
		rtsSize = 2
		if segNum > 65536 {
			rtsSize = 4
		}
	}

	rtSegments := make([]*SegmentHeader, countOfRTS)
	for i := 0; i < countOfRTS; i++ {
		rtsNum, err := r.ReadBits(rtsSize * 8)
		if err != nil {
			return nil, err
		}
		if seg, ok := segMap[int(rtsNum)]; ok {
			rtSegments[i] = seg
		}
	}

	// 7.2.6 Page association.
	var pageAssoc int
	if pageAssocSizeBit == 0 {
		bits, err := r.ReadBits(8)
		if err != nil {
			return nil, err
		}
		pageAssoc = int(bits)
	} else {
		bits, err := r.ReadBits(32)
		if err != nil {
			return nil, err
		}
		pageAssoc = int(bits)
	}

	// 7.2.7 Segment data length (32 bits).
	dataLen, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}

	dataOffset := r.Position()
	headerLen := dataOffset - startPos

	return &SegmentHeader{
		SegmentNumber:   int(segNum),
		SegmentType:     int(segTypeBits),
		PageAssociation: pageAssoc,
		ReferredTo:      rtSegments,
		DataOffset:      dataOffset,
		DataLength:      int64(dataLen),
		HeaderLength:    headerLen,
	}, nil
}
