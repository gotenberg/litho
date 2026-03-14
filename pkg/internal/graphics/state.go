package graphics

import (
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TextState represents the text-specific graphics state.
type TextState struct {
	Tc    float64    // Character spacing
	Tw    float64    // Word spacing
	Tz    float64    // Horizontal scaling (default 100)
	Tl    float64    // Leading
	Tf    string     // Font name
	Tfs   float64    // Font size
	Tmode int        // Text rendering mode (0=Fill, 1=Stroke, 3=Invisible, etc)
	Tm    [6]float64 // Text matrix
	Tlm   [6]float64 // Text line matrix
}

// Point represents a 2D coordinate
type Point struct {
	X, Y float64
}

// Path represents a sequence of drawing operations
type Path []Point

// BoundingBox represents a rectangular region
type BoundingBox struct {
	MinX, MinY, MaxX, MaxY float64
}

// State represents the current graphics state.
type State struct {
	CTM              [6]float64 // Current Transformation Matrix
	LineWidth        float64
	StrokeColor      []float64
	StrokeColorSpace string // "DeviceGray", "DeviceRGB", "DeviceCMYK", or a named space
	FillColor        []float64
	FillColorSpace   string // "DeviceGray", "DeviceRGB", "DeviceCMYK", or a named space
	Text             TextState
	SkipExtraction   bool        // Used for Optional Content Groups (Hidden Layers)
	ClippingPath     BoundingBox // Simplified clipping to a bounding box for performance
}

// MarkedContentState tracks the nested state of marked content blocks.
type MarkedContentState struct {
	ActualText string
	HasText    bool
	Tag        string
	MCID       int
	IsHiddenOC bool
	IsArtifact bool // True when the block is tagged /Artifact
}

// Machine is the graphics state machine that processes content stream operations.
type Machine struct {
	Stack             []State
	MarkedContent     []MarkedContentState
	IgnoreTextOps     bool
	StripArtifacts    bool // When true, text inside /Artifact marked content is discarded
	PendingActualText string
	OCConfigOn        map[int]bool
	OCConfigOff       map[int]bool
	ActiveOCs         map[string]bool
	CurrentPath       BoundingBox // Tracks the current active path bounds
}

// NewMachine initializes a new graphics state machine with the default state.
// CropBox can be passed in to set the initial clipping region.
func NewMachine(cropBox [4]float64) *Machine {
	// If cropBox is 0,0,0,0, set it to a very large number
	if cropBox[2] == 0 && cropBox[3] == 0 {
		cropBox = [4]float64{-10000, -10000, 10000, 10000}
	} else {
		// Ensure min/max are correct
		if cropBox[0] > cropBox[2] {
			cropBox[0], cropBox[2] = cropBox[2], cropBox[0]
		}
		if cropBox[1] > cropBox[3] {
			cropBox[1], cropBox[3] = cropBox[3], cropBox[1]
		}
	}

	defaultState := State{
		CTM: [6]float64{1, 0, 0, 1, 0, 0},
		Text: TextState{
			Tz:    100.0,
			Tm:    [6]float64{1, 0, 0, 1, 0, 0},
			Tlm:   [6]float64{1, 0, 0, 1, 0, 0},
			Tmode: 0, // Fill text by default
		},
		LineWidth:        1.0,
		FillColor:        []float64{0}, // DeviceGray 0 = black (PDF spec default)
		FillColorSpace:   "DeviceGray", // PDF spec default color space
		StrokeColor:      []float64{0}, // DeviceGray 0 = black
		StrokeColorSpace: "DeviceGray",
		ClippingPath:     BoundingBox{MinX: cropBox[0], MinY: cropBox[1], MaxX: cropBox[2], MaxY: cropBox[3]},
	}
	return &Machine{
		Stack:         []State{defaultState},
		MarkedContent: make([]MarkedContentState, 0),
		OCConfigOn:    make(map[int]bool),
		OCConfigOff:   make(map[int]bool),
		ActiveOCs:     make(map[string]bool),
		CurrentPath:   BoundingBox{MinX: 100000, MinY: 100000, MaxX: -100000, MaxY: -100000},
	}
}

// WithOCConfig allows injecting the ON/OFF arrays from the document.
func (m *Machine) WithOCConfig(on map[int]bool, off map[int]bool) *Machine {
	m.OCConfigOn = on
	m.OCConfigOff = off
	return m
}

// Current returns a pointer to the current graphics state.
func (m *Machine) Current() *State {
	return &m.Stack[len(m.Stack)-1]
}

// getFloat is a helper to safely extract a float64 from a PdfObject.
func getFloat(obj ast.PdfObject) float64 {
	switch v := obj.(type) {
	case ast.PdfReal:
		return float64(v)
	case ast.PdfInt:
		return float64(v)
	}
	return 0
}

// Execute processes a sequence of content operations and updates the graphics state.
func (m *Machine) Execute(ops []content.Operation) error {
	for _, op := range ops {
		state := m.Current()

		switch op.Operator {
		case "q": // Save graphics state
			newState := *state
			// Deep copy slices
			newState.StrokeColor = make([]float64, len(state.StrokeColor))
			copy(newState.StrokeColor, state.StrokeColor)
			newState.FillColor = make([]float64, len(state.FillColor))
			copy(newState.FillColor, state.FillColor)

			m.Stack = append(m.Stack, newState)

		case "Q": // Restore graphics state
			if len(m.Stack) > 1 {
				m.Stack = m.Stack[:len(m.Stack)-1]
			}

		case "cm": // Concatenate matrix
			if len(op.Operands) == 6 {
				a := getFloat(op.Operands[0])
				b := getFloat(op.Operands[1])
				c := getFloat(op.Operands[2])
				d := getFloat(op.Operands[3])
				e := getFloat(op.Operands[4])
				f := getFloat(op.Operands[5])

				// New CTM = provided matrix x Current CTM
				ctm := state.CTM
				state.CTM[0] = a*ctm[0] + b*ctm[2]
				state.CTM[1] = a*ctm[1] + b*ctm[3]
				state.CTM[2] = c*ctm[0] + d*ctm[2]
				state.CTM[3] = c*ctm[1] + d*ctm[3]
				state.CTM[4] = e*ctm[0] + f*ctm[2] + ctm[4]
				state.CTM[5] = e*ctm[1] + f*ctm[3] + ctm[5]
			}

		case "BT": // Begin text object
			state.Text.Tm = [6]float64{1, 0, 0, 1, 0, 0}
			state.Text.Tlm = [6]float64{1, 0, 0, 1, 0, 0}

		case "ET": // End text object
			state.Text.Tm = [6]float64{1, 0, 0, 1, 0, 0}
			state.Text.Tlm = [6]float64{1, 0, 0, 1, 0, 0}

		case "Tc": // Set character spacing
			if len(op.Operands) == 1 {
				state.Text.Tc = getFloat(op.Operands[0])
			}

		case "Tw": // Set word spacing
			if len(op.Operands) == 1 {
				state.Text.Tw = getFloat(op.Operands[0])
			}

		case "Tz": // Set horizontal scaling
			if len(op.Operands) == 1 {
				state.Text.Tz = getFloat(op.Operands[0])
			}

		case "TL": // Set leading
			if len(op.Operands) == 1 {
				state.Text.Tl = getFloat(op.Operands[0])
			}

		case "Tf": // Set text font and size
			if len(op.Operands) == 2 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					state.Text.Tf = string(name)
				}
				state.Text.Tfs = getFloat(op.Operands[1])
			}

		case "Tm": // Set text matrix
			if len(op.Operands) == 6 {
				state.Text.Tm[0] = getFloat(op.Operands[0])
				state.Text.Tm[1] = getFloat(op.Operands[1])
				state.Text.Tm[2] = getFloat(op.Operands[2])
				state.Text.Tm[3] = getFloat(op.Operands[3])
				state.Text.Tm[4] = getFloat(op.Operands[4])
				state.Text.Tm[5] = getFloat(op.Operands[5])
				state.Text.Tlm = state.Text.Tm
			}

		case "Td": // Move text position
			if len(op.Operands) == 2 {
				tx := getFloat(op.Operands[0])
				ty := getFloat(op.Operands[1])
				tlm := state.Text.Tlm
				state.Text.Tlm[4] = tx*tlm[0] + ty*tlm[2] + tlm[4]
				state.Text.Tlm[5] = tx*tlm[1] + ty*tlm[3] + tlm[5]
				state.Text.Tm = state.Text.Tlm
			}

		case "TD": // Move text position and set leading
			if len(op.Operands) == 2 {
				tx := getFloat(op.Operands[0])
				ty := getFloat(op.Operands[1])
				state.Text.Tl = -ty
				tlm := state.Text.Tlm
				state.Text.Tlm[4] = tx*tlm[0] + ty*tlm[2] + tlm[4]
				state.Text.Tlm[5] = tx*tlm[1] + ty*tlm[3] + tlm[5]
				state.Text.Tm = state.Text.Tlm
			}

		case "T*": // Move to next line
			tlm := state.Text.Tlm
			ty := -state.Text.Tl
			state.Text.Tlm[4] = ty*tlm[2] + tlm[4]
			state.Text.Tlm[5] = ty*tlm[3] + tlm[5]
			state.Text.Tm = state.Text.Tlm

		case "'": // Move to next line and show text
			tlm := state.Text.Tlm
			ty := -state.Text.Tl
			state.Text.Tlm[4] = ty*tlm[2] + tlm[4]
			state.Text.Tlm[5] = ty*tlm[3] + tlm[5]
			state.Text.Tm = state.Text.Tlm
			// Note: The text rendering part is expected to be handled externally since ' triggers a show text event.

		case "\"": // Set word/char spacing, move to next line, and show text
			if len(op.Operands) == 3 {
				state.Text.Tw = getFloat(op.Operands[0])
				state.Text.Tc = getFloat(op.Operands[1])
				tlm := state.Text.Tlm
				ty := -state.Text.Tl
				state.Text.Tlm[4] = ty*tlm[2] + tlm[4]
				state.Text.Tlm[5] = ty*tlm[3] + tlm[5]
				state.Text.Tm = state.Text.Tlm
			}

		case "Tr": // Set text rendering mode
			if len(op.Operands) == 1 {
				state.Text.Tmode = int(getFloat(op.Operands[0]))
			}

		case "rg": // Set RGB fill color
			if len(op.Operands) == 3 {
				state.FillColor = []float64{
					getFloat(op.Operands[0]),
					getFloat(op.Operands[1]),
					getFloat(op.Operands[2]),
				}
				state.FillColorSpace = "DeviceRGB"
			}

		case "RG": // Set RGB stroke color
			if len(op.Operands) == 3 {
				state.StrokeColor = []float64{
					getFloat(op.Operands[0]),
					getFloat(op.Operands[1]),
					getFloat(op.Operands[2]),
				}
				state.StrokeColorSpace = "DeviceRGB"
			}

		case "g": // Set Gray fill color
			if len(op.Operands) == 1 {
				state.FillColor = []float64{getFloat(op.Operands[0])}
				state.FillColorSpace = "DeviceGray"
			}

		case "G": // Set Gray stroke color
			if len(op.Operands) == 1 {
				state.StrokeColor = []float64{getFloat(op.Operands[0])}
				state.StrokeColorSpace = "DeviceGray"
			}

		case "k": // Set CMYK fill color
			if len(op.Operands) == 4 {
				state.FillColor = []float64{
					getFloat(op.Operands[0]),
					getFloat(op.Operands[1]),
					getFloat(op.Operands[2]),
					getFloat(op.Operands[3]),
				}
				state.FillColorSpace = "DeviceCMYK"
			}

		case "K": // Set CMYK stroke color
			if len(op.Operands) == 4 {
				state.StrokeColor = []float64{
					getFloat(op.Operands[0]),
					getFloat(op.Operands[1]),
					getFloat(op.Operands[2]),
					getFloat(op.Operands[3]),
				}
				state.StrokeColorSpace = "DeviceCMYK"
			}

		case "cs": // Set fill color space
			if len(op.Operands) == 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					state.FillColorSpace = string(name)
				}
			}

		case "CS": // Set stroke color space
			if len(op.Operands) == 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					state.StrokeColorSpace = string(name)
				}
			}

		case "sc", "scn": // Set fill color in current color space
			var vals []float64
			for _, operand := range op.Operands {
				switch operand.(type) {
				case ast.PdfReal, ast.PdfInt:
					vals = append(vals, getFloat(operand))
				}
			}
			if len(vals) > 0 {
				state.FillColor = vals
			}

		case "SC", "SCN": // Set stroke color in current color space
			var vals []float64
			for _, operand := range op.Operands {
				switch operand.(type) {
				case ast.PdfReal, ast.PdfInt:
					vals = append(vals, getFloat(operand))
				}
			}
			if len(vals) > 0 {
				state.StrokeColor = vals
			}

		case "m": // Move to
			if len(op.Operands) == 2 {
				x := getFloat(op.Operands[0])
				y := getFloat(op.Operands[1])
				// Transform point to absolute page coordinates
				px := x*state.CTM[0] + y*state.CTM[2] + state.CTM[4]
				py := x*state.CTM[1] + y*state.CTM[3] + state.CTM[5]

				if px < m.CurrentPath.MinX {
					m.CurrentPath.MinX = px
				}
				if px > m.CurrentPath.MaxX {
					m.CurrentPath.MaxX = px
				}
				if py < m.CurrentPath.MinY {
					m.CurrentPath.MinY = py
				}
				if py > m.CurrentPath.MaxY {
					m.CurrentPath.MaxY = py
				}
			}

		case "l": // Line to
			if len(op.Operands) == 2 {
				x := getFloat(op.Operands[0])
				y := getFloat(op.Operands[1])
				px := x*state.CTM[0] + y*state.CTM[2] + state.CTM[4]
				py := x*state.CTM[1] + y*state.CTM[3] + state.CTM[5]

				if px < m.CurrentPath.MinX {
					m.CurrentPath.MinX = px
				}
				if px > m.CurrentPath.MaxX {
					m.CurrentPath.MaxX = px
				}
				if py < m.CurrentPath.MinY {
					m.CurrentPath.MinY = py
				}
				if py > m.CurrentPath.MaxY {
					m.CurrentPath.MaxY = py
				}
			}

		case "re": // Rectangle
			if len(op.Operands) == 4 {
				x := getFloat(op.Operands[0])
				y := getFloat(op.Operands[1])
				w := getFloat(op.Operands[2])
				h := getFloat(op.Operands[3])

				// Calculate 4 corners in user space
				pts := []Point{
					{x, y}, {x + w, y}, {x, y + h}, {x + w, y + h},
				}

				for _, p := range pts {
					px := p.X*state.CTM[0] + p.Y*state.CTM[2] + state.CTM[4]
					py := p.X*state.CTM[1] + p.Y*state.CTM[3] + state.CTM[5]
					if px < m.CurrentPath.MinX {
						m.CurrentPath.MinX = px
					}
					if px > m.CurrentPath.MaxX {
						m.CurrentPath.MaxX = px
					}
					if py < m.CurrentPath.MinY {
						m.CurrentPath.MinY = py
					}
					if py > m.CurrentPath.MaxY {
						m.CurrentPath.MaxY = py
					}
				}
			}

		case "c", "v", "y": // Bezier curves
			// simplistic bounding box updates using the control points
			for i := 0; i < len(op.Operands); i += 2 {
				x := getFloat(op.Operands[i])
				y := getFloat(op.Operands[i+1])
				px := x*state.CTM[0] + y*state.CTM[2] + state.CTM[4]
				py := x*state.CTM[1] + y*state.CTM[3] + state.CTM[5]
				if px < m.CurrentPath.MinX {
					m.CurrentPath.MinX = px
				}
				if px > m.CurrentPath.MaxX {
					m.CurrentPath.MaxX = px
				}
				if py < m.CurrentPath.MinY {
					m.CurrentPath.MinY = py
				}
				if py > m.CurrentPath.MaxY {
					m.CurrentPath.MaxY = py
				}
			}

		case "h": // Close path
			// No bounding box change

		case "n": // End path without filling or stroking
			m.CurrentPath = BoundingBox{MinX: 100000, MinY: 100000, MaxX: -100000, MaxY: -100000}

		case "W", "W*": // Set clipping path
			// Intersect current clipping path with the new path
			if m.CurrentPath.MinX > state.ClippingPath.MinX {
				state.ClippingPath.MinX = m.CurrentPath.MinX
			}
			if m.CurrentPath.MinY > state.ClippingPath.MinY {
				state.ClippingPath.MinY = m.CurrentPath.MinY
			}
			if m.CurrentPath.MaxX < state.ClippingPath.MaxX {
				state.ClippingPath.MaxX = m.CurrentPath.MaxX
			}
			if m.CurrentPath.MaxY < state.ClippingPath.MaxY {
				state.ClippingPath.MaxY = m.CurrentPath.MaxY
			}

			// PDF Spec: W/W* doesn't end the path, it just marks it for clipping.
			// The path is usually ended by a subsequent n, S, s, f, F operator.

		case "S", "s", "f", "F", "f*", "B", "B*", "b", "b*": // Paint operations (stroke/fill)
			// Reset current path
			m.CurrentPath = BoundingBox{MinX: 100000, MinY: 100000, MaxX: -100000, MaxY: -100000}

		case "BDC": // Begin Dictionary-Driven Marked Content
			mcState := MarkedContentState{MCID: -1}
			if len(op.Operands) >= 1 {
				if tag, ok := op.Operands[0].(ast.PdfName); ok {
					mcState.Tag = string(tag)
				}
			}

			// Detect /Artifact tag — marks headers, footers, page numbers, watermarks.
			if mcState.Tag == "Artifact" {
				mcState.IsArtifact = true
				if m.StripArtifacts {
					state.SkipExtraction = true
				}
			}

			if len(op.Operands) >= 2 {
				// Handle Optional Content (/OC)
				if mcState.Tag == "OC" {
					if ocName, ok := op.Operands[1].(ast.PdfName); ok {
						// Look up the boolean flag passed in by the extractor
						if visible, exists := m.ActiveOCs[string(ocName)]; exists {
							if !visible {
								mcState.IsHiddenOC = true
								state.SkipExtraction = true
							}
						}
					} else if ocDict, ok := op.Operands[1].(ast.PdfDict); ok {
						// Rarely it's an inline dictionary
						_ = ocDict
					}
				}

				if props, ok := op.Operands[1].(ast.PdfDict); ok {
					if mcidObj, ok := props["MCID"]; ok {
						if mcidInt, isInt := mcidObj.(ast.PdfInt); isInt {
							mcState.MCID = int(mcidInt)
						}
					}
					// Check for ActualText or Alt
					if actualText, ok := props["ActualText"]; ok {
						if str, isStr := actualText.(ast.PdfString); isStr {
							mcState.ActualText = string(str)
							mcState.HasText = true
						}
					} else if altText, ok := props["Alt"]; ok {
						if str, isStr := altText.(ast.PdfString); isStr {
							mcState.ActualText = string(str)
							mcState.HasText = true
						}
					}
				}
			}
			m.MarkedContent = append(m.MarkedContent, mcState)
			if mcState.HasText {
				m.IgnoreTextOps = true
				// We store it so the caller (model.ExtractText) can pull it and render it
				m.PendingActualText = mcState.ActualText
			}

		case "BMC": // Begin Marked Content
			mcState := MarkedContentState{MCID: -1}
			if len(op.Operands) >= 1 {
				if tag, ok := op.Operands[0].(ast.PdfName); ok {
					mcState.Tag = string(tag)
				}
			}
			if mcState.Tag == "Artifact" {
				mcState.IsArtifact = true
				if m.StripArtifacts {
					state.SkipExtraction = true
				}
			}
			m.MarkedContent = append(m.MarkedContent, mcState)

		case "EMC": // End Marked Content
			if len(m.MarkedContent) > 0 {
				m.MarkedContent = m.MarkedContent[:len(m.MarkedContent)-1]
			}

			// Re-evaluate IgnoreTextOps and SkipExtraction based on remaining stack
			m.IgnoreTextOps = false
			state.SkipExtraction = false
			for _, mc := range m.MarkedContent {
				if mc.HasText {
					m.IgnoreTextOps = true
				}
				if mc.IsHiddenOC {
					state.SkipExtraction = true
				}
				if mc.IsArtifact && m.StripArtifacts {
					state.SkipExtraction = true
				}
			}

		case "DP": // Designate Marked Content Point
			// No state change needed for point markers
		}
	}
	return nil
}
