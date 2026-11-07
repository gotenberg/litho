package compose

import "testing"

func TestPruneStructure_KeepsWhatContentOrLinksHangFrom(t *testing.T) {
	t.Parallel()
	usedText := &StructElem{Role: "P"}
	usedLink := &StructElem{Role: "Link"}
	usedDeep := &StructElem{Role: "Span"}
	emptyLeaf := &StructElem{Role: "P"}
	emptyBranch := &StructElem{Role: "Sect", Kids: []*StructElem{{Role: "P"}, {Role: "H1"}}}
	deepBranch := &StructElem{Role: "Div", Kids: []*StructElem{{Role: "Sect", Kids: []*StructElem{usedDeep}}, emptyLeaf}}
	root := &StructElem{Role: "Document", Kids: []*StructElem{usedText, emptyBranch, usedLink, deepBranch}}
	doc := &Document{
		Structure: root,
		Pages: []*Page{
			{Content: []Fragment{&Text{Parent: usedText}, &Group{Content: []Fragment{&Path{Parent: usedDeep}}}}},
			{Links: []Link{{Parent: usedLink}}},
		},
	}
	PruneStructure(doc)
	var roles []string
	var walk func(e *StructElem, depth int)
	walk = func(e *StructElem, depth int) {
		roles = append(roles, e.Role)
		for _, k := range e.Kids {
			walk(k, depth+1)
		}
	}
	walk(root, 0)
	got := ""
	for _, r := range roles {
		got += r + " "
	}
	if want := "Document P Link Div Sect Span "; got != want {
		t.Errorf("structure after pruning %q, want %q", got, want)
	}
	PruneStructure(&Document{})
	if root.Kids[0] != usedText {
		t.Error("the used element moved")
	}
}

func TestPruneStructure_EmptiesATreeWithoutContent(t *testing.T) {
	t.Parallel()
	root := &StructElem{Role: "Document", Kids: []*StructElem{{Role: "P", Kids: []*StructElem{{Role: "Span"}}}}}
	PruneStructure(&Document{Structure: root})
	if len(root.Kids) != 0 {
		t.Errorf("%d kids kept, want the bare root", len(root.Kids))
	}
}
