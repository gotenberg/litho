package mutations

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// maxStructCompareDepth caps how deep [sameObject] compares two class
// definitions. Anti-DoS guard, not spec-mandated: an attribute object nests a
// couple of levels (ISO 32000-1, 14.7.5), and a deeper one is taken to differ,
// which costs a rename and nothing else.
const maxStructCompareDepth = 16

// join reserves in the output the names s defines, renaming in s each one the
// output already defines otherwise:
//
//   - A structure type the root's /RoleMap maps (ISO 32000-1, 14.7.3) when the
//     output maps it to another type, or uses it unmapped: one /RoleMap serves
//     every element of the default namespace, so s's mapping would remap the
//     output's elements, or the output's s's. So is a type s uses unmapped
//     that the output maps, keeping the meaning it had in s, unless it is a
//     standard type the output remaps: a name mapped back to it would follow
//     the output's mapping in turn, so that one is left, and logged.
//   - A class the /ClassMap defines (ISO 32000-1, 14.7.5.2) with other
//     attributes than the output's class of that name. A class defined alike
//     is the same class, and s's elements use the output's definition.
//   - An element identifier the output already uses: an identifier names one
//     element of the document (ISO 32000-1, 14.7.2, Table 323).
//
// The renaming itself happens once the elements of s are copied (see
// [structOutput.rename]).
func (t *structOutput) join(ctx context.Context, s *structSource) error {
	s.roleRenames = make(map[ast.PdfName]ast.PdfName)
	s.classRenames = make(map[ast.PdfName]ast.PdfName)
	s.idRenames = make(map[string]string)

	roleMap, _ := resolveDictAllocFirst(ctx, s.view, s.doc, s.root["RoleMap"])
	// A type mapped to itself is the type it was, so its entry defines nothing
	// another could conflict with. Renaming its key would rename its value with
	// it, into a mapping that loops (ISO 14289-2, 8.2.4 test 2).
	identity := func(name ast.PdfName) bool { return s.nameOf(ctx, roleMap[name]) == name }
	roleNames := make(map[ast.PdfName]bool)
	for _, set := range []map[ast.PdfName]bool{t.types, s.names.types} {
		for name := range set {
			roleNames[name] = true
		}
	}
	for name := range t.roleMap {
		roleNames[name] = true
	}
	for name := range roleMap {
		roleNames[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(roleMap)) {
		if identity(name) {
			continue
		}
		if existing, has := t.roleMap[name]; has {
			if t.nameOf(ctx, existing) == s.nameOf(ctx, roleMap[name]) {
				continue
			}
		} else if !t.types[name] {
			continue
		}
		renamed := uniqueName(name, roleNames)
		roleNames[renamed] = true
		s.roleRenames[name] = renamed
	}
	for _, name := range slices.Sorted(maps.Keys(roleMap)) {
		value := roleMap[name]
		if mapped := s.nameOf(ctx, value); mapped != "" {
			value = mapped
			if renamed, ok := s.roleRenames[mapped]; ok {
				value = renamed
			}
		}
		key := name
		if renamed, ok := s.roleRenames[name]; ok {
			key = renamed
		}
		if _, has := t.roleMap[key]; !has {
			copied, err := s.copier.CopyObject(ctx, value)
			if err != nil {
				return fmt.Errorf("copying the /RoleMap: %w", err)
			}
			t.roleMap[key] = copied
		}
	}
	for _, name := range slices.Sorted(maps.Keys(s.names.types)) {
		_, mapped := roleMap[name]
		_, remapped := t.roleMap[name]
		switch {
		case mapped || !remapped:
		case compliance.IsStandardStructType(name, string(models.PDFUA1)):
			// A standard type the output remaps, which ISO 14289-2, 8.2.4
			// test 4 forbids, cannot be renamed away: a new name mapped back
			// to the standard one would follow the output's mapping in turn.
			logStructCopy(s.doc, "a standard structure type the source leaves unmapped is remapped in the output",
				"type", string(name))
		default:
			// A type the source leaves unmapped, which the output maps, keeps
			// the source's meaning under a name the output does not map.
			renamed := uniqueName(name, roleNames)
			roleNames[renamed] = true
			s.roleRenames[name] = renamed
		}
		if renamed, ok := s.roleRenames[name]; ok {
			name = renamed
		}
		t.types[name] = true
	}

	classMap, _ := resolveDictAllocFirst(ctx, s.view, s.doc, s.root["ClassMap"])
	classNames := make(map[ast.PdfName]bool)
	for name := range t.classMap {
		classNames[name] = true
	}
	for name := range classMap {
		classNames[name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(classMap)) {
		origin := structOrigin{doc: s.doc, view: s.view, obj: classMap[name]}
		key := name
		if existing, has := t.classOrigin[name]; has {
			if sameObject(ctx, existing, origin, 0) {
				continue
			}
			key = uniqueName(name, classNames)
			classNames[key] = true
			s.classRenames[name] = key
		}
		copied, err := s.copier.CopyObject(ctx, classMap[name])
		if err != nil {
			return fmt.Errorf("copying the /ClassMap: %w", err)
		}
		t.classMap[key] = copied
		t.classOrigin[key] = origin
	}

	// The identifiers the /IDTree lists, and the ones elements state that it
	// leaves out: an identifier names one element either way.
	ids := maps.Clone(s.names.ids)
	for id := range s.ids {
		ids[id] = true
	}
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		if !t.idNames[id] {
			t.idNames[id] = true
			continue
		}
		renamed := uniqueID(id, t.idNames, ids)
		t.idNames[renamed] = true
		s.idRenames[id] = renamed
	}
	return nil
}

// nameOf returns the name one of the output's objects resolves to, "" when it
// is no name.
func (t *structOutput) nameOf(ctx context.Context, obj ast.PdfObject) ast.PdfName {
	name, _ := t.resolve(ctx, obj).(ast.PdfName)
	return name
}

// nameOf returns the name one of s's objects resolves to, "" when it is no
// name.
func (s *structSource) nameOf(ctx context.Context, obj ast.PdfObject) ast.PdfName {
	resolved, err := resolveAllocFirst(ctx, s.view, s.doc, obj)
	if err != nil {
		return ""
	}
	name, _ := resolved.(ast.PdfName)
	return name
}

// uniqueName returns name with the lowest suffix "_2", "_3" and so on that
// taken does not hold.
func uniqueName(name ast.PdfName, taken map[ast.PdfName]bool) ast.PdfName {
	for n := 2; ; n++ {
		candidate := ast.PdfName(string(name) + "_" + strconv.Itoa(n))
		if !taken[candidate] {
			return candidate
		}
	}
}

// uniqueID returns id with the lowest suffix "_2", "_3" and so on that neither
// taken nor own holds.
func uniqueID(id string, taken, own map[string]bool) string {
	for n := 2; ; n++ {
		candidate := id + "_" + strconv.Itoa(n)
		if !taken[candidate] && !own[candidate] {
			return candidate
		}
	}
}

// sameObject reports whether two objects, each read through its own document,
// hold the same value: equal scalars, or arrays and dictionaries whose entries
// are the same in turn. A stream, and anything deeper than
// [maxStructCompareDepth], is taken to differ.
func sameObject(ctx context.Context, a, b structOrigin, depth int) bool {
	if depth > maxStructCompareDepth {
		return false
	}
	av, err := resolveAllocFirst(ctx, a.view, a.doc, a.obj)
	if err != nil {
		return false
	}
	bv, err := resolveAllocFirst(ctx, b.view, b.doc, b.obj)
	if err != nil {
		return false
	}
	switch x := av.(type) {
	case ast.PdfDict:
		y, ok := bv.(ast.PdfDict)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			other, has := y[key]
			if !has || !sameObject(ctx, structOrigin{a.doc, a.view, value}, structOrigin{b.doc, b.view, other}, depth+1) {
				return false
			}
		}
		return true
	case ast.PdfArray:
		y, ok := bv.(ast.PdfArray)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !sameObject(ctx, structOrigin{a.doc, a.view, x[i]}, structOrigin{b.doc, b.view, y[i]}, depth+1) {
				return false
			}
		}
		return true
	case ast.PdfStream, nil:
		return false
	}
	switch bv.(type) {
	case ast.PdfDict, ast.PdfArray, ast.PdfStream, nil:
		return false
	}
	return av == bv
}

// rename applies what [structOutput.join] decided to the copies of the
// elements of s: each renamed structure type in /S, class in /C and
// identifier in /ID, and each identifier a table cell's /Headers attribute
// names (ISO 32000-1, 14.8.5.7, Table 349).
func (t *structOutput) rename(ctx context.Context, s *structSource) {
	if len(s.roleRenames) == 0 && len(s.classRenames) == 0 && len(s.idRenames) == 0 {
		return
	}
	mapping := s.copier.Mapping()
	attrs := make(map[int]bool)
	for _, num := range slices.Sorted(maps.Keys(s.prune.memo)) {
		if !s.prune.kept(num) {
			continue
		}
		dst, copied := mapping[num]
		if !copied {
			continue
		}
		elem, _ := t.alloc.Pending()[dst].(ast.PdfDict)
		t.renameElement(ctx, s, elem, attrs, 0)
	}
}

// renameElement applies the renames of s to one copied element, and to the
// elements written directly into its /K.
func (t *structOutput) renameElement(ctx context.Context, s *structSource, elem ast.PdfDict, attrs map[int]bool, depth int) {
	if elem == nil || depth > maxStructTreeDepth {
		return
	}
	if _, hasNS := elem["NS"]; !hasNS {
		if typ, isName := elem["S"].(ast.PdfName); isName {
			if renamed, ok := s.roleRenames[typ]; ok {
				elem["S"] = renamed
			}
		}
	}
	switch c := elem["C"].(type) {
	case ast.PdfName:
		if renamed, ok := s.classRenames[c]; ok {
			elem["C"] = renamed
		}
	case ast.PdfArray:
		// Class names interleave with revision numbers (ISO 32000-1, 14.7.5.2).
		out := slices.Clone(c)
		for i, entry := range out {
			if name, isName := entry.(ast.PdfName); isName {
				if renamed, ok := s.classRenames[name]; ok {
					out[i] = renamed
				}
			}
		}
		elem["C"] = out
	}
	if id, isString := elem["ID"].(ast.PdfString); isString {
		if renamed, ok := s.idRenames[string(id)]; ok {
			elem["ID"] = ast.PdfString(renamed)
		}
	}
	if len(s.idRenames) > 0 {
		t.renameHeaders(ctx, s, elem["A"], attrs)
	}
	for _, kid := range t.kidList(ctx, elem["K"]) {
		direct, isDict := kid.(ast.PdfDict)
		if !isDict {
			continue
		}
		if kind, _ := direct["Type"].(ast.PdfName); kind == "MCR" || kind == "OBJR" {
			continue
		}
		t.renameElement(ctx, s, direct, attrs, depth+1)
	}
}

// renameHeaders renames the identifiers each attribute object of an element's
// /A lists in /Headers. /A holds one attribute object or an array of them
// interleaved with revision numbers (ISO 32000-1, 14.7.5); one written as an
// object of its own is renamed once, however many elements share it.
func (t *structOutput) renameHeaders(ctx context.Context, s *structSource, aObj ast.PdfObject, attrs map[int]bool) {
	var objs ast.PdfArray
	switch v := t.resolve(ctx, aObj).(type) {
	case ast.PdfDict:
		objs = ast.PdfArray{aObj}
	case ast.PdfArray:
		objs = v
	}
	for _, obj := range objs {
		if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
			if attrs[ref.ObjectNum] {
				continue
			}
			attrs[ref.ObjectNum] = true
		}
		// Only the copy's own objects are edited: an attribute object the copy
		// did not write belongs to the target document.
		attr := t.pendingDict(obj)
		if attr == nil {
			continue
		}
		headers, isArray := t.resolve(ctx, attr["Headers"]).(ast.PdfArray)
		if !isArray {
			continue
		}
		out := slices.Clone(headers)
		for i, entry := range out {
			if id, isString := t.resolve(ctx, entry).(ast.PdfString); isString {
				if renamed, ok := s.idRenames[string(id)]; ok {
					out[i] = ast.PdfString(renamed)
				}
			}
		}
		attr["Headers"] = out
	}
}
