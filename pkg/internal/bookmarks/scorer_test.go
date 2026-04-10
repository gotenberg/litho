package bookmarks

import "testing"

func TestHeuristicScorer_LargeFont(t *testing.T) {
	s := HeuristicScorer{}
	score := s.Score(HeadingCandidate{
		Text:      "Introduction",
		FontSize:  24,
		BodySize:  10,
		CharCount: 12,
		Script:    scriptLatin,
	})
	if score < 0.5 {
		t.Errorf("large font heading should score >= 0.5, got %f", score)
	}
}

func TestHeuristicScorer_LongLine(t *testing.T) {
	s := HeuristicScorer{}
	long := "This is a very long line of body text that goes on and on and on and on and on and on and on and on and definitely should not be classified as a heading because it is way too long"
	score := s.Score(HeadingCandidate{
		Text:      long,
		FontSize:  10,
		BodySize:  10,
		CharCount: len(long),
		Script:    scriptLatin,
	})
	if score >= 0.5 {
		t.Errorf("long body line should score < 0.5, got %f", score)
	}
}

func TestHeuristicScorer_NumberingBoost(t *testing.T) {
	s := HeuristicScorer{}
	withNum := s.Score(HeadingCandidate{
		Text:           "1.1 Overview",
		FontSize:       11,
		BodySize:       10,
		CharCount:      12,
		HasNumbering:   true,
		NumberingDepth: 2,
		Script:         scriptLatin,
	})
	withoutNum := s.Score(HeadingCandidate{
		Text:      "Overview",
		FontSize:  11,
		BodySize:  10,
		CharCount: 8,
		Script:    scriptLatin,
	})
	if withNum <= withoutNum {
		t.Errorf("numbering should boost score: with=%f, without=%f", withNum, withoutNum)
	}
}

func TestHeuristicScorer_TrailingPeriod(t *testing.T) {
	s := HeuristicScorer{}
	noPunct := s.Score(HeadingCandidate{
		Text:      "Conclusions",
		FontSize:  18,
		BodySize:  10,
		CharCount: 11,
		Script:    scriptLatin,
	})
	withPeriod := s.Score(HeadingCandidate{
		Text:      "Conclusions.",
		FontSize:  18,
		BodySize:  10,
		CharCount: 12,
		Script:    scriptLatin,
	})
	if noPunct <= withPeriod {
		t.Errorf("trailing period should reduce score: noPunct=%f, withPeriod=%f", noPunct, withPeriod)
	}
}

func TestHeuristicScorer_LanguagePattern(t *testing.T) {
	s := HeuristicScorer{}
	score := s.Score(HeadingCandidate{
		Text:      "第一章 序論",
		FontSize:  10,
		BodySize:  10,
		CharCount: 6,
		Script:    scriptCJK,
	})
	if score < 0.3 {
		t.Errorf("CJK chapter pattern should boost: got %f", score)
	}
}
