package compliance

import "strings"

// xmpPropType represents the expected value type for an XMP property.
type xmpPropType int

const (
	// Simple value types: stored as text content or attributes.
	xmpPropText    xmpPropType = iota // Plain text string.
	xmpPropURI                        // URI string.
	xmpPropInteger                    // Integer value.
	xmpPropReal                       // Real/float value.
	xmpPropBoolean                    // Boolean (True/False).
	xmpPropDate                       // ISO 8601 date.

	// Container types: wrapped in rdf:Alt, rdf:Seq, or rdf:Bag.
	xmpPropAlt    // rdf:Alt (language alternatives).
	xmpPropSeq    // rdf:Seq (ordered list).
	xmpPropBag    // rdf:Bag (unordered set).
	xmpPropStruct // Structured value (nested properties).

	// xmpPropAny matches any type (for properties whose type is flexible).
	xmpPropAny
)

// xmpPropertyRegistry maps predefined XMP namespace URIs to the set of valid
// property names and their expected types. Properties not listed here require
// a PDF/A extension schema declaration.
//
// Sources: XMP Specification Parts 1-3 (ISO 16684), EXIF/XMP mapping,
// Adobe Camera Raw schema, ISO 19005 (PDF/A identification).
//
//nolint:gochecknoglobals // static lookup table.
var xmpPropertyRegistry = map[string]map[string]xmpPropType{
	// Known schema namespaces with no predefined properties: pdfx (custom
	// Document Info keys) and adhocwf (Acrobat workflow) are recognized
	// schemas, but neither standard defines named members, so every property
	// used in them is an unknown name whose value type cannot be resolved.
	// See the matching empty entries in xmpPredefinedCommon.
	"http://ns.adobe.com/pdfx/1.3/":                 {},
	"http://ns.adobe.com/AcrobatAdhocWorkflow/1.0/": {},
	"http://www.aiim.org/pdfua/ns/id/":              {},

	// Dublin Core (dc:).
	"http://purl.org/dc/elements/1.1/": {
		"contributor": xmpPropBag, "coverage": xmpPropText, "creator": xmpPropSeq,
		"date": xmpPropSeq, "description": xmpPropAlt, "format": xmpPropText,
		"identifier": xmpPropText, "language": xmpPropBag, "publisher": xmpPropBag,
		"relation": xmpPropBag, "rights": xmpPropAlt, "source": xmpPropText,
		"subject": xmpPropBag, "title": xmpPropAlt, "type": xmpPropBag,
	},

	// XMP Basic (xmp:).
	"http://ns.adobe.com/xap/1.0/": {
		"Advisory":     xmpPropBag,
		"BaseURL":      xmpPropURI,
		"CreateDate":   xmpPropDate,
		"CreatorTool":  xmpPropText,
		"Identifier":   xmpPropBag,
		"Label":        xmpPropText,
		"MetadataDate": xmpPropDate,
		"ModifyDate":   xmpPropDate,
		"Nickname":     xmpPropText,
		"Rating":       xmpPropReal,
		"Thumbnails":   xmpPropAlt,
	},

	// XMP Media Management (xmpMM:).
	"http://ns.adobe.com/xap/1.0/mm/": {
		"DerivedFrom":        xmpPropStruct,
		"DocumentID":         xmpPropURI,
		"History":            xmpPropSeq,
		"InstanceID":         xmpPropURI,
		"Ingredients":        xmpPropBag,
		"LastURL":            xmpPropURI,
		"ManagedFrom":        xmpPropStruct,
		"Manager":            xmpPropText,
		"ManageUI":           xmpPropURI,
		"ManageTo":           xmpPropURI,
		"ManagerVariant":     xmpPropText,
		"OriginalDocumentID": xmpPropURI,
		"Pantry":             xmpPropBag,
		"RenditionClass":     xmpPropText,
		"RenditionOf":        xmpPropStruct,
		"RenditionParams":    xmpPropText,
		"SaveID":             xmpPropInteger,
		"Subject":            xmpPropBag,
		"VersionID":          xmpPropText,
		"Versions":           xmpPropSeq,
	},

	// XMP Rights (xmpRights:).
	"http://ns.adobe.com/xap/1.0/rights/": {
		"Certificate":  xmpPropURI,
		"Marked":       xmpPropBoolean,
		"Owner":        xmpPropBag,
		"UsageTerms":   xmpPropAlt,
		"WebStatement": xmpPropURI,
	},

	// PDF (pdf:).
	"http://ns.adobe.com/pdf/1.3/": {
		"Keywords":   xmpPropText,
		"PDFVersion": xmpPropText,
		"Producer":   xmpPropText,
		"Trapped":    xmpPropBoolean,
	},

	// PDF/A Identification (pdfaid:).
	"http://www.aiim.org/pdfa/ns/id/": {
		"amd":         xmpPropText,
		"conformance": xmpPropText,
		"part":        xmpPropInteger,
		"corr":        xmpPropText,
		"rev":         xmpPropText,
	},

	// Photoshop (photoshop:).
	"http://ns.adobe.com/photoshop/1.0/": {
		"AuthorsPosition": xmpPropText, "CaptionWriter": xmpPropText,
		"Category": xmpPropText, "City": xmpPropText,
		"ColorMode": xmpPropInteger, "Country": xmpPropText,
		"Credit": xmpPropText, "DateCreated": xmpPropDate,
		"DocumentAncestors": xmpPropBag, "Headline": xmpPropText,
		"History": xmpPropText, "ICCProfile": xmpPropText,
		"Instructions": xmpPropText, "LabelColor": xmpPropText,
		"Source": xmpPropText, "State": xmpPropText,
		"SupplementalCategories": xmpPropBag, "TextLayers": xmpPropSeq,
		"TransmissionReference": xmpPropText, "Urgency": xmpPropInteger,
		"SidecarForExtension": xmpPropText, "EmbeddedXMPDigest": xmpPropText,
		"AncestorLink": xmpPropText, "LegacyIPTCDigest": xmpPropText,
	},

	// EXIF (exif:).
	"http://ns.adobe.com/exif/1.0/": {
		"ApertureValue": xmpPropReal, "BrightnessValue": xmpPropReal,
		"CFAPattern": xmpPropStruct, "ColorSpace": xmpPropInteger,
		"CompressedBitsPerPixel": xmpPropReal, "ComponentsConfiguration": xmpPropSeq,
		"Contrast": xmpPropInteger, "CustomRendered": xmpPropInteger,
		"DateTimeDigitized": xmpPropDate, "DateTimeOriginal": xmpPropDate,
		"DeviceSettingDescription": xmpPropStruct, "DigitalZoomRatio": xmpPropReal,
		"ExifVersion": xmpPropText, "ExposureBiasValue": xmpPropReal,
		"ExposureIndex": xmpPropReal, "ExposureMode": xmpPropInteger,
		"ExposureProgram": xmpPropInteger, "ExposureTime": xmpPropReal,
		"FileSource": xmpPropInteger, "Flash": xmpPropStruct,
		"FlashEnergy": xmpPropReal, "FlashpixVersion": xmpPropText,
		"FNumber": xmpPropReal, "FocalLength": xmpPropReal,
		"FocalLengthIn35mmFilm": xmpPropInteger, "FocalPlaneResolutionUnit": xmpPropInteger,
		"FocalPlaneXResolution": xmpPropReal, "FocalPlaneYResolution": xmpPropReal,
		"GainControl": xmpPropInteger, "GPSAltitude": xmpPropReal,
		"GPSAltitudeRef": xmpPropInteger, "GPSAreaInformation": xmpPropText,
		"GPSDestBearing": xmpPropReal, "GPSDestBearingRef": xmpPropText,
		"GPSDestDistance": xmpPropReal, "GPSDestDistanceRef": xmpPropText,
		"GPSDestLatitude": xmpPropText, "GPSDestLongitude": xmpPropText,
		"GPSDifferential": xmpPropInteger, "GPSDOP": xmpPropReal,
		"GPSImgDirection": xmpPropReal, "GPSImgDirectionRef": xmpPropText,
		"GPSLatitude": xmpPropText, "GPSLongitude": xmpPropText,
		"GPSMapDatum": xmpPropText, "GPSMeasureMode": xmpPropText,
		"GPSProcessingMethod": xmpPropText, "GPSSatellites": xmpPropText,
		"GPSSpeed": xmpPropReal, "GPSSpeedRef": xmpPropText,
		"GPSStatus": xmpPropText, "GPSTimeStamp": xmpPropDate,
		"GPSTrack": xmpPropReal, "GPSTrackRef": xmpPropText,
		"GPSVersionID": xmpPropText, "ImageUniqueID": xmpPropText,
		"ISOSpeedRatings": xmpPropSeq, "LightSource": xmpPropInteger,
		"MakerNote": xmpPropText, "MaxApertureValue": xmpPropReal,
		"MeteringMode": xmpPropInteger, "NativeDigest": xmpPropText,
		"OECF": xmpPropStruct, "PixelXDimension": xmpPropInteger,
		"PixelYDimension": xmpPropInteger, "RelatedSoundFile": xmpPropText,
		"Saturation": xmpPropInteger, "SceneCaptureType": xmpPropInteger,
		"SceneType": xmpPropInteger, "SensingMethod": xmpPropInteger,
		"Sharpness": xmpPropInteger, "ShutterSpeedValue": xmpPropReal,
		"SpatialFrequencyResponse": xmpPropStruct, "SpectralSensitivity": xmpPropText,
		"SubjectArea": xmpPropSeq, "SubjectDistance": xmpPropReal,
		"SubjectDistanceRange": xmpPropInteger, "SubjectLocation": xmpPropSeq,
		"UserComment": xmpPropAlt, "WhiteBalance": xmpPropInteger,
		"Gamma": xmpPropReal, "PhotographicSensitivity": xmpPropInteger,
		"SensitivityType": xmpPropInteger, "StandardOutputSensitivity": xmpPropInteger,
		"RecommendedExposureIndex": xmpPropInteger, "ISOSpeed": xmpPropInteger,
		"ISOSpeedLatitudeyyy": xmpPropInteger, "ISOSpeedLatitudezzz": xmpPropInteger,
		"CameraOwnerName": xmpPropText, "BodySerialNumber": xmpPropText,
		"LensInfo": xmpPropSeq, "LensMake": xmpPropText,
		"LensModel": xmpPropText, "LensSerialNumber": xmpPropText,
	},

	// EXIF Auxiliary (aux:).
	"http://ns.adobe.com/exif/1.0/aux/": {
		"Firmware": xmpPropText, "FlashCompensation": xmpPropReal,
		"ImageNumber": xmpPropInteger, "Lens": xmpPropText,
		"LensID": xmpPropInteger, "LensInfo": xmpPropSeq,
		"OwnerName": xmpPropText, "SerialNumber": xmpPropText,
		"ApproximateFocusDistance": xmpPropReal,
	},

	// TIFF (tiff:).
	"http://ns.adobe.com/tiff/1.0/": {
		"Artist": xmpPropText, "BitsPerSample": xmpPropSeq,
		"Compression": xmpPropInteger, "Copyright": xmpPropAlt,
		"DateTime": xmpPropDate, "ImageDescription": xmpPropAlt,
		"ImageLength": xmpPropInteger, "ImageWidth": xmpPropInteger,
		"Make": xmpPropText, "Model": xmpPropText,
		"NativeDigest": xmpPropText, "Orientation": xmpPropInteger,
		"PhotometricInterpretation": xmpPropInteger, "PlanarConfiguration": xmpPropInteger,
		"PrimaryChromaticities": xmpPropSeq, "ReferenceBlackWhite": xmpPropSeq,
		"ResolutionUnit": xmpPropInteger, "SamplesPerPixel": xmpPropInteger,
		"Software": xmpPropText, "TransferFunction": xmpPropSeq,
		"WhitePoint": xmpPropSeq, "XResolution": xmpPropReal,
		"YCbCrCoefficients": xmpPropSeq, "YCbCrPositioning": xmpPropInteger,
		"YCbCrSubSampling": xmpPropSeq, "YResolution": xmpPropReal,
	},

	// Camera Raw Settings (crs:).
	"http://ns.adobe.com/camera-raw-settings/1.0/": {
		"AutoBrightness": xmpPropBoolean, "AutoContrast": xmpPropBoolean,
		"AutoExposure": xmpPropBoolean, "AutoShadows": xmpPropBoolean,
		"BlueHue": xmpPropInteger, "BlueSaturation": xmpPropInteger,
		"Brightness": xmpPropInteger, "CameraProfile": xmpPropText,
		"ChromaticAberrationB": xmpPropInteger, "ChromaticAberrationR": xmpPropInteger,
		"ColorNoiseReduction": xmpPropInteger, "Contrast": xmpPropInteger,
		"CropAngle": xmpPropReal, "CropBottom": xmpPropReal,
		"CropHeight": xmpPropReal, "CropLeft": xmpPropReal,
		"CropRight": xmpPropReal, "CropTop": xmpPropReal,
		"CropUnits": xmpPropInteger, "CropWidth": xmpPropReal,
		"Exposure": xmpPropReal, "GreenHue": xmpPropInteger,
		"GreenSaturation": xmpPropInteger, "HasCrop": xmpPropBoolean,
		"HasSettings": xmpPropBoolean, "LuminanceSmoothing": xmpPropInteger,
		"RawFileName": xmpPropText, "RedHue": xmpPropInteger,
		"RedSaturation": xmpPropInteger, "Saturation": xmpPropInteger,
		"Shadows": xmpPropInteger, "ShadowTint": xmpPropInteger,
		"Sharpness": xmpPropInteger, "Temperature": xmpPropInteger,
		"Tint": xmpPropInteger, "ToneCurve": xmpPropSeq,
		"ToneCurveName": xmpPropText, "Version": xmpPropText,
		"VignetteAmount": xmpPropInteger, "VignetteMidpoint": xmpPropInteger,
		"WhiteBalance": xmpPropText,
		"Exposure2012": xmpPropReal, "Contrast2012": xmpPropInteger,
		"Highlights2012": xmpPropInteger, "Shadows2012": xmpPropInteger,
		"Whites2012": xmpPropInteger, "Blacks2012": xmpPropInteger,
		"Clarity2012": xmpPropInteger, "Vibrance": xmpPropInteger,
		"Defringe": xmpPropInteger, "Dehaze": xmpPropInteger,
		"LensProfileEnable": xmpPropInteger, "LensManualDistortionAmount": xmpPropInteger,
		"PerspectiveVertical": xmpPropInteger, "PerspectiveHorizontal": xmpPropInteger,
		"PerspectiveRotate": xmpPropReal, "PerspectiveScale": xmpPropInteger,
		"PostCropVignetteAmount": xmpPropInteger, "GrainAmount": xmpPropInteger,
		"SplitToningHighlightHue": xmpPropInteger, "SplitToningHighlightSaturation": xmpPropInteger,
		"SplitToningShadowHue": xmpPropInteger, "SplitToningShadowSaturation": xmpPropInteger,
		"SplitToningBalance": xmpPropInteger,
		"ParametricShadows":  xmpPropInteger, "ParametricDarks": xmpPropInteger,
		"ParametricLights": xmpPropInteger, "ParametricHighlights": xmpPropInteger,
		"ParametricShadowSplit": xmpPropInteger, "ParametricMidtoneSplit": xmpPropInteger,
		"ParametricHighlightSplit": xmpPropInteger,
		"SharpenRadius":            xmpPropReal, "SharpenDetail": xmpPropInteger,
		"SharpenEdgeMasking":        xmpPropInteger,
		"ColorNoiseReductionDetail": xmpPropInteger, "ColorNoiseReductionSmoothness": xmpPropInteger,
		"LuminanceNoiseReductionDetail": xmpPropInteger, "LuminanceNoiseReductionContrast": xmpPropInteger,
		"ConvertToGrayscale": xmpPropBoolean, "CameraProfileDigest": xmpPropText,
		"AlreadyApplied": xmpPropBoolean, "ProcessVersion": xmpPropText,
	},

	// XMP Dynamic Media (xmpDM:).
	"http://ns.adobe.com/xmp/1.0/DynamicMedia/": {
		"absPeakAudioFilePath": xmpPropURI, "album": xmpPropText,
		"altTapeName": xmpPropText, "altTimecode": xmpPropStruct,
		"artist": xmpPropText, "audioChannelType": xmpPropText,
		"audioCompressor": xmpPropText, "audioModDate": xmpPropDate,
		"audioSampleRate": xmpPropInteger, "audioSampleType": xmpPropText,
		"beatSpliceParams": xmpPropStruct, "cameraAngle": xmpPropText,
		"cameraLabel": xmpPropText, "cameraModel": xmpPropText,
		"cameraMove": xmpPropText, "client": xmpPropText,
		"comment": xmpPropText, "composer": xmpPropText,
		"contributedMedia": xmpPropBag, "copyright": xmpPropText,
		"director": xmpPropText, "directorPhotography": xmpPropText,
		"discNumber": xmpPropText, "duration": xmpPropStruct,
		"engineer": xmpPropText, "fileDataRate": xmpPropReal,
		"genre": xmpPropText, "good": xmpPropBoolean,
		"instrument": xmpPropText, "introTime": xmpPropStruct,
		"key": xmpPropText, "logComment": xmpPropText,
		"loop": xmpPropBoolean, "lyrics": xmpPropText,
		"markers": xmpPropSeq, "metadataModDate": xmpPropDate,
		"numberOfBeats": xmpPropReal, "outCue": xmpPropStruct,
		"partOfCompilation": xmpPropBoolean, "projectName": xmpPropText,
		"projectRef": xmpPropStruct, "pullDown": xmpPropText,
		"relativePeakAudioFilePath": xmpPropURI,
		"relativeTimestamp":         xmpPropStruct, "releaseDate": xmpPropDate,
		"resampleParams": xmpPropStruct, "scaleType": xmpPropText,
		"scene": xmpPropText, "shotDate": xmpPropDate,
		"shotDay": xmpPropText, "shotLocation": xmpPropText,
		"shotName": xmpPropText, "shotNumber": xmpPropText,
		"shotSize": xmpPropText, "speakerPlacement": xmpPropText,
		"startTimecode": xmpPropStruct, "stretchMode": xmpPropText,
		"takeNumber": xmpPropInteger, "tapeName": xmpPropText,
		"tempo": xmpPropReal, "timeScaleParams": xmpPropStruct,
		"timeSignature": xmpPropText, "trackNumber": xmpPropInteger,
		"Tracks": xmpPropBag, "videoAlphaMode": xmpPropText,
		"videoAlphaPremultipleColor":   xmpPropStruct,
		"videoAlphaUnityIsTransparent": xmpPropBoolean,
		"videoColorSpace":              xmpPropText, "videoCompressor": xmpPropText,
		"videoFieldOrder": xmpPropText, "videoFrameRate": xmpPropReal,
		"videoFrameSize": xmpPropStruct, "videoModDate": xmpPropDate,
		"videoPixelAspectRatio": xmpPropReal, "videoPixelDepth": xmpPropText,
	},

	// XMP Paged-Text (xmpTPg:).
	"http://ns.adobe.com/xap/1.0/t/pg/": {
		"MaxPageSize": xmpPropStruct, "NPages": xmpPropInteger,
		"Fonts": xmpPropBag, "Colorants": xmpPropSeq,
		"PlateNames": xmpPropSeq, "SwatchGroups": xmpPropSeq,
		"HasVisibleOverprint": xmpPropBoolean, "HasVisibleTransparency": xmpPropBoolean,
	},

	// XMP Basic Job Ticket (xmpBJ:).
	"http://ns.adobe.com/xap/1.0/bj/": {
		"JobRef": xmpPropBag,
	},
}

// isContainerPropType returns true if the type is an RDF container (Alt/Seq/Bag).
func isContainerPropType(t xmpPropType) bool {
	return t >= xmpPropAlt && t <= xmpPropBag
}

// isSimplePropType returns true if the type is a simple value (not a container or struct).
func isSimplePropType(t xmpPropType) bool {
	return t <= xmpPropDate
}

// isValidXMPInteger returns true if s is a valid XMP integer value.
// Format: optional sign followed by digits. Empty string is invalid.
func isValidXMPInteger(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}

	start := 0
	if s[0] == '+' || s[0] == '-' {
		start = 1
	}

	if start >= len(s) {
		return false
	}

	for i := start; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}

// isValidXMPReal returns true if s is a valid XMP real/float value.
// Format: optional sign, digits, optional decimal point followed by digits.
// For EXIF properties that accept rationals, use isValidXMPRealOrRational.
func isValidXMPReal(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}

	start := 0
	if s[0] == '+' || s[0] == '-' {
		start = 1
	}

	if start >= len(s) {
		return false
	}

	hasDigit := false
	hasDot := false

	for i := start; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			hasDigit = true

			continue
		}

		if s[i] == '.' && !hasDot {
			hasDot = true

			continue
		}

		return false
	}

	return hasDigit
}

// isValidXMPBoolean returns true if s is a valid XMP boolean value.
// XMP booleans are case-sensitive: "True" or "False".
func isValidXMPBoolean(s string) bool {
	s = strings.TrimSpace(s)
	return s == "True" || s == "False"
}

// isValidXMPRealOrRational returns true if s is a valid decimal or EXIF rational.
// Used for EXIF namespace properties typed as Real that also accept "N/D" format.
func isValidXMPRealOrRational(s string) bool {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		return isValidXMPRational(s)
	}
	return isValidXMPReal(s)
}

// isValidXMPDate returns true if s is a valid XMP date value.
// Accepts ISO 8601 formats: YYYY, YYYY-MM, YYYY-MM-DD, YYYY-MM-DDThh:mm,
// YYYY-MM-DDThh:mm:ss, with optional fractional seconds and timezone (Z or
// ±hh:mm). Also accepts PDF date format (D:YYYY...) since some producers
// write PDF-format dates into XMP properties.
func isValidXMPDate(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return false
	}

	// PDF date strings (D:YYYY...) are NOT valid XMP dates: the XMP Date
	// type is ISO 8601 (XMP Specification part 2 / ISO 16684), and the
	// PDF "D:" prefix is not ISO 8601 syntax, so such a value cannot
	// satisfy a Date-typed XMP property.
	if strings.HasPrefix(s, "D:") {
		return false
	}

	// Must start with 4 digits (year).
	for i := range 4 {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	if len(s) == 4 {
		return true // YYYY
	}

	// YYYY-MM
	if s[4] != '-' {
		return false
	}
	if len(s) < 7 || !isDigit(s[5]) || !isDigit(s[6]) {
		return false
	}
	if len(s) == 7 {
		return true // YYYY-MM
	}

	// YYYY-MM-DD
	if s[7] != '-' {
		return false
	}
	if len(s) < 10 || !isDigit(s[8]) || !isDigit(s[9]) {
		return false
	}
	if len(s) == 10 {
		return true // YYYY-MM-DD
	}

	// Time component requires 'T' separator.
	if s[10] != 'T' {
		return false
	}
	// At minimum: YYYY-MM-DDThh:mm (length 16).
	if len(s) < 16 {
		return false
	}
	if !isDigit(s[11]) || !isDigit(s[12]) || s[13] != ':' || !isDigit(s[14]) || !isDigit(s[15]) {
		return false
	}

	rest := s[16:]
	// Optional :ss
	if len(rest) > 0 && rest[0] == ':' {
		if len(rest) < 3 || !isDigit(rest[1]) || !isDigit(rest[2]) {
			return false
		}
		rest = rest[3:]
	}
	// Optional fractional seconds (.digits).
	if len(rest) > 0 && rest[0] == '.' {
		i := 1
		for i < len(rest) && isDigit(rest[i]) {
			i++
		}
		if i == 1 {
			return false // "." with no digits
		}
		rest = rest[i:]
	}
	// Timezone: Z, +hh:mm, or -hh:mm. Empty means local time.
	if len(rest) == 0 {
		return true
	}
	if rest == "Z" {
		return true
	}
	if rest[0] != '+' && rest[0] != '-' {
		return false
	}
	// +hh:mm or +hh (abbreviated).
	switch len(rest) {
	case 3:
		return isDigit(rest[1]) && isDigit(rest[2])
	case 6:
		return isDigit(rest[1]) && isDigit(rest[2]) && rest[3] == ':' && isDigit(rest[4]) && isDigit(rest[5])
	}
	return false
}

// isValidXMPURI returns true if s is a valid XMP URI value.
// A URI is any non-empty string.
func isValidXMPURI(s string) bool {
	s = strings.TrimSpace(s)
	return s != ""
}

// isValidXMPRational returns true if s is a valid EXIF rational value.
// Format: integer/integer (e.g., "72/1", "1/100").
//
//nolint:unused // API for EXIF rational validation; used indirectly via container element checks.
func isValidXMPRational(s string) bool {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, "/")

	if len(parts) != 2 {
		return false
	}

	return isValidXMPInteger(parts[0]) && isValidXMPInteger(parts[1])
}

// isValidXMPGPSCoordinate returns true if s is a valid GPS coordinate value.
// Format: "DDD,MM,SSk" or "DDD,MM.MMk" where k is N/S/E/W.
//
//nolint:unused // API for EXIF GPS validation; used indirectly via container element checks.
func isValidXMPGPSCoordinate(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 5 {
		return false
	}

	last := s[len(s)-1]
	return last == 'N' || last == 'S' || last == 'E' || last == 'W'
}

// validateXMPValue checks if a text value matches the expected XMP property type format.
// Returns true if valid or if the type doesn't have a format constraint.
func validateXMPValue(propType xmpPropType, value string) bool {
	switch propType {
	case xmpPropInteger:
		return isValidXMPInteger(value)
	case xmpPropReal:
		return isValidXMPReal(value)
	case xmpPropBoolean:
		return isValidXMPBoolean(value)
	case xmpPropDate:
		return isValidXMPDate(value)
	case xmpPropURI:
		return isValidXMPURI(value)
	case xmpPropText:
		// Per ISO 16684-1, Text is any sequence of Unicode characters, so every
		// value matches the type: a blank one is a conforming Text value, not a
		// type mismatch. Acrobat Distiller 6.0.1 emits pdf:Keywords=' ' (govdocs
		// thread_300_300483.pdf) and veraPDF passes it.
		return true
	case xmpPropAlt, xmpPropSeq, xmpPropBag, xmpPropStruct, xmpPropAny:
		return true // Containers and structs validated separately.
	}
	return true
}
