package tagging

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// sharedObjectPage sets a page as one text object: a 9-point running head at
// the top of the page, the body in 12-point lines under it, and the page
// number at the foot, all shown from the one BT to the one ET.
func sharedObjectPage(header, foot string) string {
	return "BT /F1 9 Tf 72 760 Td (" + header + ") Tj\n" +
		"/F1 12 Tf 0 -60 Td (Body text set in the same text object as the head.) Tj\n" +
		"0 -14 Td (A second line of the same paragraph of body text.) Tj\n" +
		"0 -14 Td (A third line, the last of the paragraph on the page.) Tj\n" +
		"/F1 9 Tf 228 -628 Td (" + foot + ") Tj\nET\n"
}

// nestingError returns how the marked-content sequences of a content stream
// fail to nest with its text objects, or "" when they nest (ISO 32000-1,
// 14.6).
func nestingError(t *testing.T, data string) string {
	t.Helper()
	ops, err := content.ParseStreamData(context.Background(), []byte(data))
	if err != nil {
		t.Fatalf("parse marked content: %v", err)
	}
	var open []string
	for _, op := range ops {
		switch op.Operator {
		case "BT":
			open = append(open, "BT")
		case "BDC", "BMC":
			open = append(open, "MC")
		case "ET", "EMC":
			want := "BT"
			if op.Operator == "EMC" {
				want = "MC"
			}
			if len(open) == 0 || open[len(open)-1] != want {
				return op.Operator + " closes " + strings.Join(open, " ")
			}
			open = open[:len(open)-1]
		}
	}
	if len(open) > 0 {
		return "left open: " + strings.Join(open, " ")
	}
	return ""
}

// TestAutoTag_CutsFurnitureOffSharedTextObject pins ISO 14289-1, 7.8: a
// running head and a page number are artifacts even when the producer sets
// them in the text object of the page's body. The head is cut off at the line
// that leaves the top margin and the page number at the line that enters the
// bottom one, and every sequence the cut writes stays between the BT and the
// ET, as ISO 32000-1, 14.6 asks.
func TestAutoTag_CutsFurnitureOffSharedTextObject(t *testing.T) {
	t.Parallel()

	marked, roles := autoTagPages(t, []string{
		sharedObjectPage("River Survey Report", "1"),
		sharedObjectPage("River Survey Report", "2"),
		sharedObjectPage("River Survey Report", "3"),
	})
	for i, m := range marked {
		flat := strings.Join(strings.Fields(m), " ")
		if !strings.Contains(flat, "BT "+strings.TrimSpace(headerArtifact)+" /F1 9 Tf 72 760 Td (River Survey Report) Tj EMC") {
			t.Errorf("page %d: head not cut off as a header artifact:\n%s", i+1, m)
		}
		if !strings.Contains(flat, strings.TrimSpace(footerArtifact)+fmt.Sprintf(" /F1 9 Tf 228 -628 Td (%d) Tj EMC ET", i+1)) {
			t.Errorf("page %d: page number not cut off as a footer artifact:\n%s", i+1, m)
		}
		if !strings.Contains(flat, "/P <</MCID 0>> BDC /F1 12 Tf 0 -60 Td (Body text") {
			t.Errorf("page %d: body not tagged inside the text object:\n%s", i+1, m)
		}
		if msg := nestingError(t, m); msg != "" {
			t.Errorf("page %d: sequences do not nest with the text object: %s\n%s", i+1, msg, m)
		}
	}
	if len(roles) != 3 {
		t.Errorf("Document has %d children, want the 3 body paragraphs", len(roles))
	}
	for _, r := range roles {
		if r != ast.PdfName(RoleP) {
			t.Errorf("Document child %s, want only body paragraphs", r)
		}
	}
}

// TestAutoTag_SharedTextObjectWithoutFurnitureStaysWhole pins that a text
// object is cut only for furniture: a head that repeats on no other page
// leaves the object one block, as before.
func TestAutoTag_SharedTextObjectWithoutFurnitureStaysWhole(t *testing.T) {
	t.Parallel()

	marked, _ := autoTagPages(t, []string{
		sharedObjectPage("Chapter one", "Draft"),
		sharedObjectPage("A different head", "Final note"),
		sharedObjectPage("Yet another title", "See appendix"),
	})
	for i, m := range marked {
		if strings.Contains(m, "/Pagination") {
			t.Errorf("page %d: cut a head that does not run:\n%s", i+1, m)
		}
		if !strings.HasPrefix(strings.TrimSpace(m), "/P <</MCID 0>> BDC\nBT") {
			t.Errorf("page %d: text object not tagged whole:\n%s", i+1, m)
		}
	}
}

// TestAutoTag_RunningHeadsOfEachHand pins that a book's left-hand and
// right-hand pages, each with a running head of its own, have both taken for
// furniture: each head runs on half the pages, never a majority of them.
func TestAutoTag_RunningHeadsOfEachHand(t *testing.T) {
	t.Parallel()

	var pages []string
	for i := range 6 {
		head := "Journal of River Studies"
		if i%2 == 1 {
			head = "Smith and Jones: Sediment"
		}
		pages = append(pages, furniturePage(head, i+1))
	}
	marked, _ := autoTagPages(t, pages)
	for i, m := range marked {
		head := "Journal of River Studies"
		if i%2 == 1 {
			head = "Smith and Jones: Sediment"
		}
		if !markedAs(m, headerArtifact, "BT /F1 9 Tf 72 760 Td ("+head+") Tj ET") {
			t.Errorf("page %d: running head of its hand not a header artifact:\n%s", i+1, m)
		}
	}
}
