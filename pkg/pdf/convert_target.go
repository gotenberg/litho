package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/models"
)

// WithConvertVersion sets the PDF version [Convert] writes: "1.4", "1.5",
// "1.6", "1.7", the default, or "2.0". The header declares it, and the file
// holds no construct a later version defines: what the source asks for that
// the version lacks is written as the nearest thing the version has. Any
// other value is refused with [ErrInvalidConvertOptions].
//
// A profile given to [WithConvertPDFA] or [WithConvertPDFUA] decides the
// version when this option is absent, and bounds it when present: PDF/A-1
// takes 1.4 alone, PDF/A-2, PDF/A-3 and PDF/UA-1 take 1.4 to 1.7 and
// default to 1.7, PDF/A-4 and PDF/UA-2 take 2.0 alone. A version the
// profile does not take is refused with [ErrInvalidConvertOptions].
func WithConvertVersion(version string) ConvertOption {
	return func(o *convertOptions) { o.version = version }
}

// WithConvertPDFA makes [Convert] write a file that conforms to a PDF/A
// profile (ISO 19005), such as [models.PDFA2b]: the file identifies itself
// as that profile in its XMP metadata, states its colours through an sRGB
// output intent, and holds nothing the profile forbids. PDF/A-1 forbids
// transparency (ISO 19005-1, 6.4), so a translucent region of the source is
// composited into an opaque image there, at 300 dpi, the text kept as
// text; every other profile draws the pages as a conversion without a
// profile does. The empty profile, the default, asks for none.
//
// [ConvertReport.Conformance] says what is left that the profile forbids
// or requires. It is empty for most sources. The file claims the profile
// either way, so read the report rather than the claim.
//
// [models.PDFA4f] and the Factur-X profiles need an embedded file a
// conversion does not have, and are refused with
// [ErrInvalidConvertOptions]: convert without a profile, attach the file
// with a [Writer] and remediate there. An unknown profile is refused the
// same way. Combined with [WithConvertPDFUA], the two profiles must be
// ones a single file can meet, as [WithPDFUA] pairs them, or Convert fails
// with an error wrapping [ErrInvalidConvertOptions] and
// [ErrIncompatibleProfiles].
func WithConvertPDFA(profile models.PDFAProfile) ConvertOption {
	return func(o *convertOptions) { o.pdfa = profile }
}

// WithConvertPDFUA makes [Convert] write a file that conforms to a PDF/UA
// profile (ISO 14289), [models.PDFUA1] or [models.PDFUA2]: the file
// identifies itself as that profile, and its structure tree, which every
// conversion writes, carries what the profile requires of it. The empty
// profile, the default, asks for none.
//
// A profile requires what only the source can say: the text that describes
// a picture, headings whose levels do not skip, a title. Where the source
// is silent the file still claims the profile, and
// [ConvertReport.Conformance] lists what is missing: read it rather than
// the claim. An unknown profile is refused with
// [ErrInvalidConvertOptions].
func WithConvertPDFUA(profile models.PDFUAProfile) ConvertOption {
	return func(o *convertOptions) { o.pdfua = profile }
}

// WithConvertTempDir sets the directory [Convert] writes its intermediate
// files in, removed before it returns. Empty, the default, is the
// operating system's temporary directory. A conversion without a profile
// writes none today.
func WithConvertTempDir(dir string) ConvertOption {
	return func(o *convertOptions) { o.tempDir = dir }
}

// WithDefaultPaperSize sets the page size in points, width then height, of
// a source that names none: a text or Markdown file, an HTML document
// without an @page size, a section of a text document, a sheet or a CSV
// file without a page setup. Without it that size is A4, 595.276 by
// 841.890 points. It never replaces a size the source names, which
// [WithPaperSize] does. Each side must be finite, positive and at most
// 14,400 points, or [Convert] fails with [ErrInvalidConvertOptions]; both
// zero keep the default.
func WithDefaultPaperSize(width, height float64) ConvertOption {
	return func(o *convertOptions) { o.defaultWidth, o.defaultHeight = width, height }
}

// WithDefaultLanguage sets the natural language of a source that names
// none, as a BCP 47 tag such as "en-US" or "ja": the language the file
// declares for its text (ISO 32000-1, 14.9.2), which PDF/UA requires, and
// the one that chooses between the regional faces of a font family for
// Han characters. Empty, the default, leaves the language unset. It never
// replaces a language the source names. A tag that is not well formed is
// refused with [ErrInvalidConvertOptions].
func WithDefaultLanguage(lang string) ConvertOption {
	return func(o *convertOptions) { o.defaultLang = lang }
}

// convertTarget is the file a [Convert] call writes: its version and the
// profiles it conforms to, each "" for none.
type convertTarget struct {
	version compose.Version
	pdfa    models.PDFAProfile
	pdfua   models.PDFUAProfile
}

// conforming reports whether the target names a profile.
func (t convertTarget) conforming() bool {
	return t.pdfa != "" || t.pdfua != ""
}

// resolveConvertTarget returns the target o asks for, or an error wrapping
// [ErrInvalidConvertOptions] for a version, a profile or a combination
// [Convert] does not write (see [WithConvertVersion], [WithConvertPDFA]
// and [WithConvertPDFUA]).
func resolveConvertTarget(o *convertOptions) (convertTarget, error) {
	t := convertTarget{pdfa: o.pdfa, pdfua: o.pdfua}
	aPart, uaPart := compliance.PDFAPart(string(o.pdfa)), compliance.PDFUAPart(string(o.pdfua))
	if o.pdfa != "" && aPart == 0 {
		return t, fmt.Errorf("%w: PDF/A profile %q", ErrInvalidConvertOptions, o.pdfa)
	}
	if o.pdfua != "" && uaPart == 0 {
		return t, fmt.Errorf("%w: PDF/UA profile %q", ErrInvalidConvertOptions, o.pdfua)
	}
	if o.pdfa == models.PDFA4f {
		return t, fmt.Errorf("%w: %s requires an embedded file, which a conversion does not have", ErrInvalidConvertOptions, o.pdfa)
	}
	if err := checkPair(o.pdfa, o.pdfua); err != nil {
		return t, fmt.Errorf("%w: %w", ErrInvalidConvertOptions, err)
	}

	// The versions a profile takes, oldest first, and the one it defaults
	// to: PDF/A-1 builds on PDF 1.4 (ISO 19005-1, clause 1); PDF/A-2,
	// PDF/A-3 (ISO 19005-2 and -3, 6.1.2) and PDF/UA-1 (ISO 14289-1, 6.1)
	// on ISO 32000-1, whose header may name any version up to 1.7; PDF/A-4
	// (ISO 19005-4, 6.1.2) and PDF/UA-2 (ISO 14289-2, 6.1) on PDF 2.0.
	lowest, highest, preferred := compose.Version14, compose.Version20, compose.Version17
	switch {
	case aPart == 4 || uaPart == 2:
		lowest, highest, preferred = compose.Version20, compose.Version20, compose.Version20
	case aPart == 1:
		lowest, highest, preferred = compose.Version14, compose.Version14, compose.Version14
	case aPart != 0 || uaPart != 0:
		lowest, highest, preferred = compose.Version14, compose.Version17, compose.Version17
	}
	if o.version == "" {
		t.version = preferred
		return t, nil
	}
	switch v := compose.Version(o.version); v {
	case compose.Version14, compose.Version15, compose.Version16, compose.Version17, compose.Version20:
		// The versions compare as strings: one digit, a dot, one digit.
		if v < lowest || v > highest {
			return t, fmt.Errorf("%w: PDF version %s with %s", ErrInvalidConvertOptions, o.version, t.profiles())
		}
		t.version = v
		return t, nil
	}
	return t, fmt.Errorf("%w: PDF version %q", ErrInvalidConvertOptions, o.version)
}

// profiles names the target's profiles for a message.
func (t convertTarget) profiles() string {
	switch {
	case t.pdfa != "" && t.pdfua != "":
		return string(t.pdfa) + " and " + string(t.pdfua)
	case t.pdfa != "":
		return string(t.pdfa)
	}
	return string(t.pdfua)
}

// writeTarget writes doc to dst as the target's file and returns what the
// target's profiles still find in it, nil for a target without a profile.
// Nothing reaches dst before the whole file is built.
//
// A target with a profile is written twice: the composition as a plain
// file of the target's version, then that file through the remediation a
// [Writer] runs, which owns conformance for every PDF litho writes.
func writeTarget(ctx context.Context, doc *compose.Document, dst io.Writer, t convertTarget, o *convertOptions) (*RemediationReport, error) {
	opts := compose.Options{Logger: o.logger, Version: t.version}
	if !t.conforming() {
		if err := compose.Write(ctx, doc, dst, opts); err != nil {
			return nil, fmt.Errorf("writing the PDF: %w", err)
		}
		return nil, nil
	}
	var plain bytes.Buffer
	if err := compose.Write(ctx, doc, &plain, opts); err != nil {
		return nil, fmt.Errorf("writing the PDF: %w", err)
	}
	openOpts := []OpenOption{WithLimits(o.limits)}
	if o.logger != nil {
		openOpts = append(openOpts, WithLogger(o.logger))
	}
	r, err := Open(ctx, bytes.NewReader(plain.Bytes()), int64(plain.Len()), openOpts...)
	if err != nil {
		return nil, fmt.Errorf("reading the PDF back: %w", err)
	}
	defer r.Close() //nolint:errcheck // an in-memory reader holds nothing to release late
	writeOpts := []WriteOption{WithFullRewrite(), WithVersion(string(t.version))}
	if o.tempDir != "" {
		writeOpts = append(writeOpts, WithTempDir(o.tempDir))
	}
	w := NewWriter(r, writeOpts...)
	defer w.Close() //nolint:errcheck // Close removes temp files; a failure leaves one behind and changes no output
	var report *RemediationReport
	if t.pdfa != "" {
		var remOpts []RemediateOption
		if t.pdfua != "" {
			remOpts = append(remOpts, WithPDFUA(t.pdfua))
		}
		report, err = w.RemediatePDFA(ctx, t.pdfa, remOpts...)
	} else {
		report, err = w.RemediatePDFUA(ctx, t.pdfua)
	}
	if err != nil {
		return nil, fmt.Errorf("conforming to %s: %w", t.profiles(), err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		return nil, fmt.Errorf("writing the PDF: %w", err)
	}
	if _, err := dst.Write(out.Bytes()); err != nil {
		return nil, fmt.Errorf("writing the PDF: %w", err)
	}
	return report, nil
}
