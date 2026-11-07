package html

import "testing"

func TestRootLanguage(t *testing.T) {
	tests := []struct {
		src, want string
	}{
		{`<html lang=" ja "><head><meta http-equiv="Content-Language" content="ko"></head></html>`, "ja"},
		{`<html><head><meta http-equiv="content-language" content="zh-Hans"></head></html>`, "zh-Hans"},
		{`<html><head><meta http-equiv="content-language" content="en, fr"><meta http-equiv="content-language" content="ko"></head></html>`, "ko"},
		{`<html><head><meta name="language" content="ja"></head></html>`, ""},
		{`<html lang=""><head><meta http-equiv="content-language" content="ja"></head></html>`, ""},
	}
	for _, tt := range tests {
		p := prep(t, tt.src, Options{})
		if p.root.Lang != tt.want || p.layout.Lang != tt.want {
			t.Errorf("%s: root %q, document %q, want %q", tt.src, p.root.Lang, p.layout.Lang, tt.want)
		}
	}
}
