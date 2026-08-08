package mutations

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestStampText_EmitsSingleSlashResourceName pins that the stamp names its font
// resource with exactly one slash.
//
// [ast.PdfName.String] already supplies the slash, so formatting one with "/%s"
// produced "BT //StampF23 36 Tf": two names where Tf expects one. The font was
// never selected, so the stamp drew under whatever font was current and its own
// text extracted as that font decoded it ("ZZQQ" came back as "婚兑"). The same
// spelling in stamp-image, stamp-pdf and flatten-form named no XObject at all.
func TestStampText_EmitsSingleSlashResourceName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newMutationDoc(t, minimalPDFMutations())
	catalog, _ := document.ResolveCatalog(ctx, doc)
	pages, _ := document.ResolvePages(ctx, doc, catalog)

	if err := StampText(ctx, alloc, doc, pages, TextStampOptions{
		Text:         "ZZQQ",
		X:            10,
		Y:            10,
		FontSize:     12,
		StandardFont: "Helvetica",
	}, []int{1}); err != nil {
		t.Fatalf("StampText: %v", err)
	}

	var streams []string
	for _, obj := range alloc.Pending() {
		if st, ok := obj.(ast.PdfStream); ok {
			data, err := st.Bytes(ctx)
			if err != nil {
				continue
			}
			streams = append(streams, string(data))
		}
	}
	if len(streams) == 0 {
		t.Fatal("StampText allocated no stream")
	}

	joined := strings.Join(streams, "\n")
	if !strings.Contains(joined, " Tf") {
		t.Fatalf("no Tf operator in the stamped content:\n%s", joined)
	}
	if bytes.Contains([]byte(joined), []byte("//")) {
		t.Errorf("resource name written with a doubled slash:\n%s", joined)
	}
}
