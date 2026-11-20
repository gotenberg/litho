// Command gen-locales generates the locale table of pkg/internal/numfmt
// and the Locale constants of pkg/models: every locale LibreOffice ships,
// with what a spreadsheet leaves to the locale it is formatted under.
//
// It works in two steps. The extract step reads LibreOffice core at a
// pinned commit, the locale data of i18npool/source/localedata/data, the
// Windows locale identifiers of i18nlangtag/source/isolang/isolang.cxx
// and include/i18nlangtag/lang.h, and the tables by which Calc's import
// maps the built-in number formats of a workbook a locale at a time
// (sc/source/filter/oox/numberformatsbuffer.cxx, its macros expanded, and
// the indices of offapi/com/sun/star/i18n/NumberFormatIndex.idl). It
// refuses sources whose SHA-256 differs from the pinned digest, resolves
// each locale's references to another locale's sections, translates its
// format codes into the SpreadsheetML syntax numfmt reads, and writes
// data/localedata.json. The generate step, the default, reads that file,
// resolves Calc's tables for each locale as Calc's import does, and writes
// the two Go files. A test of the command fails when the committed Go
// files are not what the generate step writes from the committed extract.
//
// Each field of the table names the element of LibreOffice's locale file
// it comes from. LibreOffice's files hold every field the table needs:
// nothing is taken from the Unicode CLDR.
//
// The locale data is under the Mozilla Public License 2.0: the extract
// carries it, data/LICENSE-MPL beside it holds the licence, and the
// generated files name it.
//
// Usage:
//
//	git clone --filter=blob:none --no-checkout --sparse \
//	    https://github.com/LibreOffice/core.git lo
//	git -C lo sparse-checkout set i18npool/source/localedata/data \
//	    i18nlangtag/source/isolang include/i18nlangtag \
//	    sc/source/filter/oox offapi/com/sun/star/i18n
//	git -C lo checkout 9dea406ab7695df6325b11b0e19fdca8db4b7276
//	go run ./cmd/gen-locales -extract -src lo   # refresh the extract
//	go run ./cmd/gen-locales                    # write the Go files
package main
