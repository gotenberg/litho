package compliance

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// walkerState captures the graphics state relevant for compliance checking.
type walkerState struct {
	fillCS   ast.PdfName
	strokeCS ast.PdfName
}

// contentWalker is a compliance-specific content stream walker that tracks
// graphics state to validate device color space usage, rendering intents,
// overprint modes, and other operator-level requirements. It replaces the
// scattered scanOpsForDeviceCS, checkAnnotationDeviceCS, etc. functions
// with a single consolidated walker.
type contentWalker struct {
	ctx       context.Context
	doc       *document.Document
	fillCS    ast.PdfName            // current fill color space
	strokeCS  ast.PdfName            // current stroke color space
	defaults  map[ast.PdfName]bool   // Default{RGB,CMYK,Gray} overrides
	iccN      int                    // OutputIntent ICC component count
	resources ast.PdfDict            // current resource dict
	depth     int                    // recursion depth for Do
	stack     []walkerState          // q/Q state stack
	errors    []error                // accumulated errors
	visited   map[ast.PdfObject]bool // cycle detection for Do recursion
}

// maxDoRecursion is the maximum depth for recursing into Form XObjects.
const maxDoRecursion = 10

// validPDFOperators is the complete set of operators defined in ISO 32000-1
// (Table 51) plus inline image operators (BI, ID, EI). Any operator not in
// this set is unknown and violates clause 6.2.2.
var validPDFOperators = map[string]bool{
	"b": true, "B": true, "b*": true, "B*": true,
	"BDC": true, "BI": true, "BMC": true, "BT": true, "BX": true,
	"c": true, "cm": true, "CS": true, "cs": true,
	"d": true, "d0": true, "d1": true, "Do": true, "DP": true,
	"EI": true, "EMC": true, "ET": true, "EX": true,
	"f": true, "f*": true, "F": true,
	"G": true, "g": true, "gs": true,
	"h": true,
	"i": true, "ID": true,
	"j": true, "J": true,
	"K": true, "k": true,
	"l": true,
	"m": true, "M": true, "MP": true,
	"n": true,
	"q": true, "Q": true,
	"re": true, "RG": true, "rg": true, "ri": true,
	"S": true, "s": true, "SC": true, "sc": true, "SCN": true, "scn": true, "sh": true,
	"T*": true, "Tc": true, "Td": true, "TD": true, "Tf": true,
	"Tj": true, "TJ": true, "TL": true, "Tm": true, "Tr": true, "Ts": true, "Tw": true, "Tz": true,
	"v": true,
	"w": true, "W": true, "W*": true,
	"y":  true,
	"\"": true, "'": true,
}

// newContentWalker creates a new content walker with the given parameters.
func newContentWalker(ctx context.Context, doc *document.Document, defaults map[ast.PdfName]bool, iccN int, resources ast.PdfDict) *contentWalker {
	return &contentWalker{
		ctx:       ctx,
		doc:       doc,
		defaults:  defaults,
		iccN:      iccN,
		resources: resources,
		visited:   make(map[ast.PdfObject]bool),
	}
}

// walk processes a sequence of content stream operations.
func (w *contentWalker) walk(ops []content.Operation) {
	for i, op := range ops {
		// Check context cancellation every 100 operators.
		if i%100 == 0 { //nolint:mnd // per AGENTS.md context check interval
			if w.ctx.Err() != nil {
				return
			}
		}
		// 6.2.2 test 1: reject operators not defined in ISO 32000.
		if !validPDFOperators[op.Operator] {
			w.errors = append(w.errors, fmt.Errorf("%w: %s",
				models.ErrUnknownOperator, op.Operator))
			continue
		}
		w.handleOp(op)
	}
}

// handleOp dispatches a single content stream operation.
func (w *contentWalker) handleOp(op content.Operation) {
	switch op.Operator {
	// Device gray operators.
	case "g", "G":
		w.handleDeviceCS(op.Operator, "DeviceGray")
	// Device RGB operators.
	case "rg", "RG":
		w.handleDeviceCS(op.Operator, "DeviceRGB")
	// Device CMYK operators.
	case "k", "K":
		w.handleDeviceCS(op.Operator, "DeviceCMYK")
	// Set color space operators.
	case "cs":
		w.handleCSOperator(op, false)
	case "CS":
		w.handleCSOperator(op, true)
	// Graphics state save/restore.
	case "q":
		w.stack = append(w.stack, walkerState{
			fillCS:   w.fillCS,
			strokeCS: w.strokeCS,
		})
	case "Q":
		if len(w.stack) > 0 {
			state := w.stack[len(w.stack)-1]
			w.stack = w.stack[:len(w.stack)-1]
			w.fillCS = state.fillCS
			w.strokeCS = state.strokeCS
		}
	// Extended graphics state.
	case "gs":
		w.handleGS(op)
	// Invoke XObject.
	case "Do":
		w.handleDo(op)
	// Rendering intent.
	case "ri":
		w.handleRI(op)
	}
}

// handleDeviceCS flags a device-dependent color space operator unless covered
// by a Default override or the OutputIntent ICC profile.
func (w *contentWalker) handleDeviceCS(operator string, deviceCS ast.PdfName) {
	if w.defaults[deviceCS] || iccCoversDeviceCS(w.iccN, deviceCS) {
		return
	}
	w.errors = append(w.errors, fmt.Errorf("%w: %s via %s operator",
		models.ErrMissingICCProfile, deviceCS, operator))
}

// handleCSOperator processes cs/CS operators that set the fill/stroke color space.
func (w *contentWalker) handleCSOperator(op content.Operation, isStroke bool) {
	if len(op.Operands) == 0 {
		return
	}
	csName, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return
	}

	// Track current color space.
	if isStroke {
		w.strokeCS = csName
	} else {
		w.fillCS = csName
	}

	// Direct device-dependent name.
	if deviceDependentNames[csName] && !w.defaults[csName] && !iccCoversDeviceCS(w.iccN, csName) {
		w.errors = append(w.errors, fmt.Errorf("%w: %s via %s operator",
			models.ErrMissingICCProfile, csName, op.Operator))
		return
	}

	// Resolve named color space from resources.
	if w.resources != nil {
		resolvedCS := resolveNamedColorSpace(w.ctx, w.doc, w.resources, csName)
		if resolvedCS != "" && deviceDependentNames[resolvedCS] &&
			!w.defaults[resolvedCS] && !iccCoversDeviceCS(w.iccN, resolvedCS) {
			w.errors = append(w.errors, fmt.Errorf("%w: %s (resolved from /%s) via %s operator",
				models.ErrMissingICCProfile, resolvedCS, csName, op.Operator))
		}
	}
}

// handleGS processes the gs operator to apply ExtGState changes.
func (w *contentWalker) handleGS(op content.Operation) {
	if len(op.Operands) == 0 || w.resources == nil {
		return
	}
	gsName, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return
	}

	// Look up the ExtGState in resources.
	gsObj, ok := w.resources["ExtGState"]
	if !ok {
		return
	}
	gsRes, _ := document.ResolveObject(w.ctx, w.doc, gsObj)
	gsDict, ok := gsRes.(ast.PdfDict)
	if !ok {
		return
	}
	stateObj, ok := gsDict[gsName]
	if !ok {
		return
	}
	stateRes, _ := document.ResolveObject(w.ctx, w.doc, stateObj)
	stateDict, ok := stateRes.(ast.PdfDict)
	if !ok {
		return
	}

	// Check rendering intent in ExtGState.
	if riObj, ok := stateDict["RI"]; ok {
		riRes, _ := document.ResolveObject(w.ctx, w.doc, riObj)
		if riName, ok := riRes.(ast.PdfName); ok {
			if !validRenderingIntents[riName] {
				w.errors = append(w.errors, fmt.Errorf("%w: /%s in ExtGState",
					models.ErrInvalidRenderingIntent, riName))
			}
		}
	}
}

// handleDo processes the Do operator, recursing into Form XObjects.
func (w *contentWalker) handleDo(op content.Operation) {
	if len(op.Operands) == 0 || w.resources == nil {
		return
	}
	xobjName, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return
	}

	// Look up the XObject in resources.
	xobjObj, ok := w.resources["XObject"]
	if !ok {
		return
	}
	xobjRes, _ := document.ResolveObject(w.ctx, w.doc, xobjObj)
	xobjDict, ok := xobjRes.(ast.PdfDict)
	if !ok {
		return
	}
	obj, ok := xobjDict[xobjName]
	if !ok {
		return
	}

	// Cycle detection.
	if w.visited[obj] {
		return
	}

	objRes, _ := document.ResolveObject(w.ctx, w.doc, obj)
	stream, ok := objRes.(ast.PdfStream)
	if !ok {
		return
	}

	stObj := stream.Dict["Subtype"]
	st, _ := stObj.(ast.PdfName)

	// Check Image XObject color space.
	if st == "Image" {
		w.checkImageCS(stream.Dict)
		return
	}

	if st != "Form" {
		return
	}

	if w.depth >= maxDoRecursion {
		return
	}

	// Parse the Form XObject's content stream.
	ops, err := content.ParsePageContent(w.ctx, w.doc, stream)
	if err != nil {
		return
	}

	// Determine resources for the Form XObject.
	formResources := w.resources
	if frObj, ok := stream.Dict["Resources"]; ok {
		frRes, _ := document.ResolveObject(w.ctx, w.doc, frObj)
		if frDict, ok := frRes.(ast.PdfDict); ok {
			formResources = frDict
		}
	}

	// Merge Form XObject's Group CS defaults.
	formDefaults := make(map[ast.PdfName]bool)
	maps.Copy(formDefaults, w.defaults)
	addTransparencyGroupDefaults(w.ctx, w.doc, stream.Dict, formDefaults)
	if formResources != nil {
		maps.Copy(formDefaults, collectDefaultColorSpaces(w.ctx, w.doc, formResources))
	}

	// Recurse with a child walker sharing error accumulation.
	w.visited[obj] = true
	child := &contentWalker{
		ctx:       w.ctx,
		doc:       w.doc,
		defaults:  formDefaults,
		iccN:      w.iccN,
		resources: formResources,
		depth:     w.depth + 1,
		visited:   w.visited,
	}
	child.walk(ops)
	w.errors = append(w.errors, child.errors...)

	// Check the Form XObject's named ColorSpace resources for device-dependent
	// color spaces not covered by defaults or ICC output intents.
	if formResources != nil {
		if errs := checkNamedColorSpaceResources(w.ctx, w.doc, formResources, formDefaults, w.iccN); len(errs) > 0 {
			w.errors = append(w.errors, errs...)
		}
	}
}

// checkImageCS validates the /ColorSpace of an Image XObject.
func (w *contentWalker) checkImageCS(dict ast.PdfDict) {
	csObj, ok := dict["ColorSpace"]
	if !ok {
		return
	}
	if isDeviceDependentCS(w.ctx, w.doc, csObj, w.defaults) {
		csRes, _ := document.ResolveObject(w.ctx, w.doc, csObj)
		csName := ast.PdfName("")
		switch cs := csRes.(type) {
		case ast.PdfName:
			csName = cs
		case ast.PdfArray:
			if len(cs) > 0 {
				if n, ok := cs[0].(ast.PdfName); ok {
					csName = n
				}
			}
		}
		if !iccCoversDeviceCS(w.iccN, csName) {
			w.errors = append(w.errors, fmt.Errorf("%w: %s in Image XObject",
				models.ErrMissingICCProfile, csName))
		}
	}
}

// handleRI processes the ri operator for rendering intent validation.
func (w *contentWalker) handleRI(op content.Operation) {
	if len(op.Operands) == 0 {
		return
	}
	riName, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return
	}
	if !validRenderingIntents[riName] {
		w.errors = append(w.errors, fmt.Errorf("%w: /%s via ri operator",
			models.ErrInvalidRenderingIntent, riName))
	}
}

// walkType3CharProcs walks Type3 font CharProc streams for device CS usage.
func (w *contentWalker) walkType3CharProcs() {
	if w.resources == nil {
		return
	}
	fontObj, ok := w.resources["Font"]
	if !ok {
		return
	}
	fontRes, _ := document.ResolveObject(w.ctx, w.doc, fontObj)
	fontDict, ok := fontRes.(ast.PdfDict)
	if !ok {
		return
	}
	for _, fObj := range fontDict {
		fRes, _ := document.ResolveObject(w.ctx, w.doc, fObj)
		fDict, ok := fRes.(ast.PdfDict)
		if !ok {
			continue
		}
		stObj := fDict["Subtype"]
		st, _ := stObj.(ast.PdfName)
		if st != "Type3" {
			continue
		}
		cpObj, ok := fDict["CharProcs"]
		if !ok {
			continue
		}
		cpRes, _ := document.ResolveObject(w.ctx, w.doc, cpObj)
		cpDict, ok := cpRes.(ast.PdfDict)
		if !ok {
			continue
		}
		for _, procObj := range cpDict {
			procRes, _ := document.ResolveObject(w.ctx, w.doc, procObj)
			procStream, ok := procRes.(ast.PdfStream)
			if !ok {
				continue
			}
			ops, err := content.ParsePageContent(w.ctx, w.doc, procStream)
			if err != nil {
				continue
			}
			child := &contentWalker{
				ctx:       w.ctx,
				doc:       w.doc,
				defaults:  w.defaults,
				iccN:      w.iccN,
				resources: w.resources,
				depth:     w.depth + 1,
				visited:   w.visited,
			}
			child.walk(ops)
			w.errors = append(w.errors, child.errors...)
		}
	}
}

// checkContentStreamCompliance is the consolidated entry point that replaces
// checkContentStreamDeviceCS, checkAnnotationDeviceCS, scanXObjectsForDeviceCS,
// scanFontsForDeviceCS, and checkInlineImageDeviceCS. It walks page content
// streams, Form XObjects, Type3 CharProcs, inline images, and annotation
// appearance streams using a stateful walker.
func checkContentStreamCompliance(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	catalogICCN := getOutputIntentICCComponents(ctx, doc, catalog)

	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		resources := getResources(ctx, doc, page)
		defaults := make(map[ast.PdfName]bool)
		if resources != nil {
			defaults = collectDefaultColorSpaces(ctx, doc, resources)
			addTransparencyGroupDefaults(ctx, doc, page, defaults)
		}

		iccN := catalogICCN
		if pageICCN := getOutputIntentICCComponents(ctx, doc, page); pageICCN > 0 {
			iccN = pageICCN
		}

		// Walk page content stream.
		ops, err := content.ParsePageContent(ctx, doc, page)
		if err == nil && len(ops) > 0 {
			w := newContentWalker(ctx, doc, defaults, iccN, resources)
			w.walk(ops)
			// Also walk Type3 CharProcs and Form XObjects recursively via Do.
			w.walkType3CharProcs()
			if len(w.errors) > 0 {
				return w.errors[:1] // Return first error for consistency with old behavior.
			}
		}

		// Walk inline images for device-dependent CS.
		if err := checkInlineImageDeviceCS(ctx, doc, page, defaults, iccN); err != nil {
			return []error{err}
		}

		// Walk annotation appearance streams.
		if errs := walkAnnotationAppearances(ctx, doc, page, defaults, iccN); len(errs) > 0 {
			return errs[:1]
		}

		// Check Shading resources for device-dependent color spaces.
		if resources != nil {
			if errs := checkShadingResourceCS(ctx, doc, resources, defaults, iccN); len(errs) > 0 {
				return errs[:1]
			}
		}

		// Check Pattern resources for device-dependent color spaces.
		if resources != nil {
			if errs := checkPatternResourceCS(ctx, doc, resources, defaults, iccN); len(errs) > 0 {
				return errs[:1]
			}
		}

		// Check all XObject resources (including unreferenced) for device CS.
		if resources != nil {
			if errs := checkAllXObjectCS(ctx, doc, resources, defaults, iccN); len(errs) > 0 {
				return errs[:1]
			}
		}
	}
	return nil
}

// checkShadingResourceCS scans Shading resources for device-dependent CS.
func checkShadingResourceCS(ctx context.Context, doc *document.Document, resources ast.PdfDict, defaults map[ast.PdfName]bool, iccN int) []error {
	shObj, ok := resources["Shading"]
	if !ok {
		return nil
	}
	shRes, _ := document.ResolveObject(ctx, doc, shObj)
	shDict, ok := shRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	for _, obj := range shDict {
		objRes, _ := document.ResolveObject(ctx, doc, obj)
		var shadingDict ast.PdfDict
		switch v := objRes.(type) {
		case ast.PdfDict:
			shadingDict = v
		case ast.PdfStream:
			shadingDict = v.Dict
		default:
			continue
		}
		csObj, ok := shadingDict["ColorSpace"]
		if !ok {
			continue
		}
		if isDeviceDependentCS(ctx, doc, csObj, defaults) {
			csRes, _ := document.ResolveObject(ctx, doc, csObj)
			csName := ast.PdfName("")
			if n, ok := csRes.(ast.PdfName); ok {
				csName = n
			}
			if !iccCoversDeviceCS(iccN, csName) {
				return []error{fmt.Errorf("%w: %s in Shading resource",
					models.ErrMissingICCProfile, csName)}
			}
		}
	}
	return nil
}

// checkPatternResourceCS scans tiling Pattern resources for device-dependent
// color spaces by walking their content streams.
func checkPatternResourceCS(ctx context.Context, doc *document.Document, resources ast.PdfDict, defaults map[ast.PdfName]bool, iccN int) []error {
	patObj, ok := resources["Pattern"]
	if !ok {
		return nil
	}
	patRes, _ := document.ResolveObject(ctx, doc, patObj)
	patDict, ok := patRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	for _, obj := range patDict {
		objRes, _ := document.ResolveObject(ctx, doc, obj)
		stream, ok := objRes.(ast.PdfStream)
		if !ok {
			continue
		}
		// Only tiling patterns (PatternType 1) have content streams.
		ptObj := stream.Dict["PatternType"]
		if ptInt, ok := ptObj.(ast.PdfInt); ok && ptInt != 1 {
			continue
		}

		// Walk the pattern's content stream.
		ops, err := content.ParsePageContent(ctx, doc, stream)
		if err != nil {
			continue
		}

		// Use the pattern's own resources if available, otherwise page resources.
		patResources := resources
		if prObj, ok := stream.Dict["Resources"]; ok {
			prRes, _ := document.ResolveObject(ctx, doc, prObj)
			if prDict, ok := prRes.(ast.PdfDict); ok {
				patResources = prDict
			}
		}

		// Merge pattern resource defaults with page defaults.
		patDefaults := make(map[ast.PdfName]bool)
		maps.Copy(patDefaults, defaults)
		if patResources != nil {
			maps.Copy(patDefaults, collectDefaultColorSpaces(ctx, doc, patResources))
		}

		w := newContentWalker(ctx, doc, patDefaults, iccN, patResources)
		w.walk(ops)
		if len(w.errors) > 0 {
			return w.errors[:1]
		}
	}
	return nil
}

// checkNamedColorSpaceResources scans the /Resources/ColorSpace dictionary
// for named color spaces that resolve to device-dependent spaces (DeviceRGB,
// DeviceCMYK, DeviceGray) not covered by defaults or ICC output intents.
// This satisfies VeraPDF clause 6.2.4.3 which requires that ALL
// device-dependent color spaces in the document are flagged, even those not
// explicitly invoked via content stream operators.
func checkNamedColorSpaceResources(ctx context.Context, doc *document.Document, resources ast.PdfDict, defaults map[ast.PdfName]bool, iccN int) []error {
	csObj, ok := resources["ColorSpace"]
	if !ok {
		return nil
	}
	csRes, _ := document.ResolveObject(ctx, doc, csObj)
	csDict, ok := csRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	for _, obj := range csDict {
		if isDeviceDependentCS(ctx, doc, obj, defaults) {
			csResolved, _ := document.ResolveObject(ctx, doc, obj)
			csName := ast.PdfName("")
			switch cs := csResolved.(type) {
			case ast.PdfName:
				csName = cs
			case ast.PdfArray:
				if len(cs) > 0 {
					if n, ok := cs[0].(ast.PdfName); ok {
						csName = n
					}
				}
			}
			if !iccCoversDeviceCS(iccN, csName) {
				return []error{fmt.Errorf("%w: %s in named ColorSpace resource",
					models.ErrMissingICCProfile, csName)}
			}
		}
	}
	return nil
}

// checkAllXObjectCS scans all XObject resources (including those not invoked
// via Do in the content stream) for device-dependent color spaces in Image
// XObjects.
func checkAllXObjectCS(ctx context.Context, doc *document.Document, resources ast.PdfDict, defaults map[ast.PdfName]bool, iccN int) []error {
	xobjObj, ok := resources["XObject"]
	if !ok {
		return nil
	}
	xobjRes, _ := document.ResolveObject(ctx, doc, xobjObj)
	xobjDict, ok := xobjRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	for _, obj := range xobjDict {
		objRes, _ := document.ResolveObject(ctx, doc, obj)
		stream, ok := objRes.(ast.PdfStream)
		if !ok {
			continue
		}
		stObj := stream.Dict["Subtype"]
		st, _ := stObj.(ast.PdfName)
		if st != "Image" {
			continue
		}
		csObj, ok := stream.Dict["ColorSpace"]
		if !ok {
			continue
		}
		if isDeviceDependentCS(ctx, doc, csObj, defaults) {
			csRes, _ := document.ResolveObject(ctx, doc, csObj)
			csName := ast.PdfName("")
			if n, ok := csRes.(ast.PdfName); ok {
				csName = n
			}
			if !iccCoversDeviceCS(iccN, csName) {
				return []error{fmt.Errorf("%w: %s in Image XObject resource",
					models.ErrMissingICCProfile, csName)}
			}
		}
	}
	return nil
}

// walkAnnotationAppearances walks annotation /AP /N streams on a page.
func walkAnnotationAppearances(ctx context.Context, doc *document.Document, page ast.PdfDict, defaults map[ast.PdfName]bool, iccN int) []error {
	annotsObj, ok := page["Annots"]
	if !ok {
		return nil
	}
	annotsRes, _ := document.ResolveObject(ctx, doc, annotsObj)
	annotsArr, ok := annotsRes.(ast.PdfArray)
	if !ok {
		return nil
	}
	for _, annotObj := range annotsArr {
		annotRes, _ := document.ResolveObject(ctx, doc, annotObj)
		annotDict, ok := annotRes.(ast.PdfDict)
		if !ok {
			continue
		}
		apObj, ok := annotDict["AP"]
		if !ok {
			continue
		}
		apRes, _ := document.ResolveObject(ctx, doc, apObj)
		apDict, ok := apRes.(ast.PdfDict)
		if !ok {
			continue
		}
		nObj, ok := apDict["N"]
		if !ok {
			continue
		}

		// /N can be a stream (single appearance) or a dict of named
		// appearance states (e.g., /True, /False for checkboxes).
		nRes, _ := document.ResolveObject(ctx, doc, nObj)
		var appearanceStreams []ast.PdfObject
		switch n := nRes.(type) {
		case ast.PdfStream:
			appearanceStreams = append(appearanceStreams, nObj)
		case ast.PdfDict:
			// Subdictionary of named appearances — walk each.
			for _, subObj := range n {
				appearanceStreams = append(appearanceStreams, subObj)
			}
		}

		for _, apStreamObj := range appearanceStreams {
			ops, err := content.ParsePageContent(ctx, doc, apStreamObj)
			if err != nil {
				continue
			}

			// Use annotation's own resources if available.
			annotDefaults := make(map[ast.PdfName]bool)
			maps.Copy(annotDefaults, defaults)
			annotResourceDict := ast.PdfDict(nil)
			apStreamRes, _ := document.ResolveObject(ctx, doc, apStreamObj)
			if apStream, ok := apStreamRes.(ast.PdfStream); ok {
				if arObj, ok := apStream.Dict["Resources"]; ok {
					arRes, _ := document.ResolveObject(ctx, doc, arObj)
					if arDict, ok := arRes.(ast.PdfDict); ok {
						annotResourceDict = arDict
						maps.Copy(annotDefaults, collectDefaultColorSpaces(ctx, doc, arDict))
					}
				}
				addTransparencyGroupDefaults(ctx, doc, apStream.Dict, annotDefaults)
			}
			if annotResourceDict == nil {
				annotResourceDict = getResources(ctx, doc, page)
			}

			w := newContentWalker(ctx, doc, annotDefaults, iccN, annotResourceDict)
			w.walk(ops)
			if len(w.errors) > 0 {
				return w.errors[:1]
			}
		}
	}
	return nil
}
