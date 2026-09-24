package jpx

import (
	"context"
	"fmt"
)

// Check reports, from the headers of the image alone, whether [Image.Render]
// of chs refuses it with [ErrUnsupported], and decodes no coefficient to find
// out. err is the first error Render meets that the headers show: a construct
// Render refuses as unsupported, or a malformed or over-budget one it fails on
// first. Check applies the checks Render makes of chs, then, tile by tile, the
// ones tile decoding makes of the coding parameters the main and tile-part
// headers give: the layout they imply, the magnitude bit-planes of each
// sub-band on the reversible path, and the component transformation.
//
// settled is false when Render may refuse the image where Check cannot look:
// tier-1 decoding refuses a code-block of more than maxPlanes magnitude
// bit-planes only once a packet has given it a coding pass, and Check reads
// no packet. An err wrapping ErrUnsupported stands whatever settled says,
// since Render then refuses the image at that code-block or at err.
//
// Check does not read the packets or the coded data, so an image damaged there
// before the construct Check reports fails with the damage instead.
func (img *Image) Check(ctx context.Context, chs []Channel) (settled bool, err error) {
	if err := img.checkChannels(chs); err != nil {
		return true, err
	}
	settled = true
	budget := &blockBudget{}
	for _, t := range img.cs.tiles {
		if t == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return settled, err
		}
		tileSettled, err := img.cs.checkTile(t, budget)
		settled = settled && tileSettled
		if err != nil {
			return settled, fmt.Errorf("tile %d: %w", t.index, err)
		}
	}
	return settled, nil
}

// checkTile is [Image.Check] for one tile, in the order decodeTile meets the
// checks, counting its layout against budget as decodeTile does.
func (cs *codestream) checkTile(t *tile, budget *blockBudget) (settled bool, err error) {
	d, err := cs.resolveTile(t, budget, false)
	if err != nil {
		return true, err
	}
	settled = true
	for c := range d.comps {
		tc := &d.comps[c]
		for r := range tc.resolutions {
			res := &tc.resolutions[r]
			if err := tc.checkReversible(res); err != nil {
				return settled, err
			}
			for i := range res.bands {
				if tc.blockPlanes(&res.bands[i]) > maxPlanes {
					settled = false
				}
			}
		}
	}
	if d.transformed {
		if err := checkMCT(cs, d); err != nil {
			return settled, err
		}
	}
	return settled, nil
}
