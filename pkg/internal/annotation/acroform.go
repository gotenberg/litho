package annotation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dop251/goja"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractFormData extracts interactive form data (AcroForms) from the document.
// It also executes PDF JavaScript calculation scripts to resolve dynamic fields.
// It returns a list of FormFields.
func ExtractFormData(ctx context.Context, doc *document.Document) ([]models.FormField, error) {
	return extractForm(ctx, doc, false)
}

// ExtractFormFields lists every named field of the interactive form, including
// the ones nobody has filled in, in the order the field tree gives them.
//
// [ExtractFormData] reports only fields that carry a /V, which is what a caller
// asking "what did the user type" wants. Listing a form needs the other answer:
// reporting only valued fields hid the unfilled ones from `litho forms`, and
// left no way to discover the names to hand to set-form.
//
// # Corpus evidence
//
//   - pdfjs issue14301.pdf: has 660 named fields, and `litho forms` printed
//     "(no form fields)" for it, because only its 320 checkboxes carry a
//     value.
func ExtractFormFields(ctx context.Context, doc *document.Document) ([]models.FormField, error) {
	return extractForm(ctx, doc, true)
}

// HasPageWidgetField reports whether any page carries a widget annotation that
// presents a form field.
//
// The interactive form lists its fields in /AcroForm (ISO 32000-1, 12.7.2) and
// the pages carry the widgets that present them (12.5.6.19). A producer may
// write only the widget, so asking the catalog alone answers "no" for a document
// whose fields `litho forms` lists.
//
// The walk stops at the first field found, so a document with a large form costs
// no more than one.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: has 27 fields that `litho forms` lists and no
//     /AcroForm at all.
func HasPageWidgetField(ctx context.Context, doc *document.Document) bool {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return false
	}

	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return false
	}
	for _, page := range pages {
		if ctx.Err() != nil {
			return false
		}
		annotsObj, ok := page.Dict["Annots"]
		if !ok {
			continue
		}
		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			continue
		}
		for _, annotObj := range annots {
			if _, ok := widgetFieldRoot(ctx, doc, annotObj); ok {
				return true
			}
		}
	}
	return false
}

// maxFieldParentDepth bounds the walk up a widget's /Parent chain. It is an
// anti-DoS guard, not spec-mandated: ISO 32000-1, 12.7.3 describes a shallow
// field hierarchy, and a crafted file whose /Parent points back at a descendant
// would otherwise loop.
//
// The same ascent is bounded in mutations (three constants), in remediation and
// in compliance, at 32, 64 and 100. See mutations/acroform_split_merge.go for
// the measurement that puts every one of those far above anything real, and
// [document.ResolveFieldParent] for the visited set that does the actual work
// of ending a cyclic chain.
const maxFieldParentDepth = 64

// unregisteredWidgetFields returns the form fields a page carries that the
// interactive form's /Fields array does not list.
//
// A widget annotation either is the field (its dictionary carries /FT and /T)
// or presents one through /Parent (ISO 32000-1, 12.5.6.19). The field the
// widget belongs to is reported, so a shared parent is returned once.
func unregisteredWidgetFields(
	ctx context.Context,
	doc *document.Document,
	catalog *document.Catalog,
	registered ast.PdfArray,
) ast.PdfArray {
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		doc.Logger.Debug("resolving pages for form widgets", "err", err)
		return nil
	}

	known := make(map[int]bool, len(registered))
	for _, f := range registered {
		if ref, ok := f.(ast.PdfIndirectRef); ok {
			known[ref.ObjectNum] = true
		}
	}

	var out ast.PdfArray
	for _, page := range pages {
		if ctx.Err() != nil {
			return out
		}
		annotsObj, ok := page.Dict["Annots"]
		if !ok {
			continue
		}
		annotsArr, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			doc.Logger.Debug("resolving /Annots for form widgets", "err", err)
			continue
		}
		for _, annotObj := range annotsArr {
			fieldObj, ok := widgetFieldRoot(ctx, doc, annotObj)
			if !ok {
				continue
			}
			ref, ok := fieldObj.(ast.PdfIndirectRef)
			if !ok || known[ref.ObjectNum] {
				continue
			}
			known[ref.ObjectNum] = true
			out = append(out, fieldObj)
		}
	}
	return out
}

// widgetFieldRoot returns the outermost field object a widget belongs to, and
// whether the widget names a form field at all.
func widgetFieldRoot(ctx context.Context, doc *document.Document, annotObj ast.PdfObject) (ast.PdfObject, bool) {
	annotDict, err := document.ResolveDict(ctx, doc, annotObj)
	if err != nil || annotDict == nil {
		return nil, false
	}
	if subtype, err := document.ResolveName(ctx, doc, annotDict["Subtype"]); err != nil || subtype != "Widget" {
		return nil, false
	}

	current, currentDict := annotObj, annotDict
	seen := make(map[int]bool)
	for range maxFieldParentDepth {
		parentObj, parentDict, ok := document.ResolveFieldParent(ctx, doc, currentDict, seen)
		if !ok {
			break
		}
		current, currentDict = parentObj, parentDict
	}

	// A field carries a type, inherited or its own. Without one the widget is
	// decoration rather than a form field.
	//
	// This search keeps its own visited set rather than sharing the ascent's.
	// It resumes at the node the ascent stopped on, which that set already
	// records, so a shared one would reject its own first hop. Usually it has
	// no hop to make: the ascent ends at a parentless field whose /FT is read
	// right here. The two sets matter when the ascent stopped early instead, on
	// a cycle or a parent that does not resolve.
	ftSeen := make(map[int]bool)
	node, depth := currentDict, 0
	for node != nil && depth < maxFieldParentDepth {
		if _, ok := node["FT"]; ok {
			return current, true
		}
		_, parent, ok := document.ResolveFieldParent(ctx, doc, node, ftSeen)
		if !ok {
			return nil, false
		}
		node = parent
		depth++
	}
	return nil, false
}

// extractForm walks the interactive form once. includeEmpty selects between the
// answers of [ExtractFormData] and [ExtractFormFields].
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: has 241 fields the interactive form does not
//     list, and `litho forms` reported none of them.
func extractForm(ctx context.Context, doc *document.Document, includeEmpty bool) ([]models.FormField, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("getting catalog for AcroForm: %w", err)
	}

	// A document may carry fields the interactive form does not list. ISO
	// 32000-1, 12.7.2 says /AcroForm holds them, and 12.5.6.19 says the page
	// holds the widget that presents one; producers write the widget and forget
	// the registration, so the page is walked even when the catalog states no
	// form at all.
	acroFormObj, hasAcroForm := catalog.Dict["AcroForm"]
	var acroFormDict ast.PdfDict
	if hasAcroForm {
		acroFormDict, err = document.ResolveDict(ctx, doc, acroFormObj)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve AcroForm: %w", err)
		}
		if acroFormDict == nil {
			return nil, fmt.Errorf("AcroForm is not a dictionary")
		}
	}

	// Check for XFA forms first. If present, extract XFA data as the base
	// layer, then overlay any AcroForm fields on top (AcroForm values take
	// precedence when both exist, per the PDF spec's fallback semantics).
	var xfaData map[string]string
	if acroFormDict != nil {
		var xfaErr error
		xfaData, xfaErr = extractXFAData(ctx, doc, acroFormDict)
		if xfaErr != nil {
			// An XFA packet that is malformed, unresolvable or past
			// maxXFADataSize must not cost the caller the AcroForm fields.
			// The two are separate layers and the AcroForm half is still
			// readable, so report it alone rather than failing the form.
			doc.Logger.Debug("extracting XFA data; returning AcroForm fields only",
				slog.Any("err", xfaErr))
		}
	}

	xfaOnly := func() ([]models.FormField, error) {
		if len(xfaData) == 0 {
			return nil, nil
		}
		names := make([]string, 0, len(xfaData))
		for k := range xfaData {
			names = append(names, k)
		}
		sort.Strings(names)
		results := make([]models.FormField, 0, len(names))
		for _, k := range names {
			results = append(results, models.FormField{Name: k, Value: xfaData[k]})
		}
		return results, nil
	}

	var fieldsArr ast.PdfArray
	if acroFormDict != nil {
		if fieldsObj, ok := acroFormDict["Fields"]; ok {
			fieldsArr, err = document.ResolveArray(ctx, doc, fieldsObj)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve Fields: %w", err)
			}
		}
	}
	pageFields := unregisteredWidgetFields(ctx, doc, catalog, fieldsArr)
	if len(fieldsArr) == 0 && len(pageFields) == 0 {
		// Per ISO 32000-1, Table 218, /Fields may legitimately be empty: an
		// interactive form carrying only /DA, /DR or /NeedAppearances declares
		// no root fields. An empty array resolves to a nil slice, and
		// document.ResolveArray already reports a genuine type mismatch as an
		// error, so this is "no root fields" rather than a malformed entry.
		// Rejecting it made `litho forms` fail outright on a conformant file.
		return xfaOnly()
	}

	// Start with XFA data as the base, then overlay AcroForm fields.
	formDataMap := make(map[string]string)
	maps.Copy(formDataMap, xfaData)

	// Track fields that have calculation scripts
	calcScripts := make(map[string]string)

	var order []string
	seenName := make(map[string]bool)
	if len(fieldsArr) > 0 {
		err = traverseFields(ctx, doc, fieldsArr, "", "", formDataMap, calcScripts, &order, seenName)
		if err != nil {
			return nil, fmt.Errorf("traversing form fields: %w", err)
		}
	}
	if len(pageFields) > 0 {
		err = traverseFields(ctx, doc, pageFields, "", "", formDataMap, calcScripts, &order, seenName)
		if err != nil {
			return nil, fmt.Errorf("traversing page form fields: %w", err)
		}
	}

	// If there are calculation scripts, evaluate them using goja
	if len(calcScripts) > 0 {
		vm := goja.New()

		// Inject mock this.getField("Name").value API.
		// goja Set/RunString calls for VM bootstrap are best-effort: these trivial
		// assignments and object property sets cannot fail in practice.
		_ = vm.Set("getField", func(call goja.FunctionCall) goja.Value { //nolint:errcheck // Best-effort JS execution; PDF scripts commonly reference undefined globals.
			if len(call.Arguments) > 0 {
				fieldName := call.Arguments[0].String()

				// Create a mock Field object
				fieldObj := vm.NewObject()
				_ = fieldObj.Set("value", "") //nolint:errcheck // Best-effort JS execution; PDF scripts commonly reference undefined globals.
				if val, exists := formDataMap[fieldName]; exists {
					_ = fieldObj.Set("value", val) //nolint:errcheck // Best-effort JS execution; PDF scripts commonly reference undefined globals.
				}

				// Provide a setter to update formData
				return fieldObj
			}
			return goja.Undefined()
		})

		// Map 'this.getField' -> global 'getField'
		_, _ = vm.RunString(`var this = { getField: getField };`) //nolint:errcheck // Best-effort JS execution; PDF scripts commonly reference undefined globals.

		// Inject Document-level scripts first (best-effort: malformed JS is non-fatal)
		docScripts, scriptsErr := ExtractDocumentScripts(ctx, doc)
		if scriptsErr != nil {
			// Not a goja failure like the calls around it. This one is the
			// name-tree walk declining to yield scripts at all, a malformed
			// /Names dictionary or a tree past maxDocumentScriptsSize. Field
			// calculations still run, they just run without the
			// document-level scripts.
			doc.Logger.Debug("extracting document-level JavaScript; continuing without it",
				slog.Any("err", scriptsErr))
		}
		for _, script := range docScripts {
			_, _ = vm.RunString(script) //nolint:errcheck // Best-effort JS execution; PDF scripts commonly reference undefined globals.
		}

		// Execute field calculations
		for fieldName, script := range calcScripts {
			// PDF Calculate scripts output to `event.value`
			// Mock the event object.
			_, _ = vm.RunString(`var event = { value: "" };`) //nolint:errcheck // Best-effort JS execution; PDF scripts commonly reference undefined globals.

			// Execute the specific calculation logic
			_, err := vm.RunString(script)
			if err == nil {
				// Retrieve the calculated value
				eventVal := vm.Get("event").ToObject(vm).Get("value").String()
				if eventVal != "" && eventVal != "undefined" {
					formDataMap[fieldName] = eventVal
				}
			}
		}
	}

	if !includeEmpty {
		// Filled-in values only, in document order. Ranging the map emitted a
		// different order on every run, which the writer's determinism rule
		// forbids and which made `litho forms` output unstable.
		var filled []models.FormField
		for _, name := range order {
			if v, ok := formDataMap[name]; ok {
				filled = append(filled, models.FormField{Name: name, Value: v})
			}
		}
		// XFA supplies names the AcroForm tree never lists; keep them, sorted so
		// the result stays stable.
		var extra []string
		for name := range formDataMap {
			if !seenName[name] {
				extra = append(extra, name)
			}
		}
		sort.Strings(extra)
		for _, name := range extra {
			filled = append(filled, models.FormField{Name: name, Value: formDataMap[name]})
		}
		return filled, nil
	}

	var results []models.FormField
	for _, name := range order {
		results = append(results, models.FormField{Name: name, Value: formDataMap[name]})
	}
	var extra []string
	for name := range formDataMap {
		if !seenName[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		results = append(results, models.FormField{Name: name, Value: formDataMap[name]})
	}
	return results, nil
}

// choiceValueSeparator joins the selections of a multi-select list box into the
// single string [models.FormField] carries. Not a spec value: ISO 32000-1,
// 12.7.4.4 stores them as an array and says nothing about presenting them.
const choiceValueSeparator = ", "

// nameValue renders a field value written as a name.
//
// A name is a byte sequence (ISO 32000-1, 7.3.5), so an appearance state like
// /Off or /Yes passes through unchanged. Producers do write text there, though,
// such as a UTF-16BE checkbox name: returning the raw bytes put a string that is
// not valid UTF-8 into [models.FormField], which the JSON encoder then rendered
// as replacement characters. Decoding is limited to the bytes that are already
// unusable as they stand, so every ASCII appearance state keeps comparing equal
// to the /AP key that names it.
//
// # Corpus evidence
//
//   - pdfjs issue19753.pdf: gives a checkbox a UTF-16BE name.
func nameValue(v ast.PdfName) string {
	if utf8.ValidString(string(v)) {
		return string(v)
	}
	return document.DecodePdfString(ast.PdfString(v))
}

// joinChoiceValue renders an array /V as text.
//
// Per ISO 32000-1, 12.7.4.4, Table 230, a choice field's value is a text string,
// or an array of them when the field allows multiple selection. Only the scalar
// forms were read, so every multi-select list box reported an empty value.
//
// # Corpus evidence
//
//   - pdfjs annotation-choice-widget.pdf: holds
//     ["Ipsum" "Amet" "Consectetur" "Elit"] in one multi-select list box, and
//     `litho forms` printed nothing for it.
func joinChoiceValue(ctx context.Context, doc *document.Document, arr ast.PdfArray) string {
	parts := make([]string, 0, len(arr))
	for _, item := range arr {
		res, err := document.ResolveObject(ctx, doc, item)
		if err != nil {
			doc.Logger.Debug("resolving choice field selection", slog.Any("err", err))
			continue
		}
		switch v := res.(type) {
		case ast.PdfString:
			parts = append(parts, document.DecodePdfString(v))
		case ast.PdfName:
			parts = append(parts, string(v))
		}
	}
	return strings.Join(parts, choiceValueSeparator)
}

// traverseFields records into formData, calcScripts and order the fields of
// the list fields and their descendants, under a parent whose fully qualified
// name is parentName and whose inheritable /FT is inheritedFT (ISO 32000-1,
// 12.7.3.1, Table 220). See [traverseFieldsAt].
func traverseFields(ctx context.Context, doc *document.Document, fields ast.PdfArray, parentName string, inheritedFT string, formData map[string]string, calcScripts map[string]string, order *[]string, seen map[string]bool) error {
	tr := &fieldTraversal{onPath: make(map[int]bool), walked: make(map[fieldVisit]bool)}
	return traverseFieldsAt(ctx, doc, fields, parentName, inheritedFT, formData, calcScripts, order, seen, tr, 0)
}

// maxFieldTreeDepth bounds the descent DOWN a field's /Kids in
// [traverseFieldsAt]. It is an anti-DoS guard, not spec-mandated: ISO 32000-1,
// 12.7.3 describes a field hierarchy only a few levels deep, and a chain of
// direct fields nested thousands deep would otherwise grow the goroutine stack
// with it. The signature package bounds its /Kids descent at the same value,
// and compliance at 100; [maxFieldParentDepth] bounds the /Parent ascent, the
// opposite direction.
const maxFieldTreeDepth = 64

// maxFieldTreeVisits bounds the fields one [traverseFields] walk reads. It is
// an anti-DoS guard, not spec-mandated. The walk reads a field once per fully
// qualified name, and names are paths: fields that share a child under two
// differently named parents give it two names, and a chain of such pairs gives
// 2^depth names from a file that grows linearly with the depth. A real form
// reads each of its fields once, and 1<<18 sits far above the largest.
//
// # Corpus evidence
//
//   - All corpora: the largest form tree holds 35200 fields and widgets, and
//     no field names its /Kids by reference or is reached twice.
const maxFieldTreeVisits = 1 << 18

// fieldVisit keys one unit of a [traverseFields] walk: an indirect field, or a
// /Kids array written as an object of its own, reached under the fully
// qualified name parent with the inherited field type ft. The entries the walk
// records below it depend on nothing else, so a unit reached again adds
// nothing.
type fieldVisit struct {
	obj        int
	parent, ft string
}

// fieldTraversal carries the state of one [traverseFields] walk.
type fieldTraversal struct {
	// onPath holds the fields, and the /Kids arrays written as objects of
	// their own, on the current path: one reached again there closes a cycle.
	onPath map[int]bool
	// walked holds the units already walked (see [fieldVisit]).
	walked map[fieldVisit]bool
	// visits counts the fields read, up to [maxFieldTreeVisits].
	visits int
}

// firstVisit reports whether obj, reached under parent with the inherited type
// ft, is a unit this walk has not walked yet, and records it. A direct object
// lives in the one container that spells it out, so only its container can be
// reached twice.
func (tr *fieldTraversal) firstVisit(obj ast.PdfObject, parent, ft string) bool {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		return true
	}
	key := fieldVisit{obj: ref.ObjectNum, parent: parent, ft: ft}
	if tr.walked[key] {
		return false
	}
	tr.walked[key] = true
	return true
}

// enter marks obj on the current path, and reports false when it is on it
// already. A direct object carries no object number and cannot name itself, so
// it closes no cycle and needs no mark.
func (tr *fieldTraversal) enter(obj ast.PdfObject) bool {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		return true
	}
	if tr.onPath[ref.ObjectNum] {
		return false
	}
	tr.onPath[ref.ObjectNum] = true
	return true
}

// leave clears the mark [fieldTraversal.enter] put on obj.
func (tr *fieldTraversal) leave(obj ast.PdfObject) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		delete(tr.onPath, ref.ObjectNum)
	}
}

// traverseFieldsAt is [traverseFields] carrying the walk's state. A /Kids
// entry naming an ancestor is a cycle: unguarded, a field whose /Kids named
// itself recursed until the goroutine stack was exhausted, a fatal runtime
// error no recover() converts, reached from Reader.FormData and
// Reader.FormFields.
//
// seen is NOT that guard, despite its name and type: it de-duplicates the
// reported ORDER, is keyed on the qualified name rather than on identity, and
// is shared across both calls in ExtractFormData so a field listed in /Fields
// and reached again from a page widget is reported once. A cyclic /Kids grows
// fullName at every level, so seen can never match and never bounded anything.
//
// The path marks are cleared on the way back out rather than kept. This walk
// collects per fully qualified name, which depends on the path taken, so the
// same field under a second parent is a different entry that must still be
// recorded: a mark that outlived its path would silently drop form data. They
// cover the /Kids arrays written as objects of their own as well as the
// fields, since the direct fields such an array holds can each name it again.
//
// What bounds the work is tr.walked, not the path: a unit reached again under
// the same name and inherited type records the same entries, so it is walked
// once ([fieldVisit]). A /Kids array that names one field twice, or that the
// direct fields it holds name again, would otherwise fan out 2^depth up to
// [maxFieldTreeDepth]. Fields that give a shared child distinct names are
// bounded by [maxFieldTreeVisits].
func traverseFieldsAt(ctx context.Context, doc *document.Document, fields ast.PdfArray, parentName string, inheritedFT string, formData map[string]string, calcScripts map[string]string, order *[]string, seen map[string]bool, tr *fieldTraversal, depth int) error {
	if depth > maxFieldTreeDepth {
		return nil
	}
	for _, fieldObj := range fields {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("traversing form fields: %w", err)
		}
		if !tr.firstVisit(fieldObj, parentName, inheritedFT) {
			continue
		}
		if tr.visits >= maxFieldTreeVisits {
			doc.Logger.Debug("traversing form fields: the field budget is spent, the rest of the tree is not read",
				slog.Int("visits", tr.visits))
			return nil
		}
		tr.visits++
		fieldDict, err := document.ResolveDict(ctx, doc, fieldObj)
		if err != nil || fieldDict == nil {
			continue
		}

		// Extract partial name (/T)
		partialName := ""
		if tObj, ok := fieldDict["T"]; ok {
			tStr, err := document.ResolveString(ctx, doc, tObj)
			if err == nil && tStr != "" {
				partialName = document.DecodePdfString(tStr)
			}
		}

		// Construct fully qualified name
		fullName := parentName
		if partialName != "" {
			if fullName != "" {
				fullName = fullName + "." + partialName
			} else {
				fullName = partialName
			}
		}

		// Field Type (/FT) can be inherited
		currentFT := inheritedFT
		if ftObj, ok := fieldDict["FT"]; ok {
			ftName, err := document.ResolveName(ctx, doc, ftObj)
			if err == nil && ftName != "" {
				currentFT = string(ftName)
			}
		}

		// Extract Value (/V)
		if vObj, ok := fieldDict["V"]; ok {
			vRes, err := document.ResolveObject(ctx, doc, vObj)

			if err == nil && fullName != "" {
				switch currentFT {
				case "Tx":
					// Text field
					if val, ok := vRes.(ast.PdfString); ok {
						formData[fullName] = document.DecodePdfString(val)
					}
				case "Btn":
					// Button or Checkbox
					if val, ok := vRes.(ast.PdfName); ok {
						formData[fullName] = nameValue(val)
					}
				default:
					// Fallback for other types or missing FT
					switch val := vRes.(type) {
					case ast.PdfString:
						formData[fullName] = document.DecodePdfString(val)
					case ast.PdfName:
						formData[fullName] = nameValue(val)
					case ast.PdfArray:
						formData[fullName] = joinChoiceValue(ctx, doc, val)
					}
				}
			}
		}

		// Look for Calculation Scripts in Additional Actions (/AA)
		if fullName != "" {
			if aaObj, ok := fieldDict["AA"]; ok {
				aaDict, err := document.ResolveDict(ctx, doc, aaObj)
				if err == nil && aaDict != nil {
					// Check for Calculate (/C) action
					if cObj, ok := aaDict["C"]; ok {
						cActionDict, err := document.ResolveDict(ctx, doc, cObj)
						if err == nil && cActionDict != nil {
							if sObj, ok := cActionDict["S"]; ok {
								sName, err := document.ResolveName(ctx, doc, sObj)
								if err == nil && sName == "JavaScript" {
									if jsObj, ok := cActionDict["JS"]; ok {
										jsRes, err := document.ResolveObject(ctx, doc, jsObj)
										if err == nil {
											if jsStr, ok := jsRes.(ast.PdfString); ok {
												calcScripts[fullName] = document.DecodePdfString(jsStr)
											} else if jsStream, ok := jsRes.(ast.PdfStream); ok {
												data, err := doc.DecodeStream(ctx, jsStream)
												if err == nil {
													calcScripts[fullName] = string(data)
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}

		// Traverse children (/Kids), unless this field already sits on the path.
		hasKids := false
		if kidsObj, ok := fieldDict["Kids"]; ok {
			kidsArr, err := document.ResolveArray(ctx, doc, kidsObj)
			if err == nil && kidsArr != nil {
				// hasKids is set even when the descent is skipped: it records
				// that this field HAS children, which is what stops it being
				// reported as a terminal field below. Skipping a cycle must
				// bound the walk without changing what gets reported.
				hasKids = true
				if err := traverseKids(ctx, doc, fieldObj, kidsObj, kidsArr, fullName, currentFT, formData, calcScripts, order, seen, tr, depth); err != nil {
					return err
				}
			}
		}

		// Record the terminal field itself, whether or not it carries a value.
		// A field is named by /FT or /T (ISO 32000-1, Table 220); producers also
		// drop plain annotations into /Fields, and those name no field at all.
		if !hasKids && fullName != "" && order != nil && !seen[fullName] {
			if _, isField := fieldDict["FT"]; isField || currentFT != "" || partialName != "" {
				seen[fullName] = true
				*order = append(*order, fullName)
			}
		}
	}
	return nil
}

// traverseKids walks kids, the /Kids array kidsObj of the field fieldObj named
// fullName, for [traverseFieldsAt]: once per unit ([fieldVisit]), and not at
// all when the field or the array is already on the path.
func traverseKids(ctx context.Context, doc *document.Document, fieldObj, kidsObj ast.PdfObject, kids ast.PdfArray, fullName, currentFT string, formData map[string]string, calcScripts map[string]string, order *[]string, seen map[string]bool, tr *fieldTraversal, depth int) error {
	if !tr.enter(fieldObj) {
		return nil
	}
	defer tr.leave(fieldObj)
	if !tr.enter(kidsObj) {
		return nil
	}
	defer tr.leave(kidsObj)
	if !tr.firstVisit(kidsObj, fullName, currentFT) {
		return nil
	}
	if err := traverseFieldsAt(ctx, doc, kids, fullName, currentFT, formData, calcScripts, order, seen, tr, depth+1); err != nil {
		return fmt.Errorf("traversing child fields: %w", err)
	}
	return nil
}
