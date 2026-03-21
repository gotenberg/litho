package jbig2

import "fmt"

// Document represents a parsed JBIG2 document with pages and global segments.
type Document struct {
	pages          map[int]*Page
	globalSegments map[int]*SegmentHeader
	allData        []byte
}

// Page represents a JBIG2 page composed of region segments.
type Page struct {
	number   int
	segments []*SegmentHeader
	document *Document
	bitmap   *Bitmap
}

// NewDocument parses a JBIG2 data stream (without file header, as embedded in PDF).
func NewDocument(data []byte, globals []byte) (*Document, error) {
	doc := &Document{
		pages:          make(map[int]*Page),
		globalSegments: make(map[int]*SegmentHeader),
		allData:        data,
	}

	// Parse globals if present.
	if len(globals) > 0 {
		globalSegs, err := ParseSegmentHeaders(globals)
		if err != nil {
			return nil, fmt.Errorf("parsing JBIG2 globals: %w", err)
		}
		for _, seg := range globalSegs {
			doc.globalSegments[seg.SegmentNumber] = seg
		}
	}

	// Parse the main data stream.
	segments, err := ParseSegmentHeaders(data)
	if err != nil {
		return nil, fmt.Errorf("parsing JBIG2 data: %w", err)
	}

	for _, seg := range segments {
		if seg.PageAssociation == 0 {
			doc.globalSegments[seg.SegmentNumber] = seg
		} else {
			page, ok := doc.pages[seg.PageAssociation]
			if !ok {
				page = &Page{
					number:   seg.PageAssociation,
					document: doc,
				}
				doc.pages[seg.PageAssociation] = page
			}
			page.segments = append(page.segments, seg)
		}
	}

	return doc, nil
}

// GetPage returns the decoded bitmap for the specified page number.
func (doc *Document) GetPage(pageNum int) (*Bitmap, error) {
	page, ok := doc.pages[pageNum]
	if !ok {
		return nil, fmt.Errorf("%w: page %d not found", ErrCorruptedData, pageNum)
	}

	if page.bitmap != nil {
		return page.bitmap, nil
	}

	return page.compose()
}

// PageCount returns the number of pages in the document.
func (doc *Document) PageCount() int {
	return len(doc.pages)
}

// compose assembles the page bitmap from its segments.
func (p *Page) compose() (*Bitmap, error) {
	// Find the PageInformation segment.
	var pageInfo *PageInfo
	for _, seg := range p.segments {
		if seg.SegmentType == SegTypePageInformation {
			data := p.getSegmentData(seg)
			r := NewReader(data)
			var err error
			pageInfo, err = ParsePageInfo(r)
			if err != nil {
				return nil, fmt.Errorf("parsing page info: %w", err)
			}
			break
		}
	}

	if pageInfo == nil {
		return nil, fmt.Errorf("%w: no page information segment", ErrCorruptedData)
	}

	// Create the page bitmap.
	height := pageInfo.Height
	if height <= 0 || height == 0xffffffff {
		height = 1 // Will be extended by striping.
	}

	p.bitmap = NewBitmap(pageInfo.Width, height)
	if pageInfo.DefaultPixelValue != 0 {
		p.bitmap.Fill(0xff)
	}

	// Decode and composite each region segment.
	for _, seg := range p.segments {
		switch seg.SegmentType {
		case SegTypeImmGenericRegion, SegTypeImmLosslessGeneric, SegTypeGenericRegion:
			data := p.getSegmentData(seg)
			regionBitmap, err := DecodeGenericRegion(data)
			if err != nil {
				return nil, fmt.Errorf("decoding generic region segment %d: %w",
					seg.SegmentNumber, err)
			}

			// Re-parse just the region info for location.
			r := NewReader(data)
			ri, err := ParseRegionSegmentInfo(r)
			if err != nil {
				return nil, err
			}

			// Determine combination operator.
			op := ri.CombOp
			if !pageInfo.CombOpOverrideOK {
				op = pageInfo.CombOp
			}

			// Check if this single region IS the page.
			if p.countRegions() == 1 && pageInfo.DefaultPixelValue == 0 &&
				pageInfo.Width == regionBitmap.Width() &&
				height == regionBitmap.Height() {
				p.bitmap = regionBitmap
			} else {
				Blit(regionBitmap, p.bitmap, ri.XLoc, ri.YLoc, op)
			}

		case SegTypeEndOfStripe:
			// Read the line number for striped pages.
			data := p.getSegmentData(seg)
			if len(data) >= 4 {
				r := NewReader(data)
				lineNum, err := r.ReadUint32()
				if err == nil {
					newHeight := int(lineNum) + 1
					if newHeight > p.bitmap.Height() {
						// Extend the page bitmap.
						newBm := NewBitmap(p.bitmap.Width(), newHeight)
						copy(newBm.data, p.bitmap.data)
						p.bitmap = newBm
					}
				}
			}
		}
	}

	return p.bitmap, nil
}

// getSegmentData returns the raw data for a segment.
func (p *Page) getSegmentData(seg *SegmentHeader) []byte {
	src := p.document.allData
	start := seg.DataOffset
	end := start + seg.DataLength

	if start < 0 {
		start = 0
	}
	if end > int64(len(src)) {
		end = int64(len(src))
	}
	if start >= end {
		return nil
	}

	return src[start:end]
}

func (p *Page) countRegions() int {
	count := 0
	for _, s := range p.segments {
		switch s.SegmentType {
		case SegTypeImmTextRegion, SegTypeImmLosslessTextRegion,
			SegTypeImmHalftoneRegion, SegTypeImmLosslessHalftone,
			SegTypeImmGenericRegion, SegTypeImmLosslessGeneric,
			SegTypeImmRefinementRegion, SegTypeImmLosslessRefinement:
			count++
		}
	}
	return count
}
