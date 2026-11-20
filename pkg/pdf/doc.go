// Package pdf provides a high-level API for reading, extracting data from,
// and producing PDF documents.
//
// litho is a pure-Go implementation of ISO 32000 (PDF 1.4 - 2.0) with no
// C dependencies. The package groups its functionality into four types:
//
//   - [Reader]: open an existing PDF, query metadata, extract text and
//     other content. See [Open], [OpenFile].
//   - [Writer]: queue mutations (rotate pages, stamp, encrypt, sign, ...)
//     against a Reader and emit a new PDF via [Writer.WriteTo].
//   - [Pipeline]: chain Convert, Split, Merge, Apply and WriteTo
//     operations with managed intermediate storage. See [NewPipeline].
//   - [Conversion]: lay a text, HTML, Markdown, Office or image source out
//     as a new PDF. See [FromHTML] and the other From constructors.
//
// Specialised PDF 2.0 extractors sit on [Reader] alongside the core
// text, image, and annotation methods: [Reader.ExtractGeoData],
// [Reader.ExtractMeasurements], [Reader.Extract3DModels],
// [Reader.ExtractPronunciationLexicons], [Reader.ExtractFormulas],
// [Reader.ExtractRedactedText], and [Reader.ExtractColorSpaces].
//
// Readers that draw the page to judge its text sit beside them:
// [Reader.ExtractUnseenText] lists the text a page draws that shows nowhere
// on it, and [Reader.CheckTextContrast] the text short of the contrast WCAG
// 2.x asks of it, an advisory check no PDF/UA validation depends on.
//
// A [Conversion] makes a new, tagged PDF from another format: plain text,
// Markdown, HTML with its CSS, the Office and OpenDocument text,
// spreadsheet and presentation formats, and image files ([FromImage]).
// One constructor per format makes it, [FromHTML] or [FromDOCX] among
// them, and takes the options of that format alone. [Conversion.WriteTo]
// runs it; [Pipeline.Convert] opens a pipeline on its output, where a
// later stage remediates it to a PDF/A or PDF/UA profile, encrypts it or
// signs it; [Pipeline.MergePipelines] runs several pipelines at once and
// merges what they write.
//
// # Quick start
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"log"
//		"os"
//
//		"github.com/gotenberg/litho/pkg/pdf"
//	)
//
//	func main() {
//		ctx := context.Background()
//
//		// Read
//		reader, err := pdf.OpenFile(ctx, "input.pdf")
//		if err != nil {
//			log.Fatal(err)
//		}
//		defer reader.Close()
//
//		text, _ := reader.ExtractText(ctx, 1)
//		fmt.Println(text)
//
//		// Mutate and write
//		writer := pdf.NewWriter(reader)
//		defer writer.Close()
//		writer.SetMetadata(pdf.Metadata{Title: "Redacted copy"})
//		if err := writer.RotatePages(90, 1); err != nil {
//			log.Fatal(err)
//		}
//
//		out, err := os.Create("output.pdf")
//		if err != nil {
//			log.Fatal(err)
//		}
//		defer out.Close()
//
//		if err := writer.WriteTo(ctx, out); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// # PDF/A, PDF/UA and WTPDF
//
// Use [Reader.ValidatePDFA], [Reader.ValidatePDFUA] or [Reader.ValidateWTPDF]
// to report compliance against a profile, and [Writer.RemediatePDFA],
// [Writer.RemediatePDFUA] or [Writer.RemediateWTPDF] to queue automatic
// repairs for the violations the engine can fix. Well-Tagged PDF 1.0 has two
// levels, [models.WTPDF1Accessibility] and [models.WTPDF1Reuse]. Both adopt
// the ISO 14289-2 (PDF/UA-2) requirements, so a WTPDF run reports PDF/UA-2
// clauses, without the rules the level does not state, plus the PDF
// Declaration the level requires, which a WTPDF remediation writes. A
// remediated Writer holds its output in a temp file until [Writer.WriteTo]
// streams it or [Writer.Close] removes it.
//
// [WithPDFUA] makes a PDF/A remediation, a Factur-X one included, target a
// PDF/UA profile too, so that the output conforms to both: PDF/A-1, PDF/A-2
// and PDF/A-3 pair with PDF/UA-1, PDF/A-4 with PDF/UA-2, and any other pair
// is refused with [ErrIncompatibleProfiles].
//
// A remediation is the last edit on a Writer. Queue every other edit first:
// the remediation judges and repairs the document they produce, so its
// [github.com/gotenberg/litho/pkg/models.RemediationReport] describes the
// output. After it, only [Writer.Sign], [Writer.Timestamp], [Writer.WriteTo]
// and [Writer.Close] are accepted, and every other call, a second remediation
// included, is refused with [ErrEditAfterRemediation]. The output of a PDF/A
// remediation is never encrypted, since ISO 19005 forbids it:
// [ErrPDFAForbidsEncryption]. That of a PDF/UA-1 remediation is encrypted only
// with permissions that allow extraction for accessibility, which ISO 14289-1,
// 7.16 requires: [ErrPDFUAForbidsBarringAccessibility]. That of a PDF/A-1
// remediation carries no cross-reference stream, which ISO 19005-1, 6.1.4
// forbids, and so no object stream: [ErrPDFA1ForbidsObjectStreams].
//
// # Digital signatures
//
// [Reader.ExtractSignatures] and [Reader.VerifySignatures] inspect existing
// signatures. [Writer.Sign] queues a new signature; [Writer.WriteTo] produces a
// signed incremental update, on top of the full rewrite when the output is
// one. An encrypted source, or an encrypted output, is not signed:
// [ErrSignFullRewriteIncompatible].
//
// [NewSigner], [ParseSignerPEM] and [ParseSignerPKCS12] (a .p12 or .pfx key
// store) return a [github.com/gotenberg/litho/pkg/plugins.Signer] that signs
// with an X.509 certificate and an RSA or ECDSA key: a detached CMS SignedData
// over SHA-256 with the signed attributes PAdES asks for. [WithPAdES] makes it
// a PAdES baseline signature (ETSI.CAdES.detached), and
// [WithTimestampAuthority] adds an RFC 3161 signature timestamp. Implement
// [github.com/gotenberg/litho/pkg/plugins.Signer] for anything else. [WithLTV]
// adds the signature's long-term validation data to the document security
// store, and [Writer.Timestamp] an RFC 3161 document timestamp, after the
// signature and its validation data when all are asked for.
//
// A signature covers the bytes of the file it was made in (ISO 32000-1,
// 12.8.1). An incremental update appends to them and keeps every signature. A
// full rewrite, a [Split] and a [Merge] move them, so they leave the input's
// signatures out: each signed widget's appearance is drawn into its page, and
// the field leaves the form. [Writer.DroppedSignatures],
// [github.com/gotenberg/litho/pkg/models.PipelineResult.DroppedSignatures] and
// [WithDroppedSignatures] report what an output left out.
//
// # Functional options
//
// Each entry point takes its own option type, so the compiler prevents
// passing an option to the wrong call:
//
//   - [Open] / [OpenFile] take [OpenOption]: [WithLogger], [WithPassword],
//     [WithOCREngine], [WithFilterPlugin], [WithCIISchemas],
//     [WithSchematron], [WithLenient], [WithDefaultCMYKProfile],
//     [WithLimits], [WithStrictStreams].
//   - [Reader.ExtractText], [Reader.ExtractAllText] and
//     [Reader.ExtractMarkdown] take [ExtractTextOption]: [WithHiddenText],
//     [WithInvisibleText], [WithClipPathText], [WithClippedText],
//     [WithArtifacts], [WithAnnotationText].
//   - [Reader.RenderPage], [Reader.RenderPageReport] and
//     [WithRedactionGlyphOutlines] take [RenderOption]: [WithRenderDPI],
//     [WithRenderBackground],
//     [WithRenderFontProvider], [WithRenderFontFiles],
//     [WithRenderSystemFonts], [WithRenderTarget], [WithRenderContent],
//     [WithRenderBox], [WithRenderColorModel], [WithRenderCMYKProfile],
//     [WithOverprintSimulation], [WithRenderRedactionReading].
//   - [Reader.CheckTextContrast] takes [ContrastOption]:
//     [WithEnhancedContrast].
//   - [Writer.WriteTo], [Split], [Merge], [Compose], [Pipeline.WriteTo],
//     [Pipeline.Compose], [Pipeline.Split], [Pipeline.Merge] and
//     [Pipeline.MergePipelines] take [WriteOption]:
//     [WithFullRewrite], [WithVersion], [WithXRefStream], [WithWriteLogger],
//     [WithBookmarks], [WithPipelineTempDir], [WithPipelineConcurrency],
//     [WithTempDir],
//     [WithEncryption], [WithObjectStreams], [WithDedup],
//     [WithImageRecompression], [WithFontSubsetting], [WithImageQuality],
//     [WithMaxImageDPI], [WithCompactionSkipped], [WithDroppedSignatures],
//     [WithDefaultPageSize]. [NewWriter] takes them
//     too, as defaults for the Writer's WriteTo, which overrides them with
//     its own, and a [WithDefaultPageSize] given to it also reaches its
//     remediation. [WithTempDir] places the temp files of [NewWriter],
//     [Writer.WriteTo] and [Pipeline.WriteTo], and [Split], [Merge] and
//     [Compose] ignore it. [Pipeline.Split], [Pipeline.Merge] and
//     [Pipeline.MergePipelines] honour [WithBookmarks] on their stage.
//     [WithPipelineConcurrency] reaches [Pipeline.WriteTo] alone. [Compose] ignores [WithFullRewrite],
//     [WithBookmarks] and [WithDefaultPageSize] (see its documentation).
//   - [Writer.RemediatePDFA], [Writer.RemediatePDFUA],
//     [Writer.RemediateWTPDF], [Writer.RemediateFacturX] and
//     [Writer.RepairFacturX] take [RemediateOption]: [WithLanguage],
//     [WithAggressiveFixes], [WithRegressionTolerance], [WithFigureAltText],
//     [WithDocumentID], [WithFontProvider], [WithFontFiles],
//     [WithSystemFonts], [WithWholeFontPrograms], and, for
//     [Writer.RemediatePDFA], [Writer.RemediateFacturX] and
//     [Writer.RepairFacturX], [WithPDFUA].
//   - [Writer.Sign] takes [SignOption]: [WithSignatureReason],
//     [WithSignatureLocation], [WithSignatureContactInfo], [WithSignerName],
//     [WithSigningTime], [WithSignatureField], [WithPAdES],
//     [WithTimestampAuthority], [WithLTV], [WithValidationData].
//   - [Writer.Timestamp] takes [TimestampOption]: [WithTimestampField].
//   - [Writer.Redact] takes [RedactOption]: [WithStrictRedaction],
//     [WithRedactionRasterFallback], [WithoutRedactionAnnotationClipping],
//     [WithRedactionJPEGQuality], [WithRedactionGlyphOutlines],
//     [WithoutRedactionGlyphOutlines], [WithoutRedactionPixelCheck], and the
//     deprecated no-ops [WithRedactionAnnotationClipping] and
//     [WithRedactionPixelCheck].
//   - [Writer.ConvertColors] takes [ColorOption]: [WithColorIntent],
//     [WithoutBlackPointCompensation], [WithSpotConversion],
//     [WithKOnlyBlack], [WithoutOutputIntent], [WithReplacedOutputIntents],
//     [WithJPEGQuality], and the deprecated no-op
//     [WithBlackPointCompensation].
//     Its [ColorTarget] is one of
//     [OutputIntentColorTarget], [CMYKColorTarget], [RGBColorTarget],
//     [GrayColorTarget] and [ICCColorTarget].
//   - [Writer.FlattenTransparency] takes [FlattenOption]: [WithFlattenDPI],
//     [WithFlattenPrintedAnnotations], [WithFlattenJPEGQuality],
//     [WithFlattenOverprintSimulation].
//   - [Writer.FlattenForm] takes [FlattenFormOption]: [WithFlattenFonts].
//   - [Writer.SetFormField] and [Writer.SetFormFieldValues] take
//     [FormFieldOption]: [WithFieldFonts].
//   - [Pipeline.Apply] takes [ApplyOption]: [WithEditLabel].
//   - Every conversion takes [ConvertOption]: [WithPaper],
//     [WithDefaultPaper], [WithOrientation], [WithPageRanges],
//     [WithOutline], [WithColorGlyphs], [WithDerivedAltText], [WithDate],
//     [WithDefaultLanguage], [WithConvertVersion], [WithConvertFontDirs],
//     [WithConvertLimits], [WithConvertLogger], [WithConvertTempDir].
//   - A conversion of text, HTML or Markdown takes [MarginOption]:
//     [WithMargins].
//   - [FromText] takes [TextOption]: [WithFontFamilies], [WithFontSize],
//     [WithLineHeight], [WithTabWidth].
//   - [FromHTML] takes [HTMLOption], and so does [FromMarkdown]:
//     [WithResources], [WithBaseURL], [WithHTTPClient], [WithMediaType],
//     [WithMediaFeatures], [WithScale], [WithShrinkToFit],
//     [WithCanvasBackground], [WithFinishedAnimations], [WithHeader],
//     [WithFooter], [WithFirstPageNumber], [WithFractionalFontMetrics],
//     [WithUserStylesheet], [WithRequiredResources].
//   - [FromHTML], [FromMarkdown], [FromDOCX] and [FromODT] take
//     [SinglePageOption]: [WithSinglePage].
//   - [FromMarkdown] takes [MarkdownOption]: [WithMarkdownTemplate].
//   - A conversion of an Office package takes [OfficeOption]:
//     [WithOfficePassword].
//   - [FromDOCX] and [FromODT] take [WordProcessingOption]:
//     [WithTrackedChanges], [WithComments], [WithFormFields].
//   - A conversion of sheets takes [SheetOption]: [WithOnePagePerSheet].
//   - [FromXLSX] and [FromODS] take [SpreadsheetOption]: [WithSheets],
//     [WithSheetNumbers], [WithHiddenSheets], [WithFormulaRecalculation].
//   - [FromCSV] takes [CSVOption]: [WithCSVSeparators],
//     [WithCSVTypeDetection].
//   - [FromPPTX] and [FromODP] take [PresentationOption]:
//     [WithHiddenSlides], [WithNotesPages], [WithHandouts].
//   - [FromImage] takes [ImageOption]: the [ConvertOption] values, and no
//     option of its own.
//
// [WithFullRewrite], [WithVersion], and [WithXRefStream] are advanced
// serialization controls. The defaults suit most callers:
// content-modifying operations escalate to a full rewrite on their own.
//
// [WithEncryption] is how an output becomes encrypted. No operation preserves
// a source's encryption, so an edited document is written in the clear unless
// this option asks otherwise. [WithObjectStreams], [WithDedup],
// [WithImageRecompression] and [WithFontSubsetting] shrink a full rewrite,
// losslessly, and the first three decline on an encrypted output.
// [WithImageQuality] and [WithMaxImageDPI] shrink it further by re-encoding
// images as JPEG and downsampling them, which changes their pixels.
// [WithCompactionSkipped] reports a write that applied none of them because
// it appended an incremental update. [WithImageRecompression] and the two
// lossy options cost far more time than the other two, and
// [WithFontSubsetting] leaves later edits no glyph the document does not
// show: ask for each on its own.
//
// # Resource limits
//
// A crafted document can ask for far more memory or work than its size
// suggests, so every [Reader] runs under six budgets: the decoded bytes of
// one stream, the pixels of one image, the size of an embedded PDF validation
// opens, the nesting of form XObjects, the entries of the object cache, and
// the pixels of one rendered page. A [Conversion] runs under three more:
// the bytes of its source, the pages it lays out and the memory it holds.
// [DefaultLimits] suits ordinary documents. A service that reads untrusted
// input can lower them, and one that handles unusually large images can raise
// them, through [WithLimits]:
//
//	reader, err := pdf.OpenFile(ctx, "scan.pdf", pdf.WithLimits(models.Limits{
//		MaxStreamBytes: 64 << 20,
//		MaxImagePixels: 25_000_000,
//	}))
//
// A stream or an image past its budget fails with [ErrStreamTooLarge] or
// [ErrImageTooLarge], and a page whose content passes a quarter of the stream
// budget with [ErrPageContentTooLarge]; see
// [github.com/gotenberg/litho/pkg/models.Limits] for what each budget bounds.
// A budget below zero or past its ceiling makes [Open] fail with
// [ErrInvalidLimits], and [ValidateLimits] reports it before any document
// opens, for budgets read from configuration.
//
// # Embedding files
//
// [Writer.AddAttachment] embeds a generic file in the /EmbeddedFiles name
// tree. For PDF/A-3, Factur-X, or ZUGFeRD, an embedded file must instead
// carry an /AFRelationship and appear in the catalog /AF array: use
// [Writer.AddAssociatedFile], or [Writer.RemediateFacturX] for the full
// hybrid-invoice setup. Using AddAttachment where an associated file is
// required yields a structurally valid PDF that still fails PDF/A-3 and
// Factur-X validation.
//
// # Acknowledgments
//
// litho's text extraction pipeline, CMap parser, and font resolution logic
// were heavily inspired by Mozilla's PDF.js
// (https://github.com/mozilla/pdf.js). The PDF/A, PDF/UA and WTPDF
// validation rule set and clause numbering draw extensively from veraPDF
// (https://github.com/veraPDF). Grateful acknowledgment to both
// communities for their pioneering open-source work on PDF parsing
// and conformance.
package pdf
