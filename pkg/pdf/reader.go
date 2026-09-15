package pdf

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/annotation"
	"github.com/gotenberg/litho/pkg/internal/bookmarks"
	"github.com/gotenberg/litho/pkg/internal/compliance"
	cii_schemas "github.com/gotenberg/litho/pkg/internal/compliance/cii_schemas"
	cii_schematron "github.com/gotenberg/litho/pkg/internal/compliance/cii_schematron"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/geospatial"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
	"github.com/gotenberg/litho/pkg/internal/schematron"
	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/internal/semantics/structure"
	"github.com/gotenberg/litho/pkg/internal/signature"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/xsd"
	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/plugins"
)

// OpenOption configures optional behavior when opening a PDF document via [Open] or [OpenFile].
// OpenOption closures must be safe to call from any goroutine.
type OpenOption func(*openOpts)

type openOpts struct {
	logger    *slog.Logger
	password  string
	ocr       models.OCREngine
	filters   []plugins.FilterDecoder
	ciiFS     fs.FS  // nil unless WithCIISchemas was supplied
	ciiRoot   string // schema root path within ciiFS
	ciiSchema *xsd.Schema
	schFS     fs.FS // nil unless WithSchematron was supplied
	schRoot   string
	schPhase  string
	schSchema *schematron.Schema
	lenient   bool // relax Factur-X/ZUGFeRD consumer-path tolerances
}

// WithLogger injects a structured logger for debug-level diagnostic output.
// When set, the parsing engine emits detailed debug logs (font resolution,
// object resolution, text extraction, etc.) using structured fields.
// If not set, all debug output is silently discarded.
func WithLogger(logger *slog.Logger) OpenOption {
	return func(o *openOpts) {
		o.logger = logger
	}
}

// WithPassword supplies the password for an encrypted PDF document.
// If the document is not encrypted, the password is ignored.
func WithPassword(password string) OpenOption {
	return func(o *openOpts) {
		o.password = password
	}
}

// WithOCREngine injects an OCR backend (e.g., Tesseract, Google Cloud Vision).
// When set, [Reader.ExtractText] automatically hands off scanned image pages to the
// OCR engine instead of returning [ErrRequiresOCR].
func WithOCREngine(engine models.OCREngine) OpenOption {
	return func(o *openOpts) {
		o.ocr = engine
	}
}

// WithFilterPlugin registers a [plugins.FilterDecoder] plugin for stream
// decompression. Multiple plugins can be registered; each handles the
// filter name returned by its FilterName method.
//
// Example:
//
//	import "github.com/gotenberg/litho/pkg/plugins/jbig2"
//	reader, err := pdf.OpenFile(ctx, "doc.pdf", pdf.WithFilterPlugin(jbig2.New()))
func WithFilterPlugin(p plugins.FilterDecoder) OpenOption {
	return func(o *openOpts) {
		o.filters = append(o.filters, p)
	}
}

// WithCIISchemas loads the supplied Cross-Industry Invoice XSD set
// once at open time and threads it into every subsequent
// [Reader.ValidateFacturX] / [Reader.ValidateZUGFeRD] call. When set,
// the validator runs a full pure-Go XSD pass over the embedded CII
// XML and surfaces violations under the CII_XSD clause alongside the
// existing INVOICE and CII spine rules.
//
// fsys is any [io/fs.FS]: an embed.FS works for bundled schemas, an
// os.DirFS for a path on disk. rootPath is the schema root file
// (typically CrossIndustryInvoice_100pD16B.xsd or similar). Schema
// load errors abort the [pdf.Open] call so misconfiguration surfaces
// at startup rather than at validation time. The actual load runs
// inside [Open] with the caller's context, so a slow filesystem can
// be canceled.
func WithCIISchemas(fsys fs.FS, rootPath string) OpenOption {
	return func(o *openOpts) {
		o.ciiFS = fsys
		o.ciiRoot = rootPath
	}
}

// WithSchematron registers a Schematron schema set against which
// [Reader.ValidateFacturX] and [Reader.ValidateZUGFeRD] run a
// business-rule pass over the embedded CII XML. When phase is
// non-empty, only that phase's active patterns evaluate; pass "" to
// run every pattern. Violations surface as compliance.Violation
// entries under the CII_SCH clause.
//
// fsys is any [io/fs.FS]: an embed.FS for bundled rule sets, an
// os.DirFS for a path on disk. rootPath is the Schematron root file
// (typically an EN16931-CII-validation.sch or XRechnung equivalent).
// Schema load errors abort the [pdf.Open] call so misconfiguration
// surfaces at startup rather than at validation time.
func WithSchematron(fsys fs.FS, rootPath string, phase string) OpenOption {
	return func(o *openOpts) {
		o.schFS = fsys
		o.schRoot = rootPath
		o.schPhase = phase
	}
}

// WithLenient relaxes the Factur-X / ZUGFeRD consumer-path tolerances
// that the specification does not sanction. By default litho validates
// strictly: a tolerated but non-spec /AFRelationship (e.g. Alternative
// carried over from ZUGFeRD 2.0), an application/xml embedded-stream
// subtype, or a non-spec XMP Version surfaces as a [SeverityWarning]
// [Violation] from [Reader.ValidateFacturX] / [Reader.ValidateZUGFeRD]
// (see [FacturXReport.Warnings]). WithLenient accepts those values
// silently, matching the behaviour of producers that carry them over
// from earlier ZUGFeRD generations. It does not relax fatal structural
// rules.
func WithLenient() OpenOption {
	return func(o *openOpts) {
		o.lenient = true
	}
}

// Reader is the high-level API facade for the PDF parsing engine.
// When opened via [OpenFile], the Reader holds a reference to the underlying
// file; call [Reader.Close] to release it.
//
// Reader is safe for concurrent use by multiple goroutines. All extraction
// methods ([Reader.ExtractText], [Reader.Metadata], etc.) may be called
// concurrently on the same Reader. [Reader.Close] and [Reader.ClearCache]
// must not be called concurrently with other Reader methods.
type Reader struct {
	doc         *document.Document
	ocr         models.OCREngine
	closer      io.Closer // non-nil when opened via OpenFile
	pagesOnce   sync.Once
	cachedPages []*document.Page
	pagesErr    error
	ciiSchema   *xsd.Schema        // non-nil when WithCIISchemas was set
	schSchema   *schematron.Schema // non-nil when WithSchematron was set
	schPhase    string             // phase argument to WithSchematron
	lenient     bool               // relax Factur-X consumer-path tolerances (WithLenient)
}

// Close releases any resources held by the [Reader]. When the Reader
// was created via [OpenFile], Close closes the underlying file. For
// Readers created via [Open], Close is a no-op.
func (r *Reader) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// ClearCache releases the in-memory object cache, font cache, and memoized
// page tree. This is useful in batch-processing scenarios where a single
// Reader is used to extract data from many pages sequentially and the caller
// wants to bound memory usage between pages. The caches are transparently
// rebuilt on the next access.
//
// ClearCache must not be called concurrently with other Reader methods.
func (r *Reader) ClearCache() {
	r.pagesOnce = sync.Once{} // zero value is a valid, unlocked Once
	r.cachedPages = nil
	r.pagesErr = nil
	r.doc.ClearCaches()
}

// ValidatePDFA audits the document against a specific PDF/A profile,
// returning all compliance violations found. An empty slice means the
// document passes.
func (r *Reader) ValidatePDFA(ctx context.Context, profile models.PDFAProfile) (_ []error, err error) {
	defer recoverToError(r.logger(), "Reader.ValidatePDFA", &err)
	return compliance.ValidatePDFA(ctx, r.doc, profile)
}

// ValidatePDFUA audits the document against a specific PDF/UA profile,
// returning all compliance violations found. An empty slice means the
// document passes.
func (r *Reader) ValidatePDFUA(ctx context.Context, profile models.PDFUAProfile) (_ []error, err error) {
	defer recoverToError(r.logger(), "Reader.ValidatePDFUA", &err)
	return compliance.ValidatePDFUA(ctx, r.doc, profile)
}

// ValidatePDFAReport audits the document against a PDF/A profile and
// returns structured [Violation] values carrying the clause, test
// number, and severity that [Reader.ValidatePDFA] stringifies away.
// Every PDF/A violation is [SeverityFatal]: ISO 19005 conformance is
// binary. Use [Reader.ValidatePDFA] for the flat error form.
func (r *Reader) ValidatePDFAReport(ctx context.Context, profile models.PDFAProfile) (_ []Violation, err error) {
	defer recoverToError(r.logger(), "Reader.ValidatePDFAReport", &err)
	errs, err := compliance.ValidatePDFA(ctx, r.doc, profile)
	if err != nil {
		return nil, err
	}
	return toViolations(errs), nil
}

// ValidatePDFUAReport mirrors [Reader.ValidatePDFAReport] for PDF/UA.
// Every PDF/UA violation is [SeverityFatal]: ISO 14289 conformance is
// binary.
func (r *Reader) ValidatePDFUAReport(ctx context.Context, profile models.PDFUAProfile) (_ []Violation, err error) {
	defer recoverToError(r.logger(), "Reader.ValidatePDFUAReport", &err)
	errs, err := compliance.ValidatePDFUA(ctx, r.doc, profile)
	if err != nil {
		return nil, err
	}
	return toViolations(errs), nil
}

// ValidateFacturX audits the document against the joint
// Factur-X 1.0+ / ZUGFeRD 2.1+ specification. The returned slice
// contains the PDF/A-3 violations of the archival base plus any
// hybrid-invoice rule violations.
//
// Schematron rules whose XPath the bundled engine could not compile
// or evaluate appear in the slice as advisories wrapping
// [ErrSchematronRuleSkipped]; use [errors.Is] to filter them out or
// [Reader.ValidateFacturXReport] for a pre-partitioned view.
//
// Use [Reader.ValidateZUGFeRD] when the producer claims an earlier
// ZUGFeRD generation (1.0 or 2.0) since those use different filenames
// and XMP namespaces.
func (r *Reader) ValidateFacturX(ctx context.Context) (_ []error, err error) {
	defer recoverToError(r.logger(), "Reader.ValidateFacturX", &err)
	return compliance.ValidateFacturX(ctx, r.doc, r.facturXValidateOpts(ctx)...)
}

// FacturXReport partitions the output of a [Reader.ValidateFacturXReport]
// call into hard violations and Schematron advisories. Violations are
// the PDF/A-3 plus hybrid-invoice rule failures that callers must act
// on; Advisories list Schematron rules whose XPath the bundled engine
// could not evaluate and whose verdict is therefore unknown.
//
// Values are immutable once returned; callers may share a FacturXReport
// across goroutines without synchronisation.
type FacturXReport struct {
	// Violations are hard rule failures: PDF/A-3 base, hybrid-invoice
	// AF/XMP/CII checks, and Schematron assertions that fired.
	Violations []error
	// Advisories list Schematron rules skipped because the bundled
	// XPath engine does not support the construct the rule uses.
	// Each entry wraps [ErrSchematronRuleSkipped] and names the rule
	// id plus the unsupported construct in its error message.
	Advisories []error
}

// ValidateFacturXReport runs the same checks as [Reader.ValidateFacturX]
// and returns a [FacturXReport] that pre-partitions the result into
// hard violations and Schematron advisories. Callers that only want
// the hard violations can use [Reader.ValidateFacturX] directly.
func (r *Reader) ValidateFacturXReport(ctx context.Context) (FacturXReport, error) {
	errs, err := r.ValidateFacturX(ctx)
	if err != nil {
		return FacturXReport{}, err
	}
	return partitionFacturXErrors(errs), nil
}

// ValidateZUGFeRDReport mirrors [Reader.ValidateFacturXReport] for the
// ZUGFeRD entry point. See that function's docs for how the report
// partitions errors.
func (r *Reader) ValidateZUGFeRDReport(ctx context.Context, version models.ZUGFeRDVersion) (FacturXReport, error) {
	errs, err := r.ValidateZUGFeRD(ctx, version)
	if err != nil {
		return FacturXReport{}, err
	}
	return partitionFacturXErrors(errs), nil
}

func partitionFacturXErrors(errs []error) FacturXReport {
	var rep FacturXReport
	for _, e := range errs {
		if errors.Is(e, ErrSchematronRuleSkipped) {
			rep.Advisories = append(rep.Advisories, e)
			continue
		}
		rep.Violations = append(rep.Violations, e)
	}
	return rep
}

// Structured returns the hard violations as structured [Violation]
// values carrying clause, test number, and severity. Unlike PDF/A and
// PDF/UA, a Factur-X result mixes [SeverityFatal] (PDF/A-3 base and
// fired Schematron asserts) with [SeverityWarning] (soft Schematron
// rules and report notices).
func (rep FacturXReport) Structured() []Violation {
	return toViolations(rep.Violations)
}

// Fatals returns the structured violations whose severity is
// [SeverityFatal]: the failures that make the document non-conformant.
func (rep FacturXReport) Fatals() []Violation {
	return filterBySeverity(rep.Violations, SeverityFatal)
}

// Warnings returns the structured violations whose severity is
// [SeverityWarning]: soft Schematron findings that do not by themselves
// fail conformance.
func (rep FacturXReport) Warnings() []Violation {
	return filterBySeverity(rep.Violations, SeverityWarning)
}

// facturXValidateOpts assembles the compliance options threaded into
// the validator entry points. The XSD pass uses, in order:
//
//  1. The schema set passed via [WithCIISchemas] when the caller
//     overrode the default.
//  2. The bundled D16B set from
//     [pkg/internal/compliance/cii_schemas] otherwise.
//
// A bundle load error is logged at debug and the XSD pass is skipped
// rather than failing the validation call, so a botched bump in the
// schemas directory does not break callers that rely on the spine
// rules alone.
func (r *Reader) facturXValidateOpts(ctx context.Context) []compliance.FacturXValidateOption {
	var opts []compliance.FacturXValidateOption

	// Strict by default: relax the consumer-path tolerances only when the
	// caller opted in via [WithLenient].
	opts = append(opts, compliance.WithFacturXLenient(r.lenient))

	// XSD layer: caller-supplied override first, bundled default
	// otherwise. A bundle load error degrades gracefully to spine
	// rules without aborting the validation call.
	if r.ciiSchema != nil {
		opts = append(opts, compliance.WithFacturXXSD(r.ciiSchema))
	} else if schema, err := cii_schemas.LoadDefault(ctx); err == nil {
		opts = append(opts, compliance.WithFacturXXSD(schema))
	} else {
		r.doc.Logger.Debug("xsd: bundled CII schemas unavailable, falling back to spine rules", "err", err)
	}
	// Per-Factur-X-profile XSDs (MINIMUM / BASIC WL / BASIC / EN 16931
	// / EXTENDED) tighten the cross-profile D16B schema with profile-
	// specific cardinalities. Bundle load failures degrade silently
	// to the D16B fallback configured above.
	if r.ciiSchema == nil {
		profileXSDs := map[string]*xsd.Schema{}
		for _, level := range []string{"MINIMUM", "BASIC WL", "BASIC", "EN 16931", "EXTENDED"} {
			s, err := cii_schemas.LoadFacturXProfile(ctx, level)
			if err != nil {
				r.doc.Logger.Debug("xsd: bundled Factur-X profile XSD unavailable", "level", level, "err", err)
				continue
			}
			if s != nil {
				profileXSDs[level] = s
			}
		}
		if len(profileXSDs) > 0 {
			opts = append(opts, compliance.WithFacturXProfileXSDBundle(profileXSDs))
		}
		// Legacy ZUGFeRD 1.0 XSD set lives outside the v100 family.
		if s, err := cii_schemas.LoadZUGFeRD1p0(ctx); err == nil && s != nil {
			opts = append(opts, compliance.WithZUGFeRD1XSD(s))
		} else if err != nil {
			r.doc.Logger.Debug("xsd: bundled ZUGFeRD 1.0 XSD unavailable", "err", err)
		}
	}

	// Schematron layer: caller-supplied override first, bundled
	// EN 16931 + XRechnung otherwise. The bundle route lets the
	// validator pick between the two rule sets based on the XMP
	// fx:ConformanceLevel of the document. Bundle load failures
	// log at debug and skip; the validation call does not abort.
	if r.schSchema != nil {
		opts = append(opts, compliance.WithFacturXSchematron(r.schSchema, r.schPhase))
	} else {
		var en16931Sch, xrechnungSch *schematron.Schema
		if s, err := cii_schematron.LoadDefault(ctx); err == nil {
			en16931Sch = s
		} else {
			r.doc.Logger.Debug("schematron: bundled EN 16931 rules unavailable", "err", err)
		}
		if s, err := cii_schematron.LoadXRechnung(ctx); err == nil {
			xrechnungSch = s
		} else {
			r.doc.Logger.Debug("schematron: bundled XRechnung rules unavailable", "err", err)
		}
		if en16931Sch != nil || xrechnungSch != nil {
			opts = append(opts, compliance.WithFacturXSchematronBundle(en16931Sch, xrechnungSch))
		}
		// Factur-X profile schematrons roll the CEN baseline,
		// PEPPOL-EN16931, and BR-FX-EN-* rules into one per-profile
		// rule set. The upstream sources reference a code database
		// through XPath document(); the generator at
		// cmd/gen-facturx-schematron pre-substitutes those lookups
		// with inline contains() membership tests so the bundle
		// runs cleanly under antchfx/xpath.
		profileSchematrons := map[string]*schematron.Schema{}
		for _, level := range []string{"MINIMUM", "BASIC WL", "BASIC", "EN 16931", "EXTENDED"} {
			s, err := cii_schematron.LoadFacturXProfile(ctx, level)
			if err != nil {
				r.doc.Logger.Debug("schematron: bundled Factur-X profile unavailable", "level", level, "err", err)
				continue
			}
			if s != nil {
				profileSchematrons[level] = s
			}
		}
		if len(profileSchematrons) > 0 {
			opts = append(opts, compliance.WithFacturXProfileSchematronBundle(profileSchematrons))
		}
		// Legacy ZUGFeRD 1.0 documents validate against a separate
		// schematron because their CII payload uses the
		// urn:ferd:CrossIndustryDocument:invoice:1p0 namespace the
		// v100-family bundles do not cover.
		if s, err := cii_schematron.LoadZUGFeRD1p0(ctx); err == nil && s != nil {
			opts = append(opts, compliance.WithZUGFeRD1Schematron(s))
		} else if err != nil {
			r.doc.Logger.Debug("schematron: bundled ZUGFeRD 1.0 rules unavailable", "err", err)
		}
		// PEPPOL BIS 3.0 stays opt-in via
		// [compliance.WithPeppolSchematron]: the rule set is scoped
		// to documents declaring a PEPPOL business process
		// (R001 "Business process MUST be provided", R010/R020
		// electronic address checks), so wiring it as a default
		// overlay would over-fire on the many Factur-X-style
		// invoices that legitimately omit those fields.
	}
	// Always wire the bundled Go rule overrides so rules that the
	// XPath engine cannot handle (BR-DE-19's IBAN checksum is the
	// load-bearing example) evaluate through their Go implementation
	// instead of landing in the advisory slice.
	if overrides := cii_schematron.BuiltinRuleOverrides(); len(overrides) > 0 {
		opts = append(opts, compliance.WithFacturXSchematronOverrides(overrides))
	}
	return opts
}

// ValidateZUGFeRD audits the document against the given ZUGFeRD
// version. For [models.ZUGFeRD21] this is equivalent to
// [Reader.ValidateFacturX] because the joint specification merges
// both names under the same on-disk layout.
//
// Returns [ErrInvalidZUGFeRDVersion] wrapped with the offending value
// when version is not a documented constant.
func (r *Reader) ValidateZUGFeRD(ctx context.Context, version models.ZUGFeRDVersion) (_ []error, err error) {
	defer recoverToError(r.logger(), "Reader.ValidateZUGFeRD", &err)
	if !version.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidZUGFeRDVersion, version)
	}
	return compliance.ValidateZUGFeRD(ctx, r.doc, version, r.facturXValidateOpts(ctx)...)
}

// Open initializes a new [Reader] from an [io.ReaderAt] and its total size.
// Pass functional options (e.g., [WithLogger], [WithPassword]) to customize behavior.
func Open(ctx context.Context, r io.ReaderAt, size int64, opts ...OpenOption) (_ *Reader, err error) {
	var o openOpts
	for _, opt := range opts {
		opt(&o)
	}
	defer recoverToError(o.logger, "pdf.Open", &err)
	if o.ciiFS != nil {
		schema, err := xsd.Load(ctx, o.ciiFS, o.ciiRoot)
		if err != nil {
			return nil, fmt.Errorf("loading CII schemas: %w", err)
		}
		o.ciiSchema = schema
	}
	if o.schFS != nil {
		schSchema, err := schematron.Load(ctx, o.schFS, o.schRoot)
		if err != nil {
			return nil, fmt.Errorf("loading Schematron schema: %w", err)
		}
		o.schSchema = schSchema
	}

	doc, err := document.NewDocument(ctx, r, size, o.logger)
	if err != nil {
		switch {
		case errors.Is(err, document.ErrMissingHeader):
			return nil, ErrNotAPDF
		case errors.Is(err, document.ErrDamagedXRef):
			return nil, fmt.Errorf("%w: %v", ErrDamagedXRef, err)
		default:
			return nil, fmt.Errorf("failed to open PDF document: %w", err)
		}
	}

	if doc.IsEncrypted {
		if !doc.Authenticated {
			err = doc.Authenticate(o.password)
			if err != nil {
				if errors.Is(err, crypto.ErrInvalidPassword) {
					return nil, ErrInvalidPassword
				}
				if errors.Is(err, crypto.ErrUnsupportedEncryption) {
					return nil, fmt.Errorf("%w: %v", ErrUnsupportedEncryption, err)
				}
				return nil, fmt.Errorf("authentication failed: %w", err)
			}
			doc.Logger.Debug("password authentication succeeded")
		}
	}

	if len(o.filters) > 0 {
		doc.Filters = filter.NewDecoder(o.filters)
	}

	return &Reader{
		doc:       doc,
		ocr:       o.ocr,
		ciiSchema: o.ciiSchema,
		schSchema: o.schSchema,
		schPhase:  o.schPhase,
		lenient:   o.lenient,
	}, nil
}

// OpenFile opens a PDF directly from the filesystem. The returned [Reader]
// holds a reference to the underlying file; the caller must call
// [Reader.Close] when done.
func OpenFile(ctx context.Context, path string, opts ...OpenOption) (*Reader, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	stat, err := f.Stat()
	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return nil, fmt.Errorf("failed to close file: %w", closeErr)
		}
		return nil, err
	}

	r, err := Open(ctx, f, stat.Size(), opts...)
	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return nil, fmt.Errorf("failed to close file: %w", closeErr)
		}
		return nil, err
	}
	r.closer = f
	return r, nil
}

// Version returns the PDF version string from the file header (e.g., "1.4", "2.0").
func (r *Reader) Version() string {
	return r.doc.Version
}

// IsEncrypted reports whether the document uses password-based encryption.
func (r *Reader) IsEncrypted() bool {
	return r.doc.IsEncrypted
}

// PageCount returns the total number of pages in the document.
func (r *Reader) PageCount(ctx context.Context) (_ int, err error) {
	defer recoverToError(r.logger(), "Reader.PageCount", &err)
	pages, err := r.resolvePages(ctx)
	if err != nil {
		return 0, err
	}
	return len(pages), nil
}

// ExtractTextOption configures the behavior of [Reader.ExtractText].
// ExtractTextOption closures must be safe to call from any goroutine.
type ExtractTextOption func(*extractTextOpts)

type extractTextOpts struct {
	includeHidden    bool
	includeInvisible bool
	includeClipped   bool
	includeArtifacts bool
}

// WithHiddenText includes all normally-stripped text: invisible, clipped,
// and artifact text. This is a shortcut that enables [WithInvisibleText],
// [WithClippedText], and [WithArtifacts] simultaneously.
func WithHiddenText() ExtractTextOption {
	return func(o *extractTextOpts) {
		o.includeHidden = true
	}
}

// WithInvisibleText includes text that uses rendering mode 3 (invisible)
// or white-on-white colors. Useful for forensic analysis of hidden content.
func WithInvisibleText() ExtractTextOption {
	return func(o *extractTextOpts) {
		o.includeInvisible = true
	}
}

// WithClippedText includes text that falls outside the page CropBox.
// Useful for detecting overset content or debugging layout issues.
func WithClippedText() ExtractTextOption {
	return func(o *extractTextOpts) {
		o.includeClipped = true
	}
}

// WithArtifacts includes text inside /Artifact marked content blocks
// (headers, footers, page numbers, watermarks) that is normally stripped.
func WithArtifacts() ExtractTextOption {
	return func(o *extractTextOpts) {
		o.includeArtifacts = true
	}
}

// ExtractText cleanly extracts text from a given 1-based page number.
// It orchestrates the content lexer, graphics state machine, unicode mapping,
// optional content layer rules, and spatial whitespace inference logic.
//
// By default, invisible text (rendering mode 3, white-on-white colors),
// text outside the clipping path and CropBox, and artifact text (headers,
// footers, page numbers) are silently dropped. Pass [WithHiddenText] to
// include all text regardless of visibility.
func (r *Reader) ExtractText(ctx context.Context, pageNum int, opts ...ExtractTextOption) (_ string, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractText", &err)
	var o extractTextOpts
	for _, opt := range opts {
		opt(&o)
	}

	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return "", err
	}

	// Extract raw content stream operations
	ops, err := content.ParsePageContent(ctx, r.doc, targetPage.Dict)
	if err != nil {
		return "", err
	}

	// Initialize the graphics state machine with the CropBox as the visible region.
	machine := graphics.NewMachine(boxToRect(targetPage.CropBox))
	// Extract text from every optional-content layer regardless of the
	// /OCProperties visibility flags. Visibility belongs in the viewer's
	// viewport; callers asking for textual content should get it without
	// having to pre-configure OCs.
	machine.IgnoreOCVisibility = true

	// Build the extraction config. WithHiddenText disables all stripping;
	// the granular options override individual flags.
	config := typography.ExtractionConfig{
		StripInvisible: !o.includeHidden && !o.includeInvisible,
		StripHidden:    !o.includeHidden && !o.includeInvisible,
		StripClipped:   !o.includeHidden && !o.includeClipped,
		StripArtifacts: !o.includeHidden && !o.includeArtifacts,
	}

	text, err := typography.ExtractText(ctx, r.doc, machine, targetPage, ops, config)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return "", fmt.Errorf("page %d text extraction halted: %w", pageNum, ErrFontMissing)
		}
		return "", err
	}

	r.doc.Logger.Debug("text extracted",
		slog.Int("page", pageNum),
		slog.Int("text_length", len(text)),
		slog.Int("operators", len(ops)),
		slog.Bool("include_hidden", o.includeHidden),
	)

	// OCR triage: if no text was extracted, check whether the page looks
	// like a scanned image and either hand off to the OCR engine or return
	// the sentinel error.
	if strings.TrimSpace(text) == "" {
		pageW, pageH := mediaBoxDimensions(targetPage.MediaBox)
		images := pdfimage.ExtractImages(ctx, r.doc, targetPage)
		if heuristics.RequiresOCR(nil, images, pageW, pageH) {
			if r.ocr != nil {
				return r.runOCR(ctx, targetPage, images, pageNum)
			}
			return "", fmt.Errorf("page %d: %w", pageNum, ErrRequiresOCR)
		}
	}

	return text, nil
}

// ExtractAllText extracts and concatenates text from every page in the
// document, separated by newlines. This is a convenience wrapper around
// [Reader.ExtractText] for the common "give me all the text" use case.
func (r *Reader) ExtractAllText(ctx context.Context, opts ...ExtractTextOption) (_ string, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractAllText", &err)
	pages, err := r.resolvePages(ctx)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for i := range pages {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		text, err := r.ExtractText(ctx, i+1, opts...)
		if err != nil {
			return "", fmt.Errorf("page %d: %w", i+1, err)
		}
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(text)
	}
	return sb.String(), nil
}

// Permissions represents the document's permission flags as defined by
// ISO 32000-2, Table 22. For unencrypted documents, all flags are true.
// Values must not be shared across goroutines without synchronization.
type Permissions struct {
	Print             bool // Bit 3: Print the document.
	ModifyContent     bool // Bit 4: Modify document contents.
	ExtractText       bool // Bit 5: Copy or extract text and graphics.
	ModifyAnnotations bool // Bit 6: Add or modify annotations, fill form fields.
	FillForms         bool // Bit 9: Fill form fields (even if bit 6 is clear).
	Assemble          bool // Bit 11: Assemble the document.
	PrintHighQuality  bool // Bit 12: Print high-quality (bit 3 must also be set).
}

// Permissions returns the document's permission flags. For unencrypted
// documents, all permissions are granted.
func (r *Reader) Permissions() Permissions {
	if !r.IsEncrypted() {
		return Permissions{
			Print:             true,
			ModifyContent:     true,
			ExtractText:       true,
			ModifyAnnotations: true,
			FillForms:         true,
			Assemble:          true,
			PrintHighQuality:  true,
		}
	}
	p := r.doc.Crypto.PermissionFlags()
	return Permissions{
		Print:             p&4 != 0,
		ModifyContent:     p&8 != 0,
		ExtractText:       p&16 != 0,
		ModifyAnnotations: p&32 != 0,
		FillForms:         p&256 != 0,
		Assemble:          p&1024 != 0,
		PrintHighQuality:  p&2048 != 0,
	}
}

// Mask builds the 32-bit /P permission value for these flags, suitable
// for [EncryptConfig.Permissions]. It is the inverse of the decoding in
// [Reader.Permissions]: every reserved bit the spec requires to be 1
// (bits 7-8 and 13-32, ISO 32000-2 Table 22) is set, the reserved low
// bits 1-2 are cleared, and each named permission bit is set only when
// its flag is true. This is safer than hand-assembling a mask or passing
// -1, which leaves the reserved low bits set.
func (p Permissions) Mask() int32 {
	// Start from all bits set, then clear the reserved low bits and any
	// denied permission. The high reserved bits stay 1 as the spec wants.
	var m int32 = -1
	m &^= 0b11 // reserved bits 1-2 must be 0
	clear := func(bit int32, granted bool) {
		if !granted {
			m &^= bit
		}
	}
	clear(4, p.Print)
	clear(8, p.ModifyContent)
	clear(16, p.ExtractText)
	clear(32, p.ModifyAnnotations)
	clear(256, p.FillForms)
	clear(1024, p.Assemble)
	clear(2048, p.PrintHighQuality)
	return m
}

// XMPMetadata returns a lazy Open function for the catalog /Metadata
// XMP stream, or nil when the document has no XMP packet. The stream
// data is not read until the caller invokes the returned function.
//
// Most applications want [Reader.Metadata] instead; reach for
// XMPMetadata only when you need custom XMP namespaces, PDF/A
// identification fields, or full RDF/XML access.
// Values must not be shared across goroutines without synchronization.
func (r *Reader) XMPMetadata(ctx context.Context) (_ func(context.Context) (io.ReadCloser, error), err error) {
	defer recoverToError(r.logger(), "Reader.XMPMetadata", &err)
	return semantics.XMPOpen(ctx, r.doc)
}

// Metadata returns the document-level metadata as a flat
// name-to-value map: Title, Author, Subject, Keywords, Creator,
// Producer, CreationDate, ModDate. This is the recommended entry
// point for most applications.
//
// For PDF/A-compliant documents with custom XMP namespaces or extended
// rights/history metadata, use [Reader.XMPMetadata] to read the raw
// XMP packet.
func (r *Reader) Metadata(ctx context.Context) (_ map[string]string, err error) {
	defer recoverToError(r.logger(), "Reader.Metadata", &err)
	return semantics.Metadata(ctx, r.doc)
}

// Bookmarks extracts the hierarchical outline (table of contents) from the document.
func (r *Reader) Bookmarks(ctx context.Context) (_ []models.Bookmark, err error) {
	defer recoverToError(r.logger(), "Reader.Bookmarks", &err)
	return semantics.ResolveOutline(ctx, r.doc)
}

// BookmarkGenerationOptions controls auto-generated bookmark behavior.
// See [Reader.GenerateBookmarks] and [Writer.GenerateBookmarks].
// Values must not be shared across goroutines without synchronization.
type BookmarkGenerationOptions struct {
	// MaxDepth limits how many heading levels are included. Default 3.
	MaxDepth int
	// MinFontSizeRatio sets the minimum font size (relative to body text)
	// for an element to be considered a heading. Default 1.15 (15% larger).
	MinFontSizeRatio float64
	// SkipHeaderFooter filters out text that appears as a running header or
	// footer on many pages. Default true.
	SkipHeaderFooter bool
	// MaxTitleLength truncates long headings. Default 200.
	MaxTitleLength int
	// PreferStructureTree uses the PDF's structure tree (tagged PDFs) when
	// available, falling back to font heuristics otherwise. Default true.
	PreferStructureTree bool
	// DetectNumbering parses section number prefixes (1.1, A.1, IV, §2.3,
	// Chapter 5) to improve detection and override font-size depth with
	// the numbering depth. Default true.
	DetectNumbering bool
	// StripNumberingFromTitle removes the numeric prefix from generated
	// titles (e.g., "1.1 Overview" becomes "Overview"). Default false.
	StripNumberingFromTitle bool
	// HeadingScorer scores heading candidates. When nil, the default
	// heuristic scorer is used. Replace this for ML-based or custom
	// heading detection.
	HeadingScorer HeadingScorer
	// HeadingScoreThreshold is the minimum score for a candidate to be
	// included as a heading. Default 0.5.
	HeadingScoreThreshold float64
}

// HeadingScorer assigns a heading-likelihood score to a candidate line.
// Scores are in [0, 1]; values >= 0.5 are treated as headings.
// Implementations may use any algorithm (rule-based, statistical, ML).
//
// The interface is owned by the [pdf] package so callers never have to
// import an internal path. An internal adapter translates candidates
// from the engine's shape to [HeadingCandidate] before each Score call.
// Implementations must be safe for concurrent use.
type HeadingScorer interface {
	Score(candidate HeadingCandidate) float64
}

// HeadingScript classifies a candidate's dominant script. Scorers can
// use it to pick script-specific heuristics (for example, CJK headings
// have different length and punctuation conventions than Latin).
// HeadingScript values are immutable and safe for concurrent use.
type HeadingScript string

// US Letter in PDF points, the size readers substitute for a page whose media
// box is missing or encloses no area. Not spec-mandated: ISO 32000-1, Table 30
// requires /MediaBox, so a file without a usable one is already invalid.
const (
	defaultPageWidth  = 612.0
	defaultPageHeight = 792.0
)

// Heading-script constants mirror the engine's detected script set.
const (
	HeadingScriptLatin      HeadingScript = "Latin"
	HeadingScriptCJK        HeadingScript = "CJK"
	HeadingScriptCyrillic   HeadingScript = "Cyrillic"
	HeadingScriptArabic     HeadingScript = "Arabic"
	HeadingScriptDevanagari HeadingScript = "Devanagari"
	HeadingScriptThai       HeadingScript = "Thai"
	HeadingScriptOther      HeadingScript = "Other"
)

// HeadingCandidate is the input passed to a [HeadingScorer]. It holds
// the reconstructed line text plus structural features that distinguish
// headings from body text.
// Values must not be shared across goroutines without synchronization.
type HeadingCandidate struct {
	// Text is the reconstructed line text.
	Text string
	// FontSize is the page-space font size of the line.
	FontSize float64
	// BodySize is the detected body text size for the document (or 0
	// if unknown).
	BodySize float64
	// PageNum is the 1-based page number where the line appears.
	PageNum int
	// CharCount is the number of characters in Text after normalization.
	CharCount int
	// IsBold indicates the line uses a bold font.
	IsBold bool
	// HasNumbering indicates the line starts with a section-number prefix.
	HasNumbering bool
	// NumberingDepth is the depth implied by the numbering (1 for "1",
	// 2 for "1.1", etc.). 0 if no numbering.
	NumberingDepth int
	// Script is the dominant script of the text.
	Script HeadingScript
}

// headingScorerAdapter wraps a public [HeadingScorer] so it satisfies
// the internal [bookmarks.HeadingScorer] interface. Each Score call
// translates the internal candidate shape to the public one, so the
// user's scorer never sees an internal type.
type headingScorerAdapter struct{ inner HeadingScorer }

func (a headingScorerAdapter) Score(c bookmarks.HeadingCandidate) float64 {
	return a.inner.Score(HeadingCandidate{
		Text:           c.Text,
		FontSize:       c.FontSize,
		BodySize:       c.BodySize,
		PageNum:        c.PageNum,
		CharCount:      c.CharCount,
		IsBold:         c.IsBold,
		HasNumbering:   c.HasNumbering,
		NumberingDepth: c.NumberingDepth,
		Script:         HeadingScript(bookmarks.ScriptName(c.Script)),
	})
}

// adaptHeadingScorer returns the internal scorer interface, or nil if
// the public scorer is nil so downstream callers fall back to the
// default heuristic.
func adaptHeadingScorer(s HeadingScorer) bookmarks.HeadingScorer {
	if s == nil {
		return nil
	}
	return headingScorerAdapter{inner: s}
}

// GenerateBookmarks analyzes the document content and returns an
// auto-generated bookmark tree based on heading detection. This is a
// read-only operation that does not modify the document; use
// [Writer.GenerateBookmarks] to apply the result.
//
// When the document has a structure tree and PreferStructureTree is true
// (default), bookmarks are extracted from tagged H1-H6 elements. Otherwise,
// bookmarks are inferred from font size relative to body text.
func (r *Reader) GenerateBookmarks(ctx context.Context, opts BookmarkGenerationOptions) (_ []models.Bookmark, err error) {
	defer recoverToError(r.logger(), "Reader.GenerateBookmarks", &err)
	input, err := r.buildBookmarkInput(ctx)
	if err != nil {
		return nil, err
	}
	return bookmarks.Generate(ctx, input, bookmarks.Options{
		MaxDepth:                opts.MaxDepth,
		MinFontSizeRatio:        opts.MinFontSizeRatio,
		SkipHeaderFooter:        opts.SkipHeaderFooter,
		MaxTitleLength:          opts.MaxTitleLength,
		PreferStructureTree:     opts.PreferStructureTree,
		DetectNumbering:         opts.DetectNumbering,
		StripNumberingFromTitle: opts.StripNumberingFromTitle,
		Scorer:                  adaptHeadingScorer(opts.HeadingScorer),
		ScoreThreshold:          opts.HeadingScoreThreshold,
	})
}

// buildBookmarkInput prepares the data needed by the bookmarks package:
// resolved pages, page ref → page number map, and per-page text elements.
func (r *Reader) buildBookmarkInput(ctx context.Context) (bookmarks.Input, error) {
	resolvedPages, err := r.resolvePages(ctx)
	if err != nil {
		return bookmarks.Input{}, err
	}

	pageRefToNum := make(map[ast.PdfIndirectRef]int, len(resolvedPages))
	pageTexts := make([]bookmarks.PageTextElements, 0, len(resolvedPages))

	for i, p := range resolvedPages {
		pageNum := i + 1
		pageRefToNum[p.Ref] = pageNum

		elements, err := r.ExtractTextElements(ctx, pageNum)
		if err != nil {
			// Skip pages that fail to extract; don't abort the whole operation.
			continue
		}
		pageTexts = append(pageTexts, bookmarks.PageTextElements{
			PageNum:  pageNum,
			Elements: elements,
		})
	}

	return bookmarks.Input{
		Doc:          r.doc,
		PageRefToNum: pageRefToNum,
		Pages:        pageTexts,
	}, nil
}

// FormData returns filled-in AcroForm values as a name-to-value map,
// evaluating any PDF JavaScript calculation scripts. Use this when all
// you need is "what did the user type".
//
// Use [Reader.FormFields] to list every field, including the ones nobody
// filled in.
func (r *Reader) FormData(ctx context.Context) (_ map[string]string, err error) {
	defer recoverToError(r.logger(), "Reader.FormData", &err)
	fields, err := annotation.ExtractFormData(ctx, r.doc)
	if err != nil {
		return nil, err
	}

	data := make(map[string]string)
	for _, f := range fields {
		data[f.Name] = f.Value
	}
	return data, nil
}

// FormFields lists every named field of the interactive form, in the order the
// field tree gives them, evaluating any PDF JavaScript calculation scripts. A
// field nobody filled in is reported with an empty value.
//
// Use [Reader.FormData] for the flat name-to-value mapping of the fields that
// actually carry a value.
func (r *Reader) FormFields(ctx context.Context) (_ []models.FormField, err error) {
	defer recoverToError(r.logger(), "Reader.FormFields", &err)
	return annotation.ExtractFormFields(ctx, r.doc)
}

// ExtractTables detects tables on the given 1-based page number by analysing
// stroked line segments (physical grid) and, when no lines are found, by
// spatially clustering text elements into rows and inferring column boundaries
// from consistent X-gaps.
func (r *Reader) ExtractTables(ctx context.Context, pageNum int) (_ []models.Table, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractTables", &err)
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}

	ops, err := content.ParsePageContent(ctx, r.doc, targetPage.Dict)
	if err != nil {
		return nil, err
	}

	// Extract text elements via the graphics state machine.
	machine := graphics.NewMachine(boxToRect(targetPage.CropBox))
	machine.IgnoreOCVisibility = true
	elements, err := typography.ExtractElements(ctx, r.doc, machine, targetPage, ops)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return nil, fmt.Errorf("page %d table extraction halted: %w", pageNum, ErrFontMissing)
		}
		return nil, err
	}

	// Extract stroked line segments from the same content stream.
	lines := graphics.ExtractLines(ops)

	return heuristics.DetectTables(elements, lines)
}

// HasAcroForm reports whether the document exposes an interactive
// AcroForm with at least one field. Catalog-level /NeedsRendering true
// marks the document as an XFA-rendered form; the AcroForm dictionary
// on such documents is a vestigial wrapper around XFA data, so
// HasAcroForm returns false for them. Hybrid documents that declare
// both /XFA and real widget fields keep returning true, since the
// widget fields are genuinely interactive under a standard reader.
func (r *Reader) HasAcroForm(ctx context.Context) (has bool) {
	// This entry point resolves the catalog out of file-controlled bytes like
	// any other, so it needs the same panic boundary, but its signature carries
	// no error to report one through. Recover with a nil error pointer: the
	// panic is still logged at Error with a stack, and the answer falls back to
	// false, which is what an unreadable catalog means to a caller anyway.
	defer recoverToError(r.logger(), "Reader.HasAcroForm", nil)

	resolved, err := document.ResolveCatalog(ctx, r.doc)
	if err != nil || resolved == nil || resolved.Dict == nil {
		return false
	}
	catalog := resolved.Dict
	if nrObj, ok := catalog["NeedsRendering"]; ok {
		if nr, err := document.ResolveBool(ctx, r.doc, nrObj); err == nil && nr {
			return false
		}
	}
	acroFormObj, ok := catalog["AcroForm"]
	if !ok {
		return false
	}
	acroForm, err := document.ResolveDict(ctx, r.doc, acroFormObj)
	if err != nil || acroForm == nil {
		return false
	}
	fieldsArr, err := document.ResolveArray(ctx, r.doc, acroForm["Fields"])
	return err == nil && len(fieldsArr) > 0
}

// HasFormFields reports whether the document carries any interactive form
// field, whether the interactive form lists it or only a page presents it.
//
// [Reader.HasAcroForm] answers the narrower question its name states, and keeps
// the XFA handling that goes with it, so it is asked first and its answer is
// taken as final when it says yes. The page scan only adds the fields no
// /AcroForm lists: pdfjs issue12963.pdf carries 27 of them and no /AcroForm, so
// the narrower answer read "no" for a document whose fields litho lists.
func (r *Reader) HasFormFields(ctx context.Context) (_ bool, err error) {
	defer recoverToError(r.logger(), "Reader.HasFormFields", &err)
	if r.HasAcroForm(ctx) {
		return true, nil
	}
	return annotation.HasPageWidgetField(ctx, r.doc), nil
}

// IsPortfolio reports whether the document is a PDF Portfolio (Collection).
// Portfolio PDFs are containers whose visible pages are merely a cover sheet;
// the real payload is a set of embedded files.
func (r *Reader) IsPortfolio(ctx context.Context) (_ bool, err error) {
	defer recoverToError(r.logger(), "Reader.IsPortfolio", &err)
	return semantics.IsPortfolio(ctx, r.doc)
}

// EmbeddedFiles returns every file embedded in the document: the /EmbeddedFiles
// name tree plus the files carried by FileAttachment annotations, which readers
// list alongside them. Each item includes the original filename, MIME type, and
// a lazy Open accessor. Callers can recursively pass embedded PDFs back into
// pdf.Open. A file reached from both places is returned once.
func (r *Reader) EmbeddedFiles(ctx context.Context) (_ []models.EmbeddedFile, err error) {
	defer recoverToError(r.logger(), "Reader.EmbeddedFiles", &err)
	return semantics.ExtractEmbeddedFiles(ctx, r.doc)
}

// ExtractAnnotations extracts annotations from the given 1-based page number.
func (r *Reader) ExtractAnnotations(ctx context.Context, pageNum int) (_ []models.Annotation, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractAnnotations", &err)
	pages, err := r.resolvePages(ctx)
	if err != nil {
		return nil, err
	}

	if pageNum < 1 || pageNum > len(pages) {
		return nil, fmt.Errorf("%w: %d (document has %d pages)", ErrInvalidPageNumber, pageNum, len(pages))
	}

	pageMap := document.BuildPageMap(pages)
	return annotation.ExtractAnnotations(ctx, r.doc, pages[pageNum-1], pageMap)
}

// IsLinearized reports whether the document is linearized (optimised
// for fast web view / byte-serving). Linearized PDFs place page 1
// data at the beginning of the file so it can be rendered before the
// rest of the document has downloaded.
func (r *Reader) IsLinearized() bool {
	return r.doc.IsLinearized()
}

// LinearizationInfo returns the parameters and per-page hint entries
// decoded from a linearized PDF, or nil when the document is not
// linearized.
func (r *Reader) LinearizationInfo() *models.LinearizationParams {
	lp := r.doc.Linearization
	if lp == nil {
		return nil
	}
	params := &models.LinearizationParams{
		FileLength:          lp.FileLength,
		HintStreamOffset:    lp.HintStreamOffset,
		HintStreamLength:    lp.HintStreamLength,
		FirstPageObjNum:     lp.FirstPageObjNum,
		NumPages:            lp.NumPages,
		FirstPageEndOffset:  lp.FirstPageEndOffset,
		FirstPageXRefOffset: lp.FirstPageXRefOffset,
	}
	if ht := r.doc.HintTable; ht != nil {
		params.Hints = make([]models.PageOffsetEntry, len(ht))
		for i, e := range ht {
			params.Hints[i] = models.PageOffsetEntry{
				ObjCount:            e.ObjCount,
				PageLength:          e.PageLength,
				ContentStreamOffset: e.ContentStreamOffset,
			}
		}
	}
	return params
}

// ExtractSignatures returns the digital signatures present in the document
// without performing chain validation.
func (r *Reader) ExtractSignatures(ctx context.Context) (_ []models.Signature, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractSignatures", &err)
	return signature.ExtractSignatures(ctx, r.doc)
}

// VerifySignatures validates every digital signature in the document by
// parsing the embedded PKCS#7/CMS block, extracting the X.509 signer
// certificates, and verifying the chain against the provided trusted root
// certificate pool. Pass nil for roots to use the system root certificates.
//
// When a Document Security Store (DSS) is present, embedded OCSP responses,
// CRLs, and RFC 3161 timestamp tokens are used for Long-Term Validation
// (LTV), enabling offline verification of signatures with expired certificates.
func (r *Reader) VerifySignatures(ctx context.Context, roots *x509.CertPool) (_ []models.SignatureValidation, err error) {
	defer recoverToError(r.logger(), "Reader.VerifySignatures", &err)
	return signature.VerifySignatures(ctx, r.doc, roots)
}

// DSSInfo returns a summary of the Document Security Store (/DSS)
// contents. The DSS holds the embedded validation materials (certificates,
// CRLs, OCSP responses) needed for Long-Term Validation. Returns nil if
// no DSS exists.
func (r *Reader) DSSInfo(ctx context.Context) (_ *models.DSSInfo, err error) {
	defer recoverToError(r.logger(), "Reader.DSSInfo", &err)
	return signature.ResolveDSSInfo(ctx, r.doc)
}

// Page returns the geometry and metadata for the given 1-based page number.
// Width and Height are effective dimensions in PDF points, derived from the
// CropBox (the visible area) and accounting for /Rotate and /UserUnit.
func (r *Reader) Page(ctx context.Context, pageNum int) (_ models.Page, err error) {
	defer recoverToError(r.logger(), "Reader.Page", &err)
	p, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return models.Page{}, err
	}

	box := p.CropBox
	if box == nil {
		box = p.MediaBox
	}
	w, h := mediaBoxDimensions(box)

	// A page whose box is absent or encloses no area has no renderable size.
	// ISO 32000-1, Table 30 makes /MediaBox a required page entry, so the spec
	// says nothing about what to report when it is missing, and answering 0 by 0
	// describes a page no viewer would draw. Substitute the size every
	// mainstream reader uses: pdf.js falls back to its LETTER_SIZE_MEDIABOX and
	// poppler reports 612 x 792 for pdfjs boundingBox_invalid.pdf, whose first
	// page declares /MediaBox [0 0 0 0], and for govdocs thread_400_400739.pdf,
	// which has no /MediaBox anywhere.
	//
	// The MediaBox and CropBox fields still report what the file declares. Only
	// the effective dimensions gain the fallback.
	if w == 0 || h == 0 {
		w, h = defaultPageWidth, defaultPageHeight
	}

	// /UserUnit (PDF 1.6+): scale raw coordinates to standard points.
	if p.UserUnit > 0 && p.UserUnit != 1.0 {
		w *= p.UserUnit
		h *= p.UserUnit
	}

	// /Rotate swaps the effective dimensions for 90° and 270°.
	rot := p.Rotate % 360
	if rot < 0 {
		rot += 360
	}
	if rot == 90 || rot == 270 {
		w, h = h, w
	}

	return models.Page{
		PageNumber:  pageNum,
		Width:       w,
		Height:      h,
		Rotate:      p.Rotate,
		UserUnit:    p.UserUnit,
		IsLandscape: w > h,
		DPM:         p.DPM,
		MediaBox:    boxToRect(p.MediaBox),
		CropBox:     boxToRect(p.CropBox),
		BleedBox:    boxToRect(p.BleedBox),
		TrimBox:     boxToRect(p.TrimBox),
		ArtBox:      boxToRect(p.ArtBox),
	}, nil
}

// ExtractTextElements extracts spatially positioned text elements from
// the given 1-based page number.
func (r *Reader) ExtractTextElements(ctx context.Context, pageNum int) (_ []models.TextElement, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractTextElements", &err)
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}

	ops, err := content.ParsePageContent(ctx, r.doc, targetPage.Dict)
	if err != nil {
		return nil, err
	}

	machine := graphics.NewMachine(boxToRect(targetPage.CropBox))
	machine.IgnoreOCVisibility = true
	elements, err := typography.ExtractElements(ctx, r.doc, machine, targetPage, ops)
	if err != nil {
		if errors.Is(err, typography.ErrFontNotFound) {
			return nil, fmt.Errorf("page %d text element extraction halted: %w", pageNum, ErrFontMissing)
		}
		return nil, err
	}

	return typography.CoalesceElements(ctx, elements), nil
}

// ExtractImages returns metadata for the images the given 1-based page shows,
// without decoding pixel data.
//
// An image drawn through a form XObject counts (ISO 32000-1, 8.10.1): reporting
// only the page's own resources missed the nested ones on 583 pages of the pdfjs
// and govdocs corpora. An image reached from more than one place is reported
// once. An image named twice in one resource dictionary stays two entries.
func (r *Reader) ExtractImages(ctx context.Context, pageNum int) (_ []models.ImageMeta, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractImages", &err)
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	return pdfimage.ExtractImagesRecursive(ctx, r.doc, targetPage), nil
}

// ExtractLines extracts stroked line segments from the content stream of
// the given 1-based page number.
func (r *Reader) ExtractLines(ctx context.Context, pageNum int) (_ []models.Line, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractLines", &err)
	targetPage, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}

	ops, err := content.ParsePageContent(ctx, r.doc, targetPage.Dict)
	if err != nil {
		return nil, err
	}

	return graphics.ExtractLines(ops), nil
}

// ExtractGeoData extracts geospatial metadata from the given 1-based
// page. Scans the page's /VP (Viewport) array and Image XObjects for
// /Measure dictionaries with /Subtype /GEO, returning tie points that
// map PDF coordinates to GPS (latitude/longitude). Returns nil when the
// page declares no geospatial data.
func (r *Reader) ExtractGeoData(ctx context.Context, pageNum int) (_ []models.GeoData, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractGeoData", &err)
	page, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	return geospatial.ExtractGeoData(ctx, r.doc, page)
}

// ExtractMeasurements extracts 2D rectilinear measurement scales from
// the given 1-based page. Scans the page's /VP (Viewport) array and
// Line/PolyLine annotations for /Measure dictionaries with
// /Subtype /RL, returning scale axes that map PDF coordinates to
// real-world units.
func (r *Reader) ExtractMeasurements(ctx context.Context, pageNum int) (_ []models.Measurement, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractMeasurements", &err)
	page, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	return annotation.ExtractMeasurements(ctx, r.doc, page)
}

// Extract3DModels extracts 3D model streams (U3D or PRC) from /3D
// annotations on the given 1-based page. Each returned Model3D carries
// a lazy Open accessor; the stream data is not decoded until the caller
// invokes Open. The caller must close the reader returned by Open.
func (r *Reader) Extract3DModels(ctx context.Context, pageNum int) (_ []models.Model3D, err error) {
	defer recoverToError(r.logger(), "Reader.Extract3DModels", &err)
	page, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	return semantics.Extract3DModels(ctx, r.doc, page)
}

// ExtractPronunciationLexicons returns the W3C PLS (Pronunciation
// Lexicon Specification) XML documents referenced from the catalog's
// /PronunciationLexicon array (PDF 2.0). Returns nil when none are
// present.
func (r *Reader) ExtractPronunciationLexicons(ctx context.Context) (_ []models.PronunciationLexicon, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractPronunciationLexicons", &err)
	return semantics.ExtractPronunciationLexicons(ctx, r.doc)
}

// ExtractFormulas walks the document structure tree and returns MathML
// from /Formula elements that carry an /AF entry with /AFRelationship
// /Supplement and MIME type application/mathml+xml (PDF 2.0).
func (r *Reader) ExtractFormulas(ctx context.Context) (_ []models.FormulaElement, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractFormulas", &err)
	return structure.ExtractFormulas(ctx, r.doc)
}

// ExtractRedactedText extracts text elements from the given 1-based
// page and flags any that fall inside unapplied /Redact annotations.
// Use this to detect redactions applied visually but not committed to
// content streams.
func (r *Reader) ExtractRedactedText(ctx context.Context, pageNum int) (_ []models.TextElement, err error) {
	defer recoverToError(r.logger(), "Reader.ExtractRedactedText", &err)
	elements, err := r.ExtractTextElements(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	annots, err := r.ExtractAnnotations(ctx, pageNum)
	if err != nil {
		return nil, err
	}
	return heuristics.DetectUnappliedRedactions(elements, annots), nil
}

// UndecodableStreams reports in-use stream objects whose /Filter chain
// contains a filter litho cannot decode: JBIG2Decode or JPXDecode when
// the corresponding plugin (see [WithFilterPlugin]) is not registered, or
// an unknown filter. Text, image, and validation results for those
// objects are necessarily incomplete; registering the matching plugin
// makes them decodable. Returns an empty slice when every stream is
// decodable. The check inspects filter names and does not decode the
// streams.
func (r *Reader) UndecodableStreams(ctx context.Context) (_ []models.StreamDiagnostic, err error) {
	defer recoverToError(r.logger(), "Reader.UndecodableStreams", &err)
	diags, err := r.doc.UndecodableStreams(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.StreamDiagnostic, 0, len(diags))
	for _, d := range diags {
		out = append(out, models.StreamDiagnostic{ObjectNum: d.ObjectNum, Filters: d.Filters})
	}
	return out, nil
}

// resolvePages retrieves the catalog and resolves all pages. The result is
// memoized after the first successful call since the document is read-only
// after Open. Safe for concurrent use.
func (r *Reader) resolvePages(ctx context.Context) ([]*document.Page, error) {
	r.pagesOnce.Do(func() {
		catalog, err := document.ResolveCatalog(ctx, r.doc)
		if err != nil {
			r.pagesErr = err
			return
		}
		r.cachedPages, r.pagesErr = document.ResolvePages(ctx, r.doc, catalog)
	})
	return r.cachedPages, r.pagesErr
}

// resolvePage resolves the page tree and returns the page at the given
// 1-based page number.
func (r *Reader) resolvePage(ctx context.Context, pageNum int) (*document.Page, error) {
	pages, err := r.resolvePages(ctx)
	if err != nil {
		return nil, err
	}
	if pageNum < 1 || pageNum > len(pages) {
		return nil, fmt.Errorf("%w: %d (document has %d pages)", ErrInvalidPageNumber, pageNum, len(pages))
	}
	return pages[pageNum-1], nil
}

// mediaBoxDimensions extracts page width and height from a MediaBox array
// [llx lly urx ury].
func mediaBoxDimensions(mb ast.PdfArray) (float64, float64) {
	if len(mb) < 4 {
		return 0, 0
	}
	llx := document.Float(mb[0])
	lly := document.Float(mb[1])
	urx := document.Float(mb[2])
	ury := document.Float(mb[3])

	w := urx - llx
	h := ury - lly
	if w < 0 {
		w = -w
	}
	if h < 0 {
		h = -h
	}
	return w, h
}

// boxToRect converts a PDF rectangle array [llx, lly, urx, ury] to [4]float64.
func boxToRect(arr ast.PdfArray) [4]float64 {
	if len(arr) < 4 {
		return [4]float64{}
	}
	return [4]float64{document.Float(arr[0]), document.Float(arr[1]), document.Float(arr[2]), document.Float(arr[3])}
}

// runOCR extracts the largest image from the page, decodes it, and passes it
// to the injected OCR engine.
func (r *Reader) runOCR(ctx context.Context, page *document.Page, images []models.ImageMeta, pageNum int) (string, error) {
	// Find the largest image by pixel area.
	bestArea := 0
	bestName := ""
	for _, img := range images {
		area := img.Width * img.Height
		if area > bestArea {
			bestArea = area
			bestName = img.Filename
		}
	}
	if bestName == "" {
		return "", fmt.Errorf("page %d: %w", pageNum, ErrRequiresOCR)
	}

	// Decode the image stream.
	img, err := pdfimage.DecodeImage(ctx, r.doc, page, ast.PdfName(bestName))
	if err != nil {
		return "", fmt.Errorf("page %d: %w: image decode failed: %v", pageNum, ErrOCRFailed, err)
	}

	text, err := r.ocr.Extract(img)
	if err != nil {
		return "", fmt.Errorf("page %d: %w: %v", pageNum, ErrOCRFailed, err)
	}
	return text, nil
}
