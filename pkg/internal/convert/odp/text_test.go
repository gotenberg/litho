package odp

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// presStyles are a master's presentation styles as LibreOffice writes
// them: the title, and outline levels one to three, each from the last.
const presStyles = `<style:style style:name="Default-title" style:family="presentation"><style:graphic-properties draw:stroke="none" draw:fill="none" draw:textarea-vertical-align="middle"/>` +
	`<style:paragraph-properties fo:text-align="center"/><style:text-properties fo:font-family="'Liberation Sans'" fo:font-size="44pt"/></style:style>` +
	`<style:style style:name="Default-outline1" style:family="presentation"><style:graphic-properties draw:stroke="none" draw:fill="none" style:shrink-to-fit="true"/>` +
	`<style:paragraph-properties fo:margin-top="0.5cm"/><style:text-properties fo:font-family="'Liberation Sans'" fo:font-size="32pt"/></style:style>` +
	`<style:style style:name="Default-outline2" style:family="presentation" style:parent-style-name="Default-outline1"><style:paragraph-properties fo:margin-top="0.4cm"/><style:text-properties fo:font-size="28pt"/></style:style>` +
	`<style:style style:name="Default-outline3" style:family="presentation" style:parent-style-name="Default-outline2"><style:text-properties fo:font-size="24pt"/></style:style>`

func TestRead_ParagraphStyles(t *testing.T) {
	f := flat{
		fonts:  `<style:font-face style:name="DejaVu Sans1" svg:font-family="'DejaVu Sans'"/>`,
		styles: presStyles,
		auto: `<style:style style:name="pr1" style:family="presentation" style:parent-style-name="Default-outline1"><style:text-properties fo:color="#00ff00"/></style:style>` +
			`<style:style style:name="pr2" style:family="presentation" style:parent-style-name="Default-title"/>` +
			`<style:style style:name="P1" style:family="paragraph"><style:paragraph-properties fo:text-align="end" fo:line-height="150%" fo:margin-bottom="0.2cm"/><style:text-properties fo:font-size="50%"/></style:style>` +
			`<style:style style:name="T1" style:family="text"><style:text-properties fo:font-weight="bold" fo:font-style="italic" style:text-underline-style="solid" style:text-position="super 58%" style:font-name="DejaVu Sans1" fo:language="de" fo:country="DE"/></style:style>`,
		body: page(`<draw:frame presentation:style-name="pr2" presentation:class="title" svg:width="10cm" svg:height="2cm"><draw:text-box><text:p>Title</text:p></draw:text-box></draw:frame>` +
			`<draw:frame presentation:style-name="pr1" presentation:class="outline" svg:width="10cm" svg:height="8cm"><draw:text-box>` +
			`<text:list><text:list-item><text:p>One</text:p><text:list><text:list-item><text:p text:style-name="P1">Two <text:span text:style-name="T1">x</text:span></text:p>` +
			`<text:list><text:list-item><text:p>Three</text:p></text:list-item></text:list></text:list-item></text:list></text:list-item></text:list>` +
			`</draw:text-box></draw:frame>`),
	}
	doc, _ := read(t, f.bytes())
	title := doc.Slides[0].Shapes[0]
	if p := title.Text.Paragraphs[0]; p.Align != css.AlignCenter || p.Style.Size != 44 || title.Text.Anchor != slides.AnchorMiddle || title.Fill.Kind != slides.FillNone {
		t.Errorf("title = %+v %+v", title, p)
	}
	ps := doc.Slides[0].Shapes[1].Text.Paragraphs
	if len(ps) != 3 {
		t.Fatalf("paragraphs = %d", len(ps))
	}
	green := css.Color{G: 1, A: 1}
	for i, want := range []struct {
		level  int
		size   float64
		before float64
	}{{0, 32, cm(0.5)}, {1, 14, cm(0.4)}, {2, 24, cm(0.4)}} {
		p := ps[i]
		if p.Level != want.level || !near(p.Style.Size, want.size) || !near(p.SpaceBefore, want.before) || p.Style.Color != green {
			t.Errorf("paragraph %d = level %d size %v before %v colour %v, want %+v", i, p.Level, p.Style.Size, p.SpaceBefore, p.Style.Color, want)
		}
	}
	two := ps[1]
	if two.Align != css.AlignEnd || two.LineSpacing != 1.5 || !near(two.SpaceAfter, cm(0.2)) {
		t.Errorf("paragraph style = %+v", two)
	}
	x := two.Runs[len(two.Runs)-1].Style
	if !x.Bold || !x.Italic || !x.Underline || x.Script != slides.Superscript || x.Lang != "de-DE" || x.Families[0] != "DejaVu Sans" {
		t.Errorf("span style = %+v", x)
	}
}

func TestRead_AutomaticColour(t *testing.T) {
	for _, tt := range []struct {
		name, fill string
		want       css.Color
	}{
		// The built-in standard style's green, #18a303, is dark: Impress
		// prints its text white.
		{"dark fill", `draw:fill="solid" draw:fill-color="#18a303"`, css.Color{R: 1, G: 1, B: 1, A: 1}},
		{"light fill", `draw:fill="solid" draw:fill-color="#ffff00"`, css.Color{A: 1}},
		{"default blue", `draw:fill="solid" draw:fill-color="#729fcf"`, css.Color{A: 1}},
		{"no fill on white", `draw:fill="none"`, css.Color{A: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := styledRect("", tt.fill)
			f.auto += `<style:style style:name="t" style:family="graphic" style:parent-style-name="s"><style:text-properties style:use-window-font-color="true"/></style:style>`
			f.body = page(`<draw:custom-shape draw:style-name="t" svg:width="1cm" svg:height="1cm"><text:p>x</text:p><draw:enhanced-geometry draw:type="rectangle"/></draw:custom-shape>`)
			if got := firstShape(t, f).Text.Paragraphs[0].Runs[0].Style.Color; got != tt.want {
				t.Errorf("colour = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRead_Runs(t *testing.T) {
	tests := []struct {
		name, p string
		want    []slides.Run
	}{
		{"white space", "<text:p>  a \n\t b  </text:p>", []slides.Run{{Text: "a b"}}},
		{"spaces tabs breaks", `<text:p>a<text:s text:c="3"/>b<text:tab/>c<text:line-break/>d <text:s/> e</text:p>`,
			[]slides.Run{{Text: "a   b\tc"}, {Break: true}, {Text: "d   e"}}},
		{"link", `<text:p>go <text:a xlink:href="https://x.test/">here</text:a></text:p>`,
			[]slides.Run{{Text: "go "}, {Text: "here", Href: "https://x.test/"}}},
		{"fields", `<text:p><text:page-number>3</text:page-number>/<text:page-count>9</text:page-count> <text:date>1/1/26</text:date> <text:time text:fixed="true">10:00</text:time> <text:author-name>Ann</text:author-name></text:p>`,
			[]slides.Run{{Text: "3", Field: slides.FieldSlideNumber}, {Text: "/"}, {Text: "9", Field: slides.FieldSlideCount}, {Text: " "},
				{Text: "1/1/26", Field: slides.FieldDate}, {Text: " 10:00 "}, {Text: "Ann", Field: slides.FieldAuthor}}},
		{"break trims", "<text:p>a <text:line-break/> b </text:p>", []slides.Run{{Text: "a"}, {Break: true}, {Text: "b"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := firstShape(t, flat{body: page(`<draw:frame svg:width="5cm" svg:height="2cm"><draw:text-box>` + tt.p + `</draw:text-box></draw:frame>`)})
			got := s.Text.Paragraphs[0].Runs
			if !slices.EqualFunc(got, tt.want, func(a, b slides.Run) bool {
				return a.Text == b.Text && a.Break == b.Break && a.Field == b.Field && a.Href == b.Href
			}) {
				t.Errorf("runs = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRead_Lists(t *testing.T) {
	lists := `<text:list-style style:name="L1">` +
		`<text:list-level-style-bullet text:level="1" text:bullet-char="▪"><style:list-level-properties text:space-before="1cm" text:min-label-width="2cm"/><style:text-properties fo:font-family="'DejaVu Sans'" fo:font-size="100%" fo:color="#ff0000"/></text:list-level-style-bullet>` +
		`<text:list-level-style-number text:level="2" style:num-format="a" style:num-suffix=")" text:start-value="3"><style:list-level-properties text:space-before="2cm" text:min-label-width="1cm"/></text:list-level-style-number>` +
		`<text:list-level-style-number text:level="3" style:num-format="1" style:num-suffix="."><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment fo:margin-left="3cm" fo:text-indent="-1cm"/></style:list-level-properties></text:list-level-style-number>` +
		`<text:list-level-style-bullet text:level="4" text:bullet-char="&#xf095;"><style:text-properties fo:font-family="StarBats"/></text:list-level-style-bullet>` +
		`</text:list-style>`
	item := func(p string, nested string) string {
		return `<text:list-item><text:p>` + p + `</text:p>` + nested + `</text:list-item>`
	}
	body := page(`<draw:frame svg:width="10cm" svg:height="8cm"><draw:text-box>` +
		`<text:list>` + item("default", `<text:list>`+item("default two", "")+`</text:list>`) + `</text:list>` +
		`<text:list text:style-name="L1">` + item("bullet", `<text:list>`+item("number", `<text:list>`+item("aligned", `<text:list>`+item("starbats", "")+`</text:list>`)+`</text:list>`)+`</text:list>`) +
		`<text:list-header><text:p>header</text:p></text:list-header></text:list>` +
		`</draw:text-box></draw:frame>`)
	ps := firstShape(t, flat{auto: lists, body: body}).Text.Paragraphs
	type want struct {
		margin, indent float64
		bullet         *slides.Bullet
	}
	wants := []want{
		// Measured on the pinned LibreOffice: a list of no style indents
		// each level 0.6 cm, its bullet 45% of the text in OpenSymbol.
		{cm(0.6), -cm(0.6), &slides.Bullet{Kind: slides.BulletChar, Char: "•", Families: []string{"OpenSymbol"}, Scale: 0.45}},
		{cm(1.2), -cm(0.6), &slides.Bullet{Kind: slides.BulletChar, Char: "•", Families: []string{"OpenSymbol"}, Scale: 0.45}},
		{cm(3), -cm(2), &slides.Bullet{Kind: slides.BulletChar, Char: "▪", Families: []string{"DejaVu Sans"}, Scale: 1, Color: css.Color{R: 1, A: 1}}},
		{cm(3), -cm(1), &slides.Bullet{Kind: slides.BulletNumber, Format: "alphaLcParenR", StartAt: 3, Scale: 0.45}},
		// Impress lays a label-alignment level out with no indent.
		{0, 0, &slides.Bullet{Kind: slides.BulletNumber, Format: "arabicPeriod", StartAt: 1, Scale: 0.45}},
		{0, 0, &slides.Bullet{Kind: slides.BulletChar, Char: "•", Families: []string{"OpenSymbol"}, Scale: 0.45}},
		{0, 0, nil},
	}
	if len(ps) != len(wants) {
		t.Fatalf("paragraphs = %q", texts(ps))
	}
	for i, w := range wants {
		p := ps[i]
		if i < len(wants)-1 && (!near(p.MarginLeft, w.margin) || !near(p.Indent, w.indent)) {
			t.Errorf("%d: margin %v indent %v, want %v %v", i, p.MarginLeft, p.Indent, w.margin, w.indent)
		}
		switch {
		case w.bullet == nil && p.Bullet != nil:
			t.Errorf("%d: bullet %+v, want none", i, *p.Bullet)
		case w.bullet != nil && (p.Bullet == nil || p.Bullet.Kind != w.bullet.Kind || p.Bullet.Char != w.bullet.Char || !slices.Equal(p.Bullet.Families, w.bullet.Families) ||
			p.Bullet.Format != w.bullet.Format || p.Bullet.StartAt != w.bullet.StartAt || p.Bullet.Scale != w.bullet.Scale || p.Bullet.Color != w.bullet.Color):
			t.Errorf("%d: bullet %+v, want %+v", i, p.Bullet, *w.bullet)
		}
	}
}

func TestNumberScheme(t *testing.T) {
	tests := []struct{ format, prefix, suffix, want string }{
		{"1", "", ".", "arabicPeriod"},
		{"1", "", "", "arabicPlain"},
		{"1", "(", ")", "arabicParenBoth"},
		{"i", "", ")", "romanLcParenR"},
		{"A", "", ".", "alphaUcPeriod"},
		{"I", "", "-", "romanUcPeriod"},
		{"一, 二, 三, ...", "", ".", "arabicPeriod"},
	}
	for _, tt := range tests {
		if got := numberScheme(tt.format, tt.prefix, tt.suffix); got != tt.want {
			t.Errorf("numberScheme(%q, %q, %q) = %q, want %q", tt.format, tt.prefix, tt.suffix, got, tt.want)
		}
	}
}
