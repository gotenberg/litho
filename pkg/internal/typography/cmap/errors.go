package cmap

import "errors"

// ErrCMapTooLarge reports a CMap program past its parse budget: ranges that
// expand to more codes than [maxCMapRangeCodes], or more codespace ranges than
// [maxCodeSpaceRanges]. Callers read such a CMap as one they cannot parse.
var ErrCMapTooLarge = errors.New("CMap exceeds its parse budget")
