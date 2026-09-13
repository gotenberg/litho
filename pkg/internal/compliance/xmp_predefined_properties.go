package compliance

// Predefined XMP schema property catalogs.
//
// Normative sources: XMP Specification (Adobe, January 2004) as referenced
// by ISO 19005-1 §6.7.9, and XMP Specification (Adobe, September 2005,
// later ISO 16684-1) as referenced by ISO 19005-2/3 §6.6.2.3.1. Those
// clauses require every XMP property to use either a predefined schema or
// a PDF/A extension schema, so the exact predefined property sets are
// compliance-determining spec tables, not heuristics. The property names
// and namespaces below are the schemas defined in those two XMP
// Specifications (Dublin Core, XMP Basic, XMP Rights, XMP Media
// Management, Adobe PDF, Photoshop, TIFF, EXIF and, for the 2005 variant,
// Camera Raw, EXIF Auxiliary and XMP Dynamic Media), keyed by the version
// that introduced each property so a packet is validated against the XMP
// edition normative for its PDF/A part.
//
// Value-type strings use the XMP lowercase notation ("rational",
// "seq integer", "lang alt", ...) so a later value-type phase can match
// each property against its declared XMP type. This pass consumes
// presence only.

import "maps"

// xmpPredefinedCommon lists the properties predefined identically by both
// the 2004 and 2005 XMP Specifications, so they are valid in every PDF/A
// part's catalog without an extension schema.
//
//nolint:gochecknoglobals // static spec table
var xmpPredefinedCommon = map[string]map[string]string{
	// PDF/A Identification (pdfaid:).
	"http://www.aiim.org/pdfa/ns/id/": {
		"part": "integer",
		"amd":  "text",
	},

	// Dublin Core (dc:).
	"http://purl.org/dc/elements/1.1/": {
		"contributor": "bag propername",
		"coverage":    "text",
		"creator":     "seq propername",
		"date":        "seq date",
		"description": "lang alt",
		"format":      "mimetype",
		"identifier":  "text",
		"language":    "bag locale",
		"publisher":   "bag propername",
		"relation":    "bag text",
		"rights":      "lang alt",
		"source":      "text",
		"subject":     "bag text",
		"title":       "lang alt",
		"type":        "bag text",
	},

	// XMP Basic (xmp:).
	"http://ns.adobe.com/xap/1.0/": {
		"Advisory":     "bag text",
		"BaseURL":      "url",
		"CreateDate":   "date",
		"CreatorTool":  "agentname",
		"Identifier":   "bag text",
		"MetadataDate": "date",
		"ModifyDate":   "date",
		"Nickname":     "text",
		"Thumbnails":   "alt thumbnail",
	},

	// XMP Rights (xmpRights:).
	"http://ns.adobe.com/xap/1.0/rights/": {
		"Certificate":  "url",
		"Marked":       "boolean",
		"Owner":        "bag propername",
		"UsageTerms":   "lang alt",
		"WebStatement": "url",
	},

	// XMP Media Management (xmpMM:). Ingredients, OriginalDocumentID,
	// Pantry and Subject are NOT in the XMP Specification editions
	// normative for PDF/A, so each one needs an extension schema
	// declaration when used.
	"http://ns.adobe.com/xap/1.0/mm/": {
		"DerivedFrom":     "resourceref",
		"DocumentID":      "uri",
		"History":         "seq resourceevent",
		"InstanceID":      "uri",
		"LastURL":         "url",
		"ManagedFrom":     "resourceref",
		"Manager":         "agentname",
		"ManageTo":        "uri",
		"ManageUI":        "uri",
		"ManagerVariant":  "text",
		"RenditionClass":  "renditionclass",
		"RenditionOf":     "resourceref",
		"RenditionParams": "text",
		"SaveID":          "integer",
		"VersionID":       "text",
		"Versions":        "seq version",
	},

	// XMP Basic Job Ticket (xmpBJ:).
	"http://ns.adobe.com/xap/1.0/bj/": {
		"JobRef": "bag job",
	},

	// XMP Paged-Text (xmpTPg:).
	"http://ns.adobe.com/xap/1.0/t/pg/": {
		"MaxPageSize": "dimensions",
		"NPages":      "integer",
	},

	// Adobe PDF (pdf:). Trapped is NOT predefined, so it needs an
	// extension schema declaration when used.
	"http://ns.adobe.com/pdf/1.3/": {
		"Keywords":   "text",
		"PDFVersion": "text",
		"Producer":   "agentname",
	},

	// Photoshop (photoshop:).
	"http://ns.adobe.com/photoshop/1.0/": {
		"AuthorsPosition":       "text",
		"CaptionWriter":         "propername",
		"Category":              "text",
		"City":                  "text",
		"Country":               "text",
		"Credit":                "text",
		"DateCreated":           "date",
		"Headline":              "text",
		"Instructions":          "text",
		"Source":                "text",
		"State":                 "text",
		"TransmissionReference": "text",
		"Urgency":               "integer",
	},

	// TIFF (tiff:).
	"http://ns.adobe.com/tiff/1.0/": {
		"Artist":                    "propername",
		"BitsPerSample":             "seq integer",
		"Compression":               "integer",
		"Copyright":                 "lang alt",
		"DateTime":                  "date",
		"ImageDescription":          "lang alt",
		"ImageLength":               "integer",
		"ImageWidth":                "integer",
		"Make":                      "propername",
		"Model":                     "propername",
		"Orientation":               "integer",
		"PhotometricInterpretation": "integer",
		"PlanarConfiguration":       "integer",
		"PrimaryChromaticities":     "seq rational",
		"ReferenceBlackWhite":       "seq rational",
		"ResolutionUnit":            "integer",
		"SamplesPerPixel":           "integer",
		"Software":                  "agentname",
		"TransferFunction":          "seq integer",
		"WhitePoint":                "seq rational",
		"XResolution":               "rational",
		"YCbCrCoefficients":         "seq rational",
		"YCbCrPositioning":          "integer",
		"YCbCrSubSampling":          "seq integer",
		"YResolution":               "rational",
	},

	// EXIF (exif:).
	"http://ns.adobe.com/exif/1.0/": {
		"ApertureValue":            "rational",
		"BrightnessValue":          "rational",
		"CFAPattern":               "cfapattern",
		"ComponentsConfiguration":  "seq integer",
		"CompressedBitsPerPixel":   "rational",
		"Contrast":                 "integer",
		"CustomRendered":           "integer",
		"DateTimeDigitized":        "date",
		"DateTimeOriginal":         "date",
		"DeviceSettingDescription": "devicesettings",
		"DigitalZoomRatio":         "rational",
		"ExposureBiasValue":        "rational",
		"ExposureIndex":            "rational",
		"ExposureMode":             "integer",
		"ExposureProgram":          "integer",
		"ExposureTime":             "rational",
		"FileSource":               "integer",
		"Flash":                    "flash",
		"FlashEnergy":              "rational",
		"FNumber":                  "rational",
		"FocalLength":              "rational",
		"FocalLengthIn35mmFilm":    "integer",
		"FocalPlaneResolutionUnit": "integer",
		"FocalPlaneXResolution":    "rational",
		"FocalPlaneYResolution":    "rational",
		"GainControl":              "integer",
		"GPSAltitude":              "rational",
		"GPSAltitudeRef":           "integer",
		"GPSAreaInformation":       "text",
		"GPSDestBearing":           "rational",
		"GPSDestBearingRef":        "text",
		"GPSDestDistance":          "rational",
		"GPSDestDistanceRef":       "text",
		"GPSDestLatitude":          "gpscoordinate",
		"GPSDestLongitude":         "gpscoordinate",
		"GPSDifferential":          "integer",
		"GPSDOP":                   "rational",
		"GPSImgDirection":          "rational",
		"GPSImgDirectionRef":       "text",
		"GPSLatitude":              "gpscoordinate",
		"GPSLongitude":             "gpscoordinate",
		"GPSMapDatum":              "text",
		"GPSProcessingMethod":      "text",
		"GPSSatellites":            "text",
		"GPSSpeed":                 "rational",
		"GPSSpeedRef":              "text",
		"GPSStatus":                "text",
		"GPSTimeStamp":             "date",
		"GPSTrack":                 "rational",
		"GPSTrackRef":              "text",
		"GPSVersionID":             "text",
		"ImageUniqueID":            "text",
		"ISOSpeedRatings":          "seq integer",
		"MaxApertureValue":         "rational",
		"MeteringMode":             "integer",
		"OECF":                     "oecf/sfr",
		"PixelXDimension":          "integer",
		"PixelYDimension":          "integer",
		"RelatedSoundFile":         "text",
		"Saturation":               "integer",
		"SceneCaptureType":         "integer",
		"SceneType":                "integer",
		"SensingMethod":            "integer",
		"Sharpness":                "integer",
		"ShutterSpeedValue":        "rational",
		"SpatialFrequencyResponse": "oecf/sfr",
		"SpectralSensitivity":      "text",
		"SubjectArea":              "seq integer",
		"SubjectDistance":          "rational",
		"SubjectDistanceRange":     "integer",
		"SubjectLocation":          "seq integer",
		"UserComment":              "lang alt",
		"WhiteBalance":             "integer",
	},

	// Known XMP namespaces that predefine no properties of their own. The
	// XMP Specification editions normative for PDF/A do not list any
	// property in these schemas, so every member used in a packet needs an
	// extension schema declaration. Without one, the property fails both
	// definedness (ISO 19005-1 §6.7.9 / ISO 19005-2/3 §6.6.2.3.1) and
	// value-type resolution under those same clauses.
	"http://ns.adobe.com/pdfx/1.3/":                 {},
	"http://ns.adobe.com/AcrobatAdhocWorkflow/1.0/": {},

	// The PDF/UA Identification schema is ISO 14289, not part of the XMP
	// Specification or ISO 19005. Under PDF/A parts 1-3 every pdfuaid
	// property is therefore not predefined and needs an extension schema
	// declaration, failing both definedness and value-type resolution
	// (ISO 19005-1 §6.7.9 / ISO 19005-2/3 §6.6.2.3.1) when undeclared.
	"http://www.aiim.org/pdfua/ns/id/": {},
}

// xmpPredefined2004Extra lists the properties predefined only by the 2004
// XMP Specification, the edition normative for PDF/A-1 (ISO 19005-1
// §6.7.9). Where the 2004 and 2005 editions disagree on a property's
// presence or value type, the 2004 form belongs here.
//
//nolint:gochecknoglobals // static spec table
var xmpPredefined2004Extra = map[string]map[string]string{
	"http://www.aiim.org/pdfa/ns/id/": {
		"conformance": "text",
	},
	"http://ns.adobe.com/photoshop/1.0/": {
		"SupplementalCategories": "text",
	},
	"http://ns.adobe.com/exif/1.0/": {
		"ColorSpace":      "integer",
		"ExifVersion":     "text",
		"FlashpixVersion": "text",
		"GPSMeasureMode":  "integer",
		"LightSource":     "integer",
		"MakerNote":       "text",
	},

	// Schemas the 2005 edition introduced, listed here with no properties so
	// part 1 knows the namespace yet predefines nothing under it. Without the
	// entry the namespace falls out of the catalog entirely and every property
	// it carries is waved through as belonging to a known schema, which is how
	// nine veraPDF test-suite files carrying xmpDM properties passed ISO 19005-1
	// §6.7.9 t2 (isPredefinedInXMP2004). A property in one of these still
	// passes when an extension schema declares it, which the caller checks
	// first.
	"http://ns.adobe.com/xmp/1.0/DynamicMedia/":    {},
	"http://ns.adobe.com/exif/1.0/aux/":            {},
	"http://ns.adobe.com/camera-raw-settings/1.0/": {},
}

// xmpPredefined2005Extra lists the properties predefined only by the 2005
// XMP Specification, the edition normative for PDF/A-2 and PDF/A-3 (ISO
// 19005-2/3 §6.6.2.3.1). exif:MakerNote is deliberately absent: the 2005
// edition dropped it, so under parts 2 and 3 it is no longer predefined
// and needs an extension schema declaration, whereas part 1 (the 2004
// edition, [xmpPredefined2004Extra]) still predefines it.
//
//nolint:gochecknoglobals // static spec table
var xmpPredefined2005Extra = map[string]map[string]string{
	"http://www.aiim.org/pdfa/ns/id/": {
		"conformance": "text",
		"corr":        "text",
	},
	"http://ns.adobe.com/xap/1.0/": {
		"Label":  "text",
		"Rating": "real",
	},
	"http://ns.adobe.com/xap/1.0/t/pg/": {
		"Colorants":  "seq colorant",
		"Fonts":      "bag font",
		"PlateNames": "seq text",
	},
	"http://ns.adobe.com/photoshop/1.0/": {
		"SupplementalCategories": "bag text",
	},
	"http://ns.adobe.com/exif/1.0/": {
		"ColorSpace":      "integer",
		"ExifVersion":     "text",
		"FlashpixVersion": "text",
		"GPSMeasureMode":  "text",
		"LightSource":     "integer",
	},
	// EXIF Auxiliary (aux:). Introduced by the 2005 edition, absent from
	// the 2004 catalog entirely.
	"http://ns.adobe.com/exif/1.0/aux/": {
		"Lens":         "text",
		"SerialNumber": "text",
	},
	// Camera Raw (crs:). Introduced by the 2005 edition, absent from the
	// 2004 catalog.
	"http://ns.adobe.com/camera-raw-settings/1.0/": {
		"AutoBrightness":       "boolean",
		"AutoContrast":         "boolean",
		"AutoExposure":         "boolean",
		"AutoShadows":          "boolean",
		"BlueHue":              "integer",
		"BlueSaturation":       "integer",
		"Brightness":           "integer",
		"CameraProfile":        "text",
		"ChromaticAberrationB": "integer",
		"ChromaticAberrationR": "integer",
		"ColorNoiseReduction":  "integer",
		"Contrast":             "integer",
		"CropAngle":            "real",
		"CropBottom":           "real",
		"CropHeight":           "real",
		"CropLeft":             "real",
		"CropRight":            "real",
		"CropTop":              "real",
		"CropUnits":            "integer",
		"CropWidth":            "real",
		"Exposure":             "real",
		"GreenHue":             "integer",
		"GreenSaturation":      "integer",
		"HasCrop":              "boolean",
		"HasSettings":          "boolean",
		"LuminanceSmoothing":   "integer",
		"RawFileName":          "text",
		"RedHue":               "integer",
		"RedSaturation":        "integer",
		"Saturation":           "integer",
		"Shadows":              "integer",
		"ShadowTint":           "integer",
		"Sharpness":            "integer",
		"Temperature":          "integer",
		"Tint":                 "integer",
		"ToneCurve":            "seq text",
		"ToneCurveName":        "text",
		"Version":              "text",
		"VignetteAmount":       "integer",
		"VignetteMidpoint":     "integer",
		"WhiteBalance":         "text",
	},
	// XMP Dynamic Media (xmpDM:). Introduced by the 2005 edition, absent
	// from the 2004 catalog.
	"http://ns.adobe.com/xmp/1.0/DynamicMedia/": {
		"absPeakAudioFilePath":         "uri",
		"album":                        "text",
		"altTapeName":                  "text",
		"altTimecode":                  "timecode",
		"artist":                       "text",
		"audioChannelType":             "text",
		"audioCompressor":              "text",
		"audioModDate":                 "date",
		"audioSampleRate":              "integer",
		"audioSampleType":              "text",
		"beatSpliceParams":             "beatsplicestretch",
		"composer":                     "text",
		"contributedMedia":             "bag media",
		"copyright":                    "text",
		"duration":                     "time",
		"engineer":                     "text",
		"fileDataRate":                 "rational",
		"genre":                        "text",
		"instrument":                   "text",
		"introTime":                    "time",
		"key":                          "text",
		"logComment":                   "text",
		"loop":                         "boolean",
		"markers":                      "seq marker",
		"metadataModDate":              "date",
		"numberOfBeats":                "real",
		"outCue":                       "time",
		"projectRef":                   "projectlink",
		"pullDown":                     "text",
		"relativePeakAudioFilePath":    "uri",
		"relativeTimestamp":            "time",
		"releaseDate":                  "date",
		"resampleParams":               "resamplestretch",
		"scaleType":                    "text",
		"scene":                        "text",
		"shotDate":                     "date",
		"shotLocation":                 "text",
		"shotName":                     "text",
		"speakerPlacement":             "text",
		"startTimecode":                "timecode",
		"stretchMode":                  "text",
		"tapeName":                     "text",
		"tempo":                        "real",
		"timeScaleParams":              "timescalestretch",
		"timeSignature":                "text",
		"trackNumber":                  "integer",
		"videoAlphaMode":               "text",
		"videoAlphaPremultipleColor":   "colorant",
		"videoAlphaUnityIsTransparent": "boolean",
		"videoColorSpace":              "text",
		"videoCompressor":              "text",
		"videoFieldOrder":              "text",
		"videoFrameRate":               "text",
		"videoFrameSize":               "dimensions",
		"videoModDate":                 "date",
		"videoPixelAspectRatio":        "rational",
		"videoPixelDepth":              "text",
	},
}

// xmpPredefinedXMP2004 is the merged XMP 2004 catalog (PDF/A-1, ISO
// 19005-1 §6.7.9).
//
//nolint:gochecknoglobals // static spec table, merged once at init
var xmpPredefinedXMP2004 = xmpMergePredefined(xmpPredefinedCommon, xmpPredefined2004Extra)

// xmpPredefinedXMP2005 is the merged XMP 2005 catalog (PDF/A-2 and
// PDF/A-3, ISO 19005-2/3 §6.6.2.3.1).
//
//nolint:gochecknoglobals // static spec table, merged once at init
var xmpPredefinedXMP2005 = xmpMergePredefined(xmpPredefinedCommon, xmpPredefined2005Extra)

// xmpMergePredefined returns a deep copy of base overlaid with extra,
// leaving both inputs untouched.
func xmpMergePredefined(base, extra map[string]map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(base)+len(extra))
	for ns, props := range base {
		merged := make(map[string]string, len(props))
		maps.Copy(merged, props)
		out[ns] = merged
	}
	for ns, props := range extra {
		merged, ok := out[ns]
		if !ok {
			merged = make(map[string]string, len(props))
			out[ns] = merged
		}
		maps.Copy(merged, props)
	}
	return out
}

// xmpPredefinedPropertyCatalog returns the predefined property catalog for
// the given PDF/A part: XMP 2004 for part 1, XMP 2005 for parts 2 and 3.
// Other parts return nil: ISO 19005-4 drops the extension-schema
// requirements of parts 1-3, so PDF/A-4 has no predefined-property rule
// to enforce (see [xmpSchemaRule]).
func xmpPredefinedPropertyCatalog(part int) map[string]map[string]string {
	switch part {
	case 1:
		return xmpPredefinedXMP2004
	case 2, 3:
		return xmpPredefinedXMP2005
	}
	return nil
}

// XMPPredefinedSchemaNamespace reports whether ns is a predefined XMP
// schema namespace with a known property catalog for the given PDF/A part
// (1 → XMP 2004, 2/3 → XMP 2005). Parts outside 1-3 have no catalog and
// always return false.
func XMPPredefinedSchemaNamespace(part int, ns string) bool {
	catalog := xmpPredefinedPropertyCatalog(part)
	if catalog == nil {
		return false
	}
	_, ok := catalog[ns]
	return ok
}

// XMPPredefinedProperty reports whether the XMP Specification variant for
// the given PDF/A part predefines the property ns:name. A property in a
// predefined namespace for which this returns false needs a PDF/A
// extension schema declaration (ISO 19005-1 §6.7.9, ISO 19005-2/3
// §6.6.2.3.1).
func XMPPredefinedProperty(part int, ns, name string) bool {
	catalog := xmpPredefinedPropertyCatalog(part)
	if catalog == nil {
		return false
	}
	props, ok := catalog[ns]
	if !ok {
		return false
	}
	_, ok = props[name]
	return ok
}
