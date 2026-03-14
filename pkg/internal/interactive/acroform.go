package interactive

import (
	"fmt"
	"maps"

	"github.com/dop251/goja"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractFormData extracts interactive form data (AcroForms) from the document.
// It also executes PDF JavaScript calculation scripts to resolve dynamic fields.
// It returns a list of FormFields.
func ExtractFormData(doc *document.Document) ([]models.FormField, error) {
	catalog, err := document.GetCatalog(doc)
	if err != nil {
		return nil, err
	}

	acroFormObj, ok := catalog.Dict["AcroForm"]
	if !ok {
		return nil, nil // No forms in document
	}

	acroFormResolved, err := document.ResolveObject(doc, acroFormObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve AcroForm: %v", err)
	}

	acroFormDict, ok := acroFormResolved.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("AcroForm is not a dictionary")
	}

	// Check for XFA forms first. If present, extract XFA data as the base
	// layer, then overlay any AcroForm fields on top (AcroForm values take
	// precedence when both exist, per the PDF spec's fallback semantics).
	xfaData, _ := extractXFAData(doc, acroFormDict)

	fieldsObj, ok := acroFormDict["Fields"]
	if !ok {
		// No AcroForm fields — return XFA data if we have it.
		if len(xfaData) > 0 {
			var results []models.FormField
			for k, v := range xfaData {
				results = append(results, models.FormField{Name: k, Value: v})
			}
			return results, nil
		}
		return nil, nil
	}

	fieldsResolved, err := document.ResolveObject(doc, fieldsObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve Fields: %v", err)
	}

	fieldsArr, ok := fieldsResolved.(ast.PdfArray)
	if !ok {
		return nil, fmt.Errorf("fields is not an array")
	}

	// Start with XFA data as the base, then overlay AcroForm fields.
	formDataMap := make(map[string]string)
	maps.Copy(formDataMap, xfaData)

	// Track fields that have calculation scripts
	calcScripts := make(map[string]string)

	err = traverseFields(doc, fieldsArr, "", "", formDataMap, calcScripts)
	if err != nil {
		return nil, err
	}

	// If there are calculation scripts, evaluate them using goja
	if len(calcScripts) > 0 {
		vm := goja.New()

		// Inject mock this.getField("Name").value API.
		// goja Set/RunString calls for VM bootstrap are best-effort: these trivial
		// assignments and object property sets cannot fail in practice.
		_ = vm.Set("getField", func(call goja.FunctionCall) goja.Value { //nolint:errcheck
			if len(call.Arguments) > 0 {
				fieldName := call.Arguments[0].String()

				// Create a mock Field object
				fieldObj := vm.NewObject()
				_ = fieldObj.Set("value", "") //nolint:errcheck
				if val, exists := formDataMap[fieldName]; exists {
					_ = fieldObj.Set("value", val) //nolint:errcheck
				}

				// Provide a setter to update formData
				return fieldObj
			}
			return goja.Undefined()
		})

		// Map 'this.getField' -> global 'getField'
		_, _ = vm.RunString(`var this = { getField: getField };`) //nolint:errcheck

		// Inject Document-level scripts first (best-effort: malformed JS is non-fatal)
		docScripts, _ := ExtractDocumentScripts(doc) //nolint:errcheck
		for _, script := range docScripts {
			_, _ = vm.RunString(script) //nolint:errcheck
		}

		// Execute field calculations
		for fieldName, script := range calcScripts {
			// PDF Calculate scripts output to `event.value`
			// We mock the event object
			_, _ = vm.RunString(`var event = { value: "" };`) //nolint:errcheck

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

	var results []models.FormField
	for k, v := range formDataMap {
		results = append(results, models.FormField{
			Name:  k,
			Value: v,
		})
	}

	return results, nil
}

func traverseFields(doc *document.Document, fields ast.PdfArray, parentName string, inheritedFT string, formData map[string]string, calcScripts map[string]string) error {
	for _, fieldObj := range fields {
		fieldResolved, err := document.ResolveObject(doc, fieldObj)
		if err != nil {
			continue
		}

		fieldDict, ok := fieldResolved.(ast.PdfDict)
		if !ok {
			continue
		}

		// Extract partial name (/T)
		partialName := ""
		if tObj, ok := fieldDict["T"]; ok {
			tRes, _ := document.ResolveObject(doc, tObj)
			if tStr, ok := tRes.(ast.PdfString); ok {
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
			ftRes, _ := document.ResolveObject(doc, ftObj)
			if ftName, ok := ftRes.(ast.PdfName); ok {
				currentFT = string(ftName)
			}
		}

		// Extract Value (/V)
		if vObj, ok := fieldDict["V"]; ok {
			vRes, _ := document.ResolveObject(doc, vObj)

			if fullName != "" {
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
				aaRes, _ := document.ResolveObject(doc, aaObj)
				if aaDict, ok := aaRes.(ast.PdfDict); ok {
					// Check for Calculate (/C) action
					if cObj, ok := aaDict["C"]; ok {
						cRes, _ := document.ResolveObject(doc, cObj)
						if cActionDict, ok := cRes.(ast.PdfDict); ok {
							if sObj, ok := cActionDict["S"]; ok {
								sRes, _ := document.ResolveObject(doc, sObj)
								if sName, ok := sRes.(ast.PdfName); ok && sName == "JavaScript" {
									if jsObj, ok := cActionDict["JS"]; ok {
										jsRes, _ := document.ResolveObject(doc, jsObj)
										if jsStr, ok := jsRes.(ast.PdfString); ok {
											calcScripts[fullName] = document.DecodePdfString(jsStr)
										} else if jsStream, ok := jsRes.(ast.PdfStream); ok {
											data, err := filter.DecodeStream(jsStream)
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

		// Traverse children (/Kids)
		if kidsObj, ok := fieldDict["Kids"]; ok {
			kidsRes, _ := document.ResolveObject(doc, kidsObj)
			if kidsArr, ok := kidsRes.(ast.PdfArray); ok {
				if err := traverseFields(doc, kidsArr, fullName, currentFT, formData, calcScripts); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
