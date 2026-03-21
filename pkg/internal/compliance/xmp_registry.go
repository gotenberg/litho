package compliance

// xmpType represents the expected value type for an XMP property.
type xmpType int

const (
	// Simple value types — stored as text content or attributes.
	xmpText    xmpType = iota // Plain text string.
	xmpURI                    // URI string.
	xmpInteger                // Integer value.
	xmpReal                   // Real/float value.
	xmpBoolean                // Boolean (True/False).
	xmpDate                   // ISO 8601 date.

	// Container types — wrapped in rdf:Alt, rdf:Seq, or rdf:Bag.
	xmpAlt    // rdf:Alt (language alternatives).
	xmpSeq    // rdf:Seq (ordered list).
	xmpBag    // rdf:Bag (unordered set).
	xmpStruct // Structured value (nested properties).

	// xmpAny matches any type (for properties whose type is flexible).
	xmpAny
)

// xmpPropertyRegistry maps predefined XMP namespace URIs to the set of valid
// property names and their expected types. Properties not listed here require
// a PDF/A extension schema declaration.
//
// Sources: XMP Specification Parts 1-3, EXIF/XMP mapping, Adobe Camera Raw
// schema, ISO 19005 (PDF/A identification).
var xmpPropertyRegistry = map[string]map[string]xmpType{
	// Dublin Core (dc:).
	"http://purl.org/dc/elements/1.1/": {
		"contributor": xmpBag, "coverage": xmpText, "creator": xmpSeq,
		"date": xmpSeq, "description": xmpAlt, "format": xmpText,
		"identifier": xmpText, "language": xmpBag, "publisher": xmpBag,
		"relation": xmpBag, "rights": xmpAlt, "source": xmpText,
		"subject": xmpBag, "title": xmpAlt, "type": xmpBag,
	},

	// XMP Basic (xmp:).
	"http://ns.adobe.com/xap/1.0/": {
		"Advisory":     xmpBag,
		"BaseURL":      xmpURI,
		"CreateDate":   xmpDate,
		"CreatorTool":  xmpText,
		"Identifier":   xmpBag,
		"Label":        xmpText,
		"MetadataDate": xmpDate,
		"ModifyDate":   xmpDate,
		"Nickname":     xmpText,
		"Rating":       xmpReal,
		"Thumbnails":   xmpAlt,
	},

	// XMP Media Management (xmpMM:).
	"http://ns.adobe.com/xap/1.0/mm/": {
		"DerivedFrom":        xmpStruct,
		"DocumentID":         xmpURI,
		"History":            xmpSeq,
		"InstanceID":         xmpURI,
		"Ingredients":        xmpBag,
		"LastURL":            xmpURI,
		"ManagedFrom":        xmpStruct,
		"Manager":            xmpText,
		"ManageUI":           xmpURI,
		"ManageTo":           xmpURI,
		"ManagerVariant":     xmpText,
		"OriginalDocumentID": xmpURI,
		"Pantry":             xmpBag,
		"RenditionClass":     xmpText,
		"RenditionOf":        xmpStruct,
		"RenditionParams":    xmpText,
		"SaveID":             xmpInteger,
		"Subject":            xmpBag,
		"VersionID":          xmpText,
		"Versions":           xmpSeq,
	},

	// XMP Rights (xmpRights:).
	"http://ns.adobe.com/xap/1.0/rights/": {
		"Certificate":  xmpURI,
		"Marked":       xmpBoolean,
		"Owner":        xmpBag,
		"UsageTerms":   xmpAlt,
		"WebStatement": xmpURI,
	},

	// PDF (pdf:).
	"http://ns.adobe.com/pdf/1.3/": {
		"Keywords":   xmpText,
		"PDFVersion": xmpText,
		"Producer":   xmpText,
		"Trapped":    xmpText,
	},

	// PDF/A Identification (pdfaid:).
	"http://www.aiim.org/pdfa/ns/id/": {
		"amd":         xmpText,
		"conformance": xmpText,
		"part":        xmpInteger,
		"corr":        xmpText,
		"rev":         xmpText,
	},

	// Photoshop (photoshop:).
	"http://ns.adobe.com/photoshop/1.0/": {
		"AuthorsPosition":        xmpText,
		"CaptionWriter":          xmpText,
		"Category":               xmpText,
		"City":                   xmpText,
		"ColorMode":              xmpInteger,
		"Country":                xmpText,
		"Credit":                 xmpText,
		"DateCreated":            xmpDate,
		"DocumentAncestors":      xmpBag,
		"Headline":               xmpText,
		"History":                xmpText,
		"ICCProfile":             xmpText,
		"Instructions":           xmpText,
		"LabelColor":             xmpText,
		"Source":                 xmpText,
		"State":                  xmpText,
		"SupplementalCategories": xmpBag,
		"TextLayers":             xmpSeq,
		"TransmissionReference":  xmpText,
		"Urgency":                xmpInteger,
		"SidecarForExtension":    xmpText,
		"EmbeddedXMPDigest":      xmpText,
		"AncestorLink":           xmpText,
		"LegacyIPTCDigest":       xmpText,
	},

	// EXIF (exif:).
	"http://ns.adobe.com/exif/1.0/": {
		"ApertureValue":            xmpReal,
		"BrightnessValue":          xmpReal,
		"CFAPattern":               xmpStruct,
		"ColorSpace":               xmpInteger,
		"CompressedBitsPerPixel":   xmpReal,
		"ComponentsConfiguration":  xmpSeq,
		"Contrast":                 xmpInteger,
		"CustomRendered":           xmpInteger,
		"DateTimeDigitized":        xmpDate,
		"DateTimeOriginal":         xmpDate,
		"DeviceSettingDescription": xmpStruct,
		"DigitalZoomRatio":         xmpReal,
		"ExifVersion":              xmpText,
		"ExposureBiasValue":        xmpReal,
		"ExposureIndex":            xmpReal,
		"ExposureMode":             xmpInteger,
		"ExposureProgram":          xmpInteger,
		"ExposureTime":             xmpReal,
		"FileSource":               xmpInteger,
		"Flash":                    xmpStruct,
		"FlashEnergy":              xmpReal,
		"FlashpixVersion":          xmpText,
		"FNumber":                  xmpReal,
		"FocalLength":              xmpReal,
		"FocalLengthIn35mmFilm":    xmpInteger,
		"FocalPlaneResolutionUnit": xmpInteger,
		"FocalPlaneXResolution":    xmpReal,
		"FocalPlaneYResolution":    xmpReal,
		"GainControl":              xmpInteger,
		"GPSAltitude":              xmpReal,
		"GPSAltitudeRef":           xmpInteger,
		"GPSAreaInformation":       xmpText,
		"GPSDestBearing":           xmpReal,
		"GPSDestBearingRef":        xmpText,
		"GPSDestDistance":          xmpReal,
		"GPSDestDistanceRef":       xmpText,
		"GPSDestLatitude":          xmpText,
		"GPSDestLongitude":         xmpText,
		"GPSDifferential":          xmpInteger,
		"GPSDOP":                   xmpReal,
		"GPSImgDirection":          xmpReal,
		"GPSImgDirectionRef":       xmpText,
		"GPSLatitude":              xmpText,
		"GPSLongitude":             xmpText,
		"GPSMapDatum":              xmpText,
		"GPSMeasureMode":           xmpText,
		"GPSProcessingMethod":      xmpText,
		"GPSSatellites":            xmpText,
		"GPSSpeed":                 xmpReal,
		"GPSSpeedRef":              xmpText,
		"GPSStatus":                xmpText,
		"GPSTimeStamp":             xmpDate,
		"GPSTrack":                 xmpReal,
		"GPSTrackRef":              xmpText,
		"GPSVersionID":             xmpText,
		"ImageUniqueID":            xmpText,
		"ISOSpeedRatings":          xmpSeq,
		"LightSource":              xmpInteger,
		"MakerNote":                xmpText,
		"MaxApertureValue":         xmpReal,
		"MeteringMode":             xmpInteger,
		"NativeDigest":             xmpText,
		"OECF":                     xmpStruct,
		"PixelXDimension":          xmpInteger,
		"PixelYDimension":          xmpInteger,
		"RelatedSoundFile":         xmpText,
		"Saturation":               xmpInteger,
		"SceneCaptureType":         xmpInteger,
		"SceneType":                xmpInteger,
		"SensingMethod":            xmpInteger,
		"Sharpness":                xmpInteger,
		"ShutterSpeedValue":        xmpReal,
		"SpatialFrequencyResponse": xmpStruct,
		"SpectralSensitivity":      xmpText,
		"SubjectArea":              xmpSeq,
		"SubjectDistance":          xmpReal,
		"SubjectDistanceRange":     xmpInteger,
		"SubjectLocation":          xmpSeq,
		"UserComment":              xmpAlt,
		"WhiteBalance":             xmpInteger,
		// Additional EXIF 2.3+ properties.
		"Gamma":                     xmpReal,
		"PhotographicSensitivity":   xmpInteger,
		"SensitivityType":           xmpInteger,
		"StandardOutputSensitivity": xmpInteger,
		"RecommendedExposureIndex":  xmpInteger,
		"ISOSpeed":                  xmpInteger,
		"ISOSpeedLatitudeyyy":       xmpInteger,
		"ISOSpeedLatitudezzz":       xmpInteger,
		"CameraOwnerName":           xmpText,
		"BodySerialNumber":          xmpText,
		"LensInfo":                  xmpSeq,
		"LensMake":                  xmpText,
		"LensModel":                 xmpText,
		"LensSerialNumber":          xmpText,
	},

	// EXIF Auxiliary (aux:).
	"http://ns.adobe.com/exif/1.0/aux/": {
		"Firmware":                 xmpText,
		"FlashCompensation":        xmpReal,
		"ImageNumber":              xmpInteger,
		"Lens":                     xmpText,
		"LensID":                   xmpInteger,
		"LensInfo":                 xmpSeq,
		"OwnerName":                xmpText,
		"SerialNumber":             xmpText,
		"ApproximateFocusDistance": xmpReal,
	},

	// TIFF (tiff:).
	"http://ns.adobe.com/tiff/1.0/": {
		"Artist":                    xmpText,
		"BitsPerSample":             xmpSeq,
		"Compression":               xmpInteger,
		"Copyright":                 xmpAlt,
		"DateTime":                  xmpDate,
		"ImageDescription":          xmpAlt,
		"ImageLength":               xmpInteger,
		"ImageWidth":                xmpInteger,
		"Make":                      xmpText,
		"Model":                     xmpText,
		"NativeDigest":              xmpText,
		"Orientation":               xmpInteger,
		"PhotometricInterpretation": xmpInteger,
		"PlanarConfiguration":       xmpInteger,
		"PrimaryChromaticities":     xmpSeq,
		"ReferenceBlackWhite":       xmpSeq,
		"ResolutionUnit":            xmpInteger,
		"SamplesPerPixel":           xmpInteger,
		"Software":                  xmpText,
		"TransferFunction":          xmpSeq,
		"WhitePoint":                xmpSeq,
		"XResolution":               xmpReal,
		"YCbCrCoefficients":         xmpSeq,
		"YCbCrPositioning":          xmpInteger,
		"YCbCrSubSampling":          xmpSeq,
		"YResolution":               xmpReal,
	},

	// Camera Raw Settings (crs:).
	"http://ns.adobe.com/camera-raw-settings/1.0/": {
		"AutoBrightness": xmpBoolean, "AutoContrast": xmpBoolean,
		"AutoExposure": xmpBoolean, "AutoShadows": xmpBoolean,
		"BlueHue": xmpInteger, "BlueSaturation": xmpInteger,
		"Brightness": xmpInteger, "CameraProfile": xmpText,
		"ChromaticAberrationB": xmpInteger, "ChromaticAberrationR": xmpInteger,
		"ColorNoiseReduction": xmpInteger, "Contrast": xmpInteger,
		"CropAngle": xmpReal, "CropBottom": xmpReal,
		"CropHeight": xmpReal, "CropLeft": xmpReal,
		"CropRight": xmpReal, "CropTop": xmpReal,
		"CropUnits": xmpInteger, "CropWidth": xmpReal,
		"Exposure": xmpReal, "GreenHue": xmpInteger,
		"GreenSaturation": xmpInteger, "HasCrop": xmpBoolean,
		"HasSettings": xmpText, "LuminanceSmoothing": xmpInteger,
		"RawFileName": xmpText, "RedHue": xmpInteger,
		"RedSaturation": xmpInteger, "Saturation": xmpInteger,
		"Shadows": xmpInteger, "ShadowTint": xmpInteger,
		"Sharpness": xmpInteger, "Temperature": xmpInteger,
		"Tint": xmpInteger, "ToneCurve": xmpSeq,
		"ToneCurveName": xmpText, "Version": xmpText,
		"VignetteAmount": xmpInteger, "VignetteMidpoint": xmpInteger,
		"WhiteBalance": xmpText,
		// Additional CRS properties.
		"Exposure2012": xmpReal, "Contrast2012": xmpInteger,
		"Highlights2012": xmpInteger, "Shadows2012": xmpInteger,
		"Whites2012": xmpInteger, "Blacks2012": xmpInteger,
		"Clarity2012": xmpInteger, "Vibrance": xmpInteger,
		"Defringe": xmpInteger, "Dehaze": xmpInteger,
		"LensProfileEnable": xmpInteger, "LensManualDistortionAmount": xmpInteger,
		"PerspectiveVertical": xmpInteger, "PerspectiveHorizontal": xmpInteger,
		"PerspectiveRotate": xmpReal, "PerspectiveScale": xmpInteger,
		"PostCropVignetteAmount": xmpInteger, "GrainAmount": xmpInteger,
		"SplitToningHighlightHue": xmpInteger, "SplitToningHighlightSaturation": xmpInteger,
		"SplitToningShadowHue": xmpInteger, "SplitToningShadowSaturation": xmpInteger,
		"SplitToningBalance": xmpInteger,
		"ParametricShadows":  xmpInteger, "ParametricDarks": xmpInteger,
		"ParametricLights": xmpInteger, "ParametricHighlights": xmpInteger,
		"ParametricShadowSplit": xmpInteger, "ParametricMidtoneSplit": xmpInteger,
		"ParametricHighlightSplit": xmpInteger,
		"SharpenRadius":            xmpReal, "SharpenDetail": xmpInteger,
		"SharpenEdgeMasking":        xmpInteger,
		"ColorNoiseReductionDetail": xmpInteger, "ColorNoiseReductionSmoothness": xmpInteger,
		"LuminanceNoiseReductionDetail": xmpInteger, "LuminanceNoiseReductionContrast": xmpInteger,
		"ConvertToGrayscale": xmpBoolean, "CameraProfileDigest": xmpText,
		"AlreadyApplied": xmpBoolean, "ProcessVersion": xmpText,
	},

	// XMP Dynamic Media (xmpDM:).
	"http://ns.adobe.com/xmp/1.0/DynamicMedia/": {
		"absPeakAudioFilePath": xmpURI, "album": xmpText,
		"altTapeName": xmpText, "altTimecode": xmpStruct,
		"artist": xmpText, "audioChannelType": xmpText,
		"audioCompressor": xmpText, "audioModDate": xmpDate,
		"audioSampleRate": xmpInteger, "audioSampleType": xmpText,
		"beatSpliceParams": xmpStruct, "cameraAngle": xmpText,
		"cameraLabel": xmpText, "cameraModel": xmpText,
		"cameraMove": xmpText, "client": xmpText,
		"comment": xmpText, "composer": xmpText,
		"contributedMedia": xmpBag, "copyright": xmpText,
		"director": xmpText, "directorPhotography": xmpText,
		"discNumber": xmpText, "duration": xmpStruct,
		"engineer": xmpText, "fileDataRate": xmpReal,
		"genre": xmpText, "good": xmpBoolean,
		"instrument": xmpText, "introTime": xmpStruct,
		"key": xmpText, "logComment": xmpText,
		"loop": xmpBoolean, "lyrics": xmpText,
		"markers": xmpSeq, "metadataModDate": xmpDate,
		"numberOfBeats": xmpReal, "outCue": xmpStruct,
		"partOfCompilation": xmpBoolean, "projectName": xmpText,
		"projectRef": xmpStruct, "pullDown": xmpText,
		"relativePeakAudioFilePath": xmpURI,
		"relativeTimestamp":         xmpStruct, "releaseDate": xmpDate,
		"resampleParams": xmpStruct, "scaleType": xmpText,
		"scene": xmpText, "shotDate": xmpText,
		"shotDay": xmpText, "shotLocation": xmpText,
		"shotName": xmpText, "shotNumber": xmpText,
		"shotSize": xmpText, "speakerPlacement": xmpText,
		"startTimecode": xmpStruct, "stretchMode": xmpText,
		"takeNumber": xmpInteger, "tapeName": xmpText,
		"tempo": xmpReal, "timeScaleParams": xmpStruct,
		"timeSignature": xmpText, "trackNumber": xmpInteger,
		"Tracks": xmpBag, "videoAlphaMode": xmpText,
		"videoAlphaPremultipleColor":   xmpStruct,
		"videoAlphaUnityIsTransparent": xmpBoolean,
		"videoColorSpace":              xmpText, "videoCompressor": xmpText,
		"videoFieldOrder": xmpText, "videoFrameRate": xmpReal,
		"videoFrameSize": xmpStruct, "videoModDate": xmpDate,
		"videoPixelAspectRatio": xmpReal, "videoPixelDepth": xmpText,
	},

	// XMP Paged-Text (xmpTPg:).
	"http://ns.adobe.com/xap/1.0/t/pg/": {
		"MaxPageSize":            xmpStruct,
		"NPages":                 xmpInteger,
		"Fonts":                  xmpBag,
		"Colorants":              xmpSeq,
		"PlateNames":             xmpSeq,
		"SwatchGroups":           xmpSeq,
		"HasVisibleOverprint":    xmpBoolean,
		"HasVisibleTransparency": xmpBoolean,
	},

	// XMP Basic Job Ticket (xmpBJ:).
	"http://ns.adobe.com/xap/1.0/bj/": {
		"JobRef": xmpBag,
	},
}

// isSimpleType returns true if the type is a simple value (not a container).
func isSimpleType(t xmpType) bool {
	return t <= xmpDate
}

// isContainerType returns true if the type is an RDF container (Alt/Seq/Bag).
func isContainerType(t xmpType) bool {
	return t >= xmpAlt && t <= xmpBag
}
