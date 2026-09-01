package annotation

import (
	"context"
	"fmt"
	"maps"
	"sort"

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
// pdfjs issue14301.pdf has 660 named fields and `litho forms` printed
// "(no form fields)" for it, because only its 320 checkboxes carry a value. That
// also left no way to discover the names to hand to set-form.
func ExtractFormFields(ctx context.Context, doc *document.Document) ([]models.FormField, error) {
	return extractForm(ctx, doc, true)
}

// extractForm walks the interactive form once. includeEmpty selects between the
// two answers above.
func extractForm(ctx context.Context, doc *document.Document, includeEmpty bool) ([]models.FormField, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("getting catalog for AcroForm: %w", err)
	}

	acroFormObj, ok := catalog.Dict["AcroForm"]
	if !ok {
		return nil, nil // No forms in document
	}

	acroFormDict, err := document.ResolveDict(ctx, doc, acroFormObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve AcroForm: %w", err)
	}
	if acroFormDict == nil {
		return nil, fmt.Errorf("AcroForm is not a dictionary")
	}

	// Check for XFA forms first. If present, extract XFA data as the base
	// layer, then overlay any AcroForm fields on top (AcroForm values take
	// precedence when both exist, per the PDF spec's fallback semantics).
	xfaData, _ := extractXFAData(ctx, doc, acroFormDict)

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

	fieldsObj, ok := acroFormDict["Fields"]
	if !ok {
		// No AcroForm fields. Return XFA data if present.
		return xfaOnly()
	}

	fieldsArr, err := document.ResolveArray(ctx, doc, fieldsObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve Fields: %w", err)
	}
	if len(fieldsArr) == 0 {
		// Per ISO 32000-1, Table 218, /Fields may legitimately be empty: an
		// interactive form carrying only /DA, /DR or /NeedAppearances declares
		// no root fields. An empty array resolves to a nil slice, and
		// [document.ResolveArray] already reports a genuine type mismatch as an
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
	err = traverseFields(ctx, doc, fieldsArr, "", "", formDataMap, calcScripts, &order, seenName)
	if err != nil {
		return nil, fmt.Errorf("traversing form fields: %w", err)
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
		docScripts, _ := ExtractDocumentScripts(ctx, doc) //nolint:errcheck // Best-effort JS execution; PDF scripts commonly reference undefined globals.
		for _, script := range docScripts {
			_, _ = vm.RunString(script) //nolint:errcheck // Best-effort JS execution; PDF scripts commonly reference undefined globals.
		}

		// Execute field calculations
		for fieldName, script := range calcScripts {
			// PDF Calculate scripts output to `event.value`
			// We mock the event object
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

func traverseFields(ctx context.Context, doc *document.Document, fields ast.PdfArray, parentName string, inheritedFT string, formData map[string]string, calcScripts map[string]string, order *[]string, seen map[string]bool) error {
	for _, fieldObj := range fields {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("traversing form fields: %w", err)
		}
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
						formData[fullName] = string(val)
					}
				default:
					// Fallback for other types or missing FT
					switch val := vRes.(type) {
					case ast.PdfString:
						formData[fullName] = document.DecodePdfString(val)
					case ast.PdfName:
						formData[fullName] = string(val)
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

		// Traverse children (/Kids)
		hasKids := false
		if kidsObj, ok := fieldDict["Kids"]; ok {
			kidsArr, err := document.ResolveArray(ctx, doc, kidsObj)
			if err == nil && kidsArr != nil {
				hasKids = true
				if err := traverseFields(ctx, doc, kidsArr, fullName, currentFT, formData, calcScripts, order, seen); err != nil {
					return fmt.Errorf("traversing child fields: %w", err)
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
