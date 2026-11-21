package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// popplerDifferenceFile is the layout of a committed file of the pages the
// targets cell's poppler oracle draws apart from litho for a cause
// adjudicated against poppler (see [Manifest.GoldenOverrides]).
type popplerDifferenceFile struct {
	Differences []popplerDifference `json:"differences"`
}

// popplerDifference accepts that poppler draws one page of a fixture apart
// from litho, on any target. Pixels is the count of differing pixels
// measured when the entry was written: the page passes while it differs
// by at most that many, so a new fault on the page still fails it. A
// target whose file draws the construct otherwise, such as a PDF/A-1
// file without transparency, may draw the page alike in both; an entry
// whose page poppler draws as litho does on every target fails them, so
// the file never keeps an adjudication nothing needs. Reason names the
// construct, what poppler does with it, the clause, and the renderers that
// draw it as litho does.
type popplerDifference struct {
	Fixture string `json:"fixture"`
	Page    int    `json:"page"`
	Pixels  int    `json:"pixels"`
	Reason  string `json:"reason"`
}

type popplerDifferenceKey struct {
	fixture string
	page    int
}

// errPopplerDifferenceInvalid flags an entry that breaks the contract of
// [popplerDifference].
var errPopplerDifferenceInvalid = errors.New("invalid poppler difference")

// readPopplerDifferences parses and validates a poppler difference file.
func readPopplerDifferences(path string) (map[popplerDifferenceKey]popplerDifference, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from the committed corpus manifest
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var file popplerDifferenceFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make(map[popplerDifferenceKey]popplerDifference, len(file.Differences))
	for i, d := range file.Differences {
		key := popplerDifferenceKey{d.Fixture, d.Page}
		switch {
		case d.Fixture == "" || d.Page < 1 || d.Page > renderMaxPages:
			return nil, fmt.Errorf("%w: entry %d names no page the oracle draws", errPopplerDifferenceInvalid, i)
		case d.Pixels < 1:
			return nil, fmt.Errorf("%w: %s page %d accepts no pixel", errPopplerDifferenceInvalid, d.Fixture, d.Page)
		case d.Reason == "":
			return nil, fmt.Errorf("%w: %s page %d gives no reason", errPopplerDifferenceInvalid, d.Fixture, d.Page)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("%w: %s page %d listed twice", errPopplerDifferenceInvalid, d.Fixture, d.Page)
		}
		out[key] = d
	}
	return out, nil
}

// LoadOverrides reads the corpus's committed poppler differences.
func (f *targetsFeature) LoadOverrides(path string) error {
	diffs, err := readPopplerDifferences(path)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.popplerDiffs = diffs
	f.mu.Unlock()
	return nil
}

// popplerPageJudgement is what the poppler oracle found on one page.
type popplerPageJudgement struct {
	// reason says why the page fails, "" when it passes.
	reason string
	// entry is set when the corpus accepts a difference on the page, and
	// matched when poppler then draws the page as litho does.
	entry, matched bool
}

// judgePopplerPage holds poppler's drawing of page of r against litho's.
// The page passes when it matches, or when it differs by no more pixels
// than the corpus's entry for it accepts; an entry whose page matches on
// every target is stale, which the caller judges across the targets.
func (f *targetsFeature) judgePopplerPage(staging string, r *targetResult, page int) popplerPageJudgement {
	detail, pixels := comparePoppler(staging, r.file, page)
	f.mu.RLock()
	accepted, ok := f.popplerDiffs[popplerDifferenceKey{r.fixture, page}]
	f.mu.RUnlock()
	switch {
	case !ok:
		return popplerPageJudgement{reason: detail}
	case detail == "":
		return popplerPageJudgement{entry: true, matched: true}
	case pixels == 0 || pixels > accepted.Pixels:
		return popplerPageJudgement{reason: fmt.Sprintf("%s, past the %d accepted", detail, accepted.Pixels), entry: true}
	}
	return popplerPageJudgement{entry: true}
}
