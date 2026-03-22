package compliance2

import "testing"

func TestIsValidLangTag(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name string
		tag  string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"simple two-letter", "en"},
		{"simple three-letter", "fra"},
		{"language-region", "en-US"},
		{"language-region lowercase", "en-us"},
		{"language-script-region", "zh-Hans-CN"},
		{"private use", "x-private"},
		{"private use multi", "x-private-foo"},
		{"grandfathered IANA", "i-klingon"},
		{"grandfathered IANA uppercase", "I-Klingon"},
		{"language-region-private", "en-US-x-custom"},
		{"script-region", "sr-Latn-RS"},
		{"variant date-style", "de-CH-1901"},
		{"multiple variants", "sl-rozaj-biske"},
		{"four-letter language", "scou"},
		{"five-letter language", "abcde"},
		{"eight-letter language", "abcdefgh"},
		{"three-digit region", "es-419"},
		{"extlang", "zh-yue"},
		{"extlang with script", "zh-yue-Hans"},
		{"extension singleton", "en-a-ext1"},
		{"extension multi subtag", "en-a-ext1-ext2"},
		{"multiple extensions", "en-a-ext1-b-ext2"},
		{"extension then private", "en-a-ext1-x-priv"},
		{"variant digit-prefix", "en-US-1abc"},
		{"case insensitive script", "en-latn-us"},
	}

	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if !isValidLangTag(tc.tag) {
				t.Errorf("isValidLangTag(%q) = false, want true", tc.tag)
			}
		})
	}

	invalid := []struct {
		name string
		tag  string
	}{
		{"single letter not i or x", "a"},
		{"too-long primary", "abcdefghi"},
		{"single digit primary", "1"},
		{"digit in primary", "e1"},
		{"empty subtag double dash", "en--US"},
		{"subtag too long", "en-abcdefghi"},
		{"non-ascii", "en-ü"},
		{"space inside", "en US"},
		{"trailing dash", "en-"},
		{"leading dash", "-en"},
		{"lone x", "x"},
		{"lone i", "i"},
		{"singleton without subtag", "en-a"},
		{"private use empty subtag", "x-"},
		{"extension subtag too short", "en-a-x"},
	}

	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if isValidLangTag(tc.tag) {
				t.Errorf("isValidLangTag(%q) = true, want false", tc.tag)
			}
		})
	}
}
