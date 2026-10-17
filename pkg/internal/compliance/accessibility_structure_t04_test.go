package compliance

import (
	"context"
	"testing"
)

// TestCheckNamespaceRoleMapping_T04 pins ISO 14289-2 §8.2.4 t4 to veraPDF
// 1.30.2's reading: each element whose type is standard in its own namespace
// fires when following its mapping to the end reaches no standard type, and
// only then.
//
// The structure tree root holds one Document in the PDF 2.0 namespace (object
// 5), which no mapping reaches, and one child element under test. Object 6 is
// the PDF 1.7 namespace and object 7 a custom one, so a case can hang a
// /RoleMapNS on either.
//
// # Corpus evidence
//
//   - litho stdremap_ua2: the PDF 1.7 Sect the /RoleMap sends to Book, which
//     veraPDF 1.30.2 reports once and litho missed.
//   - litho remapchain_ua2: a legacy chain Div -> Section -> Sect and a
//     namespace chain Sect -> Section -> Sect, which veraPDF passes and litho
//     reported twice.
func TestCheckNamespaceRoleMapping_T04(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		roleMap string
		elem    string
		ns17    string
		custom  string
		want    int
	}{
		{
			name:    "PDF 1.7 namespace element remapped by the legacy map",
			roleMap: "<< /Document /Book >>",
			elem:    "/S /Document /NS 6 0 R",
			want:    1,
		},
		{
			name:    "no-namespace element remapped by the legacy map",
			roleMap: "<< /Document /Book >>",
			elem:    "/S /Document",
			want:    1,
		},
		{
			name:    "PDF 2.0 namespace element leaves the legacy map inert",
			roleMap: "<< /Document /Book >>",
			elem:    "/S /Document /NS 5 0 R",
		},
		{
			name:    "legacy chain through a private type to a standard one",
			roleMap: "<< /Document /Foo /Foo /Sect >>",
			elem:    "/S /Document",
		},
		{
			name:    "legacy chain closing on its own start",
			roleMap: "<< /Document /Foo /Foo /Document >>",
			elem:    "/S /Document",
			want:    1,
		},
		{
			// The cycle returns to Document in no namespace, a type distinct
			// from the PDF 1.7 Document it started from, and that one is
			// standard.
			name:    "PDF 1.7 element whose legacy cycle returns in no namespace",
			roleMap: "<< /Document /Foo /Foo /Document >>",
			elem:    "/S /Document /NS 6 0 R",
		},
		{
			name:    "standard type mapped to itself keeps its type",
			roleMap: "<< /Sect /Sect >>",
			elem:    "/S /Sect",
		},
		{
			name:    "legacy entry no element uses",
			roleMap: "<< /Sect /Book >>",
			elem:    "/S /P",
		},
		{
			name:   "namespace chain through a custom type to a standard one",
			elem:   "/S /Sect /NS 6 0 R",
			ns17:   "/RoleMapNS << /Sect [/Section 7 0 R] >>",
			custom: "/RoleMapNS << /Section [/Sect 5 0 R] >>",
		},
		{
			name: "namespace map to a custom type mapped no further",
			elem: "/S /Sect /NS 6 0 R",
			ns17: "/RoleMapNS << /Sect [/Section 7 0 R] >>",
			want: 1,
		},
		{
			name: "namespace entry no element uses",
			elem: "/S /P /NS 6 0 R",
			ns17: "/RoleMapNS << /Sect [/Section 7 0 R] >>",
		},
		{
			name:    "a PDF 1.7 namespace with a /RoleMapNS leaves the legacy map inert",
			roleMap: "<< /Sect /Book >>",
			elem:    "/S /Sect /NS 6 0 R",
			ns17:    "/RoleMapNS << /Note [/Aside 5 0 R] >>",
		},
		{
			name:   "custom namespace defines no standard type",
			elem:   "/S /Sect /NS 7 0 R",
			custom: "/RoleMapNS << /Sect [/Book 7 0 R] >>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			roleMap := ""
			if tc.roleMap != "" {
				roleMap = " /RoleMap " + tc.roleMap
			}
			doc, cat := tableDoc(t, 1,
				"<< /Type /Catalog /StructTreeRoot 2 0 R >>",
				"<< /Type /StructTreeRoot"+roleMap+" /Namespaces [5 0 R 6 0 R 7 0 R] /K [3 0 R] >>",
				"<< /Type /StructElem /S /Document /NS 5 0 R /P 2 0 R /K [4 0 R] >>",
				"<< /Type /StructElem "+tc.elem+" /P 3 0 R >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) "+tc.ns17+" >>",
				"<< /Type /Namespace /NS (http://litho.example/ns) "+tc.custom+" >>",
			)
			got := 0
			for _, v := range checkNamespaceRoleMapping(context.Background(), doc, cat, "PDF/UA-2") {
				if v.Rule == Rule_14289_2_8_2_4_t04 {
					got++
				}
			}
			if got != tc.want {
				t.Errorf("§8.2.4 t4 fired %d time(s), want %d", got, tc.want)
			}
		})
	}
}
