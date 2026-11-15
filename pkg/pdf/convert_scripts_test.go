package pdf

import (
	"context"
	"io"
	"strings"
	"testing"
)

// TestWarningScriptNotRun_HTMLAndMarkdown checks that an HTML and a
// Markdown source holding scripts a browser would run report one
// WarningScriptNotRun of the omitted effect, saying how many, and that a
// source holding only a data block reports none.
func TestWarningScriptNotRun_HTMLAndMarkdown(t *testing.T) {
	t.Parallel()

	fonts := WithConvertFontDirs(convertFontDir(t))
	for _, tc := range []struct {
		name string
		conv *Conversion
		want string
	}{
		{"html", FromHTML(strings.NewReader(`<body onload="go()"><script>go()</script><noscript>no script</noscript><p>x`), fonts), "2 scripts did not run"},
		{"markdown", FromMarkdown(strings.NewReader("# T\n\n<script src=\"app.js\"></script>\n\ntext\n"), fonts), "1 script did not run"},
		{"data block", FromHTML(strings.NewReader(`<script type="application/ld+json">{}</script><p>x`), fonts), ""},
	} {
		rep, err := tc.conv.WriteTo(context.Background(), io.Discard)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var got []ConvertWarning
		for _, w := range rep.Warnings {
			if w.Code == WarningScriptNotRun {
				got = append(got, w)
			}
		}
		if tc.want == "" {
			if len(got) != 0 {
				t.Errorf("%s: got %+v, want no WarningScriptNotRun", tc.name, got)
			}
			continue
		}
		if len(got) != 1 || !strings.Contains(got[0].Message, tc.want) || got[0].Effect != EffectOmitted {
			t.Errorf("%s: got %+v, want one omitted WarningScriptNotRun saying %q", tc.name, got, tc.want)
		}
	}
}
