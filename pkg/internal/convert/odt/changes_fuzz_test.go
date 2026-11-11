package odt

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// FuzzResolveChanges rewrites arbitrary flat documents into each
// tracked-changes view: it must neither panic nor loop, and the body must
// still build.
func FuzzResolveChanges(f *testing.F) {
	flat := func(body string) []byte {
		return []byte(`<office:document ` + nsDecl + `><office:body><office:text>` + body + `</office:text></office:body></office:document>`)
	}
	seed := flat(`<text:tracked-changes>` + region("i", "insertion", "A", "") + region("d", "deletion", "B", `<text:p>x</text:p><text:p>y</text:p>`) +
		`</text:tracked-changes><text:p>a<text:change-start text:change-id="i"/>b</text:p><text:p>c<text:change-end text:change-id="i"/><text:change text:change-id="d"/>e</text:p>`)
	f.Add(seed, uint8(0))
	f.Add(seed[:len(seed)/2], uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, view uint8) {
		pkg, err := odf.Open(context.Background(), data, odf.Options{})
		if err != nil {
			return
		}
		body := odf.Section(pkg.Content, "body").Child(odf.NSOffice, "text")
		if body == nil {
			return
		}
		b := newFuzzBuilder(pkg)
		marks, err := resolveChanges(context.Background(), body, office.TrackedChanges(view%3), b.rowTracked, b.warn)
		if err != nil {
			t.Fatal(err)
		}
		b.changeMarks = marks
		if root, _ := b.document(body); root == nil {
			t.Fatal("no root")
		}
	})
}
