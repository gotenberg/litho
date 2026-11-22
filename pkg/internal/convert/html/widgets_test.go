package html

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestPrepare_ControlsTakeTheirWidget gives each control the widget the
// layout draws, with the state that shows: checkedness, disabledness (a
// disabled fieldset's outside its first legend included), a range's
// thumb, a progress bar's and a meter's fill, a colour well's colour.
func TestPrepare_ControlsTakeTheirWidget(t *testing.T) {
	t.Parallel()
	p := prep(t, `<input id=cb type=checkbox checked><input id=rb type=radio>`+
		`<fieldset disabled><legend><input id=inlegend></legend><input id=infield></fieldset>`+
		`<input id=r type=range value=30><input id=rs type=range min=0 max=10 step=4 value=7><input id=rdef type=range>`+
		`<progress id=p value=0.3></progress><progress id=pi></progress><progress id=pmax max=200 value=300></progress>`+
		`<meter id=m value=0.6></meter><meter id=mlow min=0 max=10 low=3 high=7 optimum=9 value=1></meter>`+
		`<meter id=mmid min=0 max=10 low=3 high=7 optimum=9 value=5></meter>`+
		`<input id=c type=color value="#FF8800"><input id=cbad type=color value="red"><input id=n type=number value=4>`, Options{})
	for id, want := range map[string]layout.Control{
		"cb":       {Kind: layout.ControlCheckBox, Checked: true},
		"rb":       {Kind: layout.ControlRadio},
		"inlegend": {Kind: layout.ControlTextField, Chars: 20},
		"infield":  {Kind: layout.ControlTextField, Chars: 20, Disabled: true},
		"r":        {Kind: layout.ControlRange, Value: 0.3},
		"rs":       {Kind: layout.ControlRange, Value: 0.8},
		"rdef":     {Kind: layout.ControlRange, Value: 0.5},
		"p":        {Kind: layout.ControlProgress, Value: 0.3},
		"pi":       {Kind: layout.ControlProgress, Value: -1},
		"pmax":     {Kind: layout.ControlProgress, Value: 1},
		"m":        {Kind: layout.ControlMeter, Value: 0.6},
		"mlow":     {Kind: layout.ControlMeter, Value: 0.1, Level: layout.MeterEvenLessGood},
		"mmid":     {Kind: layout.ControlMeter, Value: 0.5, Level: layout.MeterSuboptimal},
		"c":        {Kind: layout.ControlColor, Swatch: typeset.Color{R: 1, G: 0x88 / 255.0, A: 1}},
		"cbad":     {Kind: layout.ControlColor, Swatch: typeset.Black},
		"n":        {Kind: layout.ControlTextField, Chars: 20},
	} {
		got := byID(t, p.root, id).Control
		if got == nil {
			t.Errorf("%s: no control", id)
			continue
		}
		c := *got
		c.Labels = nil
		if !near(c.Value, want.Value) {
			t.Errorf("%s: value %v, want %v", id, c.Value, want.Value)
		}
		c.Value = want.Value
		if c.Kind != want.Kind || c.Checked != want.Checked || c.Disabled != want.Disabled || c.Level != want.Level ||
			c.Chars != want.Chars || c.Swatch != want.Swatch {
			t.Errorf("%s: control %+v, want %+v", id, c, want)
		}
	}
}

func near(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

// TestPrepare_ControlsAreForms tags a control as a Form element whose
// PrintField attributes say its kind, a check box's and a radio button's
// state, and its description (ISO 32000-1, 14.8.5.6, Table 348); a
// progress bar, a meter, a range and a colour well, which no role names,
// start no element.
func TestPrepare_ControlsAreForms(t *testing.T) {
	t.Parallel()
	p := prep(t, `<input id=t name=email><input id=cb type=checkbox checked aria-label="Agree">`+
		`<input id=rb type=radio title="Pick"><input id=s type=submit><button id=b>Go</button>`+
		`<select id=d><option>a</select><select id=l size=3><option>a</select><textarea id=ta></textarea>`+
		`<input id=dt type=date><progress id=p></progress><input id=r type=range>`, Options{})
	for id, want := range map[string]typeset.PrintField{
		"t":  {Role: typeset.PrintText, Desc: "email"},
		"cb": {Role: typeset.PrintCheckBox, Checked: typeset.CheckOn, Desc: "Agree"},
		"rb": {Role: typeset.PrintRadio, Checked: typeset.CheckOff, Desc: "Pick"},
		"s":  {Role: typeset.PrintPushButton},
		"b":  {Role: typeset.PrintPushButton},
		"d":  {Role: typeset.PrintText},
		"l":  {Role: typeset.PrintList},
		"ta": {Role: typeset.PrintText},
		"dt": {Role: typeset.PrintText},
	} {
		n := byID(t, p.root, id)
		if n.Role != "Form" || n.PrintField != want {
			t.Errorf("%s: role %q %+v, want Form %+v", id, n.Role, n.PrintField, want)
		}
	}
	for _, id := range []string{"p", "r"} {
		if n := byID(t, p.root, id); n.Role != "" {
			t.Errorf("%s: role %q, want none", id, n.Role)
		}
	}
}

// TestPrepare_DateFieldsShowChromiumsText shows a date or time input's
// value as Chromium shows it in its en-US locale, its fields apart from
// the literal text between them, and placeholders for no value.
func TestPrepare_DateFieldsShowChromiumsText(t *testing.T) {
	t.Parallel()
	p := prep(t, `<input id=d type=date value=2026-11-22><input id=d0 type=date value=1999-01-05><input id=de type=date value=bad>`+
		`<input id=t type=time value=13:45><input id=ts type=time value=09:05:30><input id=tm type=time value=13:45:30.5>`+
		`<input id=te type=time><input id=dl type=datetime-local value=2026-11-22T13:45><input id=m type=month value=2026-11>`+
		`<input id=w type=week value=2026-W47><input id=we type=week value=2026-W99><input id=mid type=time value=00:05>`, Options{})
	for id, want := range map[string]string{
		"d": "11/22/2026", "d0": "01/05/1999", "de": "mm/dd/yyyy", "t": "01:45 PM", "ts": "09:05:30 AM",
		"tm": "01:45:30.500 PM", "te": "--:-- --", "dl": "11/22/2026, 01:45 PM", "m": "November 2026",
		"w": "Week 47, 2026", "we": "Week --, ----", "mid": "12:05 AM",
	} {
		n := byID(t, p.root, id)
		if got := treeText(n); got != want {
			t.Errorf("%s: text %q, want %q", id, got, want)
		}
	}
	m := byID(t, p.root, "m")
	if got := m.Control.Labels[0].Text; got != "September 2026" || m.Control.Fields != 2 {
		t.Errorf("month: as wide as %q with %d fields, want September 2026 and 2", got, m.Control.Fields)
	}
	if got := byID(t, p.root, "dl").Control.Fields; got != 6 {
		t.Errorf("local date and time: %d fields, want 6", got)
	}
	d := byID(t, p.root, "d")
	var fields []string
	for _, c := range d.Children {
		if c.Kind == layout.Element {
			fields = append(fields, treeText(c))
		}
	}
	if strings.Join(fields, ",") != "11,22,2026" {
		t.Errorf("date fields %v, want 11, 22 and 2026 apart", fields)
	}
}

// TestPrepare_PlaceholdersAndFiles colours a placeholder #757575, keeps an
// empty field's line, and builds a file input as Chromium shows it.
func TestPrepare_PlaceholdersAndFiles(t *testing.T) {
	t.Parallel()
	p := prep(t, `<input id=ph placeholder="Name"><input id=v value="x" placeholder="Name"><input id=e>`+
		`<textarea id=ta placeholder="More"></textarea><input id=f type=file><input id=fm type=file multiple>`, Options{})
	for id, gray := range map[string]bool{"ph": true, "v": false, "ta": true} {
		c := byID(t, p.root, id).Children[0].Style.Color
		if got := c == placeholderColor; got != gray {
			t.Errorf("%s: placeholder colour %v, want %v", id, got, gray)
		}
	}
	if e := byID(t, p.root, "e"); len(e.Children) != 1 || e.Children[0].Kind != layout.LineBreak {
		t.Errorf("empty field children %+v, want its empty line", e.Children)
	}
	for id, want := range map[string]string{"f": "Choose FileNo file chosen", "fm": "Choose FilesNo file chosen"} {
		f := byID(t, p.root, id)
		if got := treeText(f); got != want || f.Control.Kind != layout.ControlFile || f.Children[0].Control.Kind != layout.ControlButton {
			t.Errorf("%s: %q %+v, want %q with a button", id, got, f.Control, want)
		}
	}
}
