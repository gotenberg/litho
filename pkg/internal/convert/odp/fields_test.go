package odp

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/odf"
)

// TestImpressFormat locks the Impress field formats data styles choose
// (see [reader.fieldCode], which holds the measurements).
func TestImpressFormat(t *testing.T) {
	const ns = `xmlns:number="urn:oasis:names:tc:opendocument:xmlns:datastyle:1.0"`
	for _, tc := range []struct {
		xml, want string
	}{
		{`<number:date-style ` + ns + ` number:automatic-order="true"><number:day number:style="long"/><number:text>.</number:text><number:month number:style="long"/><number:text>.</number:text><number:year number:style="long"/></number:date-style>`, "mm/dd/yy"},
		{`<number:date-style ` + ns + `><number:day number:style="long"/><number:text>.</number:text><number:month number:style="long"/><number:text>.</number:text><number:year number:style="long"/></number:date-style>`, "mm/dd/yyyy"},
		{`<number:date-style ` + ns + `><number:day/><number:text>. </number:text><number:month number:textual="true"/><number:text> </number:text><number:year number:style="long"/></number:date-style>`, "mmm d, yyyy"},
		{`<number:date-style ` + ns + `><number:day/><number:month number:style="long" number:textual="true"/><number:year number:style="long"/></number:date-style>`, "mmmm d, yyyy"},
		{`<number:date-style ` + ns + `><number:day-of-week number:style="long"/><number:day/><number:month number:style="long" number:textual="true"/><number:year number:style="long"/></number:date-style>`, "dddd, mmmm d, yyyy"},
		{`<number:time-style ` + ns + `><number:hours/><number:text>:</number:text><number:minutes/><number:text>:</number:text><number:seconds/><number:am-pm/></number:time-style>`, "hh:mm:ss AM/PM"},
		{`<number:time-style ` + ns + `><number:hours/><number:text>:</number:text><number:minutes/></number:time-style>`, "hh:mm"},
		{`<number:time-style ` + ns + `><number:hours/><number:minutes/><number:am-pm/></number:time-style>`, "hh:mm AM/PM"},
		{`<number:date-style ` + ns + `><number:year number:style="long"/><number:month/></number:date-style>`, ""},
		{`<number:date-style ` + ns + `><number:quarter/><number:day/><number:month/><number:year number:style="long"/></number:date-style>`, ""},
	} {
		el, err := odf.ParseXML(context.Background(), []byte(tc.xml))
		if err != nil {
			t.Fatal(err)
		}
		got, ok := impressFormat(el)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("%s: %q %v, want %q", tc.xml, got, ok, tc.want)
		}
	}
}
