package bookmarks

import "testing"

func TestDetectScript(t *testing.T) {
	tests := []struct {
		text string
		want scriptKind
	}{
		{"Hello world", scriptLatin},
		{"第一章 序論", scriptCJK},
		{"제1장 서론", scriptCJK},
		{"Глава 1 Введение", scriptCyrillic},
		{"الفصل الأول", scriptArabic},
	}
	for _, tc := range tests {
		got := detectScript(tc.text)
		if got != tc.want {
			t.Errorf("detectScript(%q) = %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestMatchLanguagePattern(t *testing.T) {
	if !matchLanguagePattern("第一章 序論", scriptCJK) {
		t.Error("CJK chapter pattern should match")
	}
	if !matchLanguagePattern("제1장 서론", scriptCJK) {
		t.Error("Korean chapter pattern should match")
	}
	if !matchLanguagePattern("Глава 5 Введение", scriptCyrillic) {
		t.Error("Russian chapter pattern should match")
	}
	if matchLanguagePattern("Plain English", scriptLatin) {
		t.Error("Latin should not match any pattern (no Latin patterns defined)")
	}
}

func TestScriptFontSizeRatio(t *testing.T) {
	if r := scriptFontSizeRatio(scriptCJK); r >= 1.15 || r <= 0 {
		t.Errorf("CJK ratio should be < 1.15 and > 0, got %f", r)
	}
	if r := scriptFontSizeRatio(scriptLatin); r != 0 {
		t.Errorf("Latin ratio should be 0 (use default), got %f", r)
	}
}
