package ooxml

// vmlPresets maps the shape type numbers of VML (ECMA-376 Part 4, the
// o:spt attribute of v:shape and v:shapetype, the binary format's MSO_SPT
// values) onto the DrawingML preset of the same shape, as LibreOffice
// maps them: the type's name in svx's EnhancedCustomShapeTypeNames, then
// GetOOXMLPresetGeometry of filter/source/msfilter/util.cxx. The thick
// arrow (14), the balloon (17), the seal (18), the picture frame (75) and
// the host control (201) have no preset, and the WordArt types (24 to 31,
// 136 to 175), which LibreOffice maps onto the text warps of 20.1.10.76
// ST_TextShapeType, are not shapes.
var vmlPresets = map[int]string{ //nolint:gochecknoglobals // a read-only table, LibreOffice's MSO_SPT to OOXML mapping.
	1:   "rect",                       // rectangle
	2:   "roundRect",                  // round-rectangle
	3:   "ellipse",                    // ellipse
	4:   "diamond",                    // diamond
	5:   "triangle",                   // isosceles-triangle
	6:   "rtTriangle",                 // right-triangle
	7:   "parallelogram",              // parallelogram
	8:   "trapezoid",                  // trapezoid
	9:   "hexagon",                    // hexagon
	10:  "octagon",                    // octagon
	11:  "plus",                       // cross
	12:  "star5",                      // star5
	13:  "rightArrow",                 // right-arrow
	15:  "homePlate",                  // pentagon-right
	16:  "cube",                       // cube
	19:  "arc",                        // mso-spt19
	20:  "line",                       // mso-spt20
	21:  "plaque",                     // mso-spt21
	22:  "can",                        // can
	23:  "donut",                      // ring
	32:  "straightConnector1",         // mso-spt32
	33:  "bentConnector2",             // mso-spt33
	34:  "bentConnector3",             // mso-spt34
	35:  "bentConnector4",             // mso-spt35
	36:  "bentConnector5",             // mso-spt36
	37:  "curvedConnector2",           // mso-spt37
	38:  "curvedConnector3",           // mso-spt38
	39:  "curvedConnector4",           // mso-spt39
	40:  "curvedConnector5",           // mso-spt40
	41:  "callout1",                   // mso-spt41
	42:  "callout2",                   // mso-spt42
	43:  "callout3",                   // mso-spt43
	44:  "accentCallout1",             // mso-spt44
	45:  "accentCallout2",             // mso-spt45
	46:  "accentCallout3",             // mso-spt46
	47:  "borderCallout1",             // line-callout-1
	48:  "borderCallout2",             // line-callout-2
	49:  "borderCallout3",             // mso-spt49
	50:  "accentBorderCallout1",       // mso-spt50
	51:  "accentBorderCallout2",       // mso-spt51
	52:  "accentBorderCallout3",       // mso-spt52
	53:  "ribbon",                     // mso-spt53
	54:  "ribbon2",                    // mso-spt54
	55:  "chevron",                    // chevron
	56:  "pentagon",                   // pentagon
	57:  "noSmoking",                  // forbidden
	58:  "star8",                      // star8
	59:  "star16",                     // mso-spt59
	60:  "star32",                     // mso-spt60
	61:  "wedgeRectCallout",           // rectangular-callout
	62:  "wedgeRoundRectCallout",      // round-rectangular-callout
	63:  "wedgeEllipseCallout",        // round-callout
	64:  "wave",                       // mso-spt64
	65:  "foldedCorner",               // paper
	66:  "leftArrow",                  // left-arrow
	67:  "downArrow",                  // down-arrow
	68:  "upArrow",                    // up-arrow
	69:  "leftRightArrow",             // left-right-arrow
	70:  "upDownArrow",                // up-down-arrow
	71:  "irregularSeal1",             // mso-spt71
	72:  "irregularSeal2",             // bang
	73:  "lightningBolt",              // lightning
	74:  "heart",                      // heart
	76:  "quadArrow",                  // quad-arrow
	77:  "leftArrowCallout",           // left-arrow-callout
	78:  "rightArrowCallout",          // right-arrow-callout
	79:  "upArrowCallout",             // up-arrow-callout
	80:  "downArrowCallout",           // down-arrow-callout
	81:  "leftRightArrowCallout",      // left-right-arrow-callout
	82:  "upDownArrowCallout",         // up-down-arrow-callout
	83:  "quadArrowCallout",           // quad-arrow-callout
	84:  "bevel",                      // quad-bevel
	85:  "leftBracket",                // left-bracket
	86:  "rightBracket",               // right-bracket
	87:  "leftBrace",                  // left-brace
	88:  "rightBrace",                 // right-brace
	89:  "leftUpArrow",                // mso-spt89
	90:  "bentUpArrow",                // mso-spt90
	91:  "bentArrow",                  // mso-spt91
	92:  "star24",                     // star24
	93:  "stripedRightArrow",          // striped-right-arrow
	94:  "notchedRightArrow",          // notched-right-arrow
	95:  "blockArc",                   // block-arc
	96:  "smileyFace",                 // smiley
	97:  "verticalScroll",             // vertical-scroll
	98:  "horizontalScroll",           // horizontal-scroll
	99:  "circularArrow",              // circular-arrow
	100: "pie",                        // mso-spt100
	101: "uturnArrow",                 // mso-spt101
	102: "curvedRightArrow",           // mso-spt102
	103: "curvedLeftArrow",            // mso-spt103
	104: "curvedUpArrow",              // mso-spt104
	105: "curvedDownArrow",            // mso-spt105
	106: "cloudCallout",               // cloud-callout
	107: "ellipseRibbon",              // mso-spt107
	108: "ellipseRibbon2",             // mso-spt108
	109: "flowChartProcess",           // flowchart-process
	110: "flowChartDecision",          // flowchart-decision
	111: "flowChartInputOutput",       // flowchart-data
	112: "flowChartPredefinedProcess", // flowchart-predefined-process
	113: "flowChartInternalStorage",   // flowchart-internal-storage
	114: "flowChartDocument",          // flowchart-document
	115: "flowChartMultidocument",     // flowchart-multidocument
	116: "flowChartTerminator",        // flowchart-terminator
	117: "flowChartPreparation",       // flowchart-preparation
	118: "flowChartManualInput",       // flowchart-manual-input
	119: "flowChartManualOperation",   // flowchart-manual-operation
	120: "flowChartConnector",         // flowchart-connector
	121: "flowChartPunchedCard",       // flowchart-card
	122: "flowChartPunchedTape",       // flowchart-punched-tape
	123: "flowChartSummingJunction",   // flowchart-summing-junction
	124: "flowChartOr",                // flowchart-or
	125: "flowChartCollate",           // flowchart-collate
	126: "flowChartSort",              // flowchart-sort
	127: "flowChartExtract",           // flowchart-extract
	128: "flowChartMerge",             // flowchart-merge
	129: "flowChartOfflineStorage",    // mso-spt129
	130: "flowChartOnlineStorage",     // flowchart-stored-data
	131: "flowChartMagneticTape",      // flowchart-sequential-access
	132: "flowChartMagneticDisk",      // flowchart-magnetic-disk
	133: "flowChartMagneticDrum",      // flowchart-direct-access-storage
	134: "flowChartDisplay",           // flowchart-display
	135: "flowChartDelay",             // flowchart-delay
	176: "flowChartAlternateProcess",  // flowchart-alternate-process
	177: "flowChartOffpageConnector",  // flowchart-off-page-connector
	178: "callout1",                   // mso-spt178
	179: "accentCallout1",             // mso-spt179
	180: "borderCallout1",             // mso-spt180
	181: "borderCallout3",             // line-callout-3
	182: "leftRightUpArrow",           // mso-spt182
	183: "sun",                        // sun
	184: "moon",                       // moon
	185: "bracketPair",                // bracket-pair
	186: "bracePair",                  // brace-pair
	187: "star4",                      // star4
	188: "doubleWave",                 // mso-spt188
	189: "actionButtonBlank",          // mso-spt189
	190: "actionButtonHome",           // mso-spt190
	191: "actionButtonHelp",           // mso-spt191
	192: "actionButtonInformation",    // mso-spt192
	193: "actionButtonForwardNext",    // mso-spt193
	194: "actionButtonBackPrevious",   // mso-spt194
	195: "actionButtonEnd",            // mso-spt195
	196: "actionButtonBeginning",      // mso-spt196
	197: "actionButtonReturn",         // mso-spt197
	198: "actionButtonDocument",       // mso-spt198
	199: "actionButtonSound",          // mso-spt199
	200: "actionButtonMovie",          // mso-spt200
	202: "rect",                       // mso-spt202
}

// VMLPreset returns the DrawingML preset the VML shape type spt draws,
// and whether it has one.
func VMLPreset(spt int) (string, bool) {
	p, ok := vmlPresets[spt]
	return p, ok
}
