package document

import (
	"slices"
	"testing"
)

// TestValidateEndobjFormat_IntegerBody pins that an object whose body is one or
// two integers answers the endobj EOL test like any other object. ISO
// 19005-2/3, 6.1.9 requires an EOL marker before the endobj keyword of every
// indirect object. veraPDF 1.30.2 does not judge it for such an object, since
// its parser has read the keyword while looking for the R of a reference;
// litho follows the clause (see [Document.ValidateObjectHeaderFormat]).
//
// # Corpus evidence
//
//   - facturx v2__correct_PHP_@gpFacturX_sample_*.pdf, the one file that name matches:
//     writes three stream lengths as "8746 endobj", which veraPDF passes and
//     litho reports.
func TestValidateEndobjFormat_IntegerBody(t *testing.T) {
	t.Parallel()

	data, offsets := formatViolationPDF(t,
		"<< /Type /Catalog >>\n",
		"8746 ",
		"1 2 ",
		"8746\n",
	)
	doc := formatViolationDoc(data, offsets)
	for _, tc := range []struct {
		objNum int
		want   []string
	}{
		{2, []string{"compliance: obj 2: endobj not preceded by EOL marker (0x20)"}},
		{3, []string{"compliance: obj 3: endobj not preceded by EOL marker (0x20)"}},
		{4, nil},
	} {
		got := headerMessages(t, doc.ValidateObjectHeaderFormat(offsets[tc.objNum], tc.objNum))
		if !slices.Equal(got, tc.want) {
			t.Errorf("object %d reports %q, want %q", tc.objNum, got, tc.want)
		}
	}
}
