package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// The pinned sources: LibreOffice core at the commit of the
// libreoffice-26.8.2.2 tag, the release of the pinned Gotenberg image,
// and the digest of the files the extract reads ([readSources]).
const (
	commit        = "9dea406ab7695df6325b11b0e19fdca8db4b7276"
	sourcesSHA256 = "54c63a2679b346930abf9142153435a0fe6c4dfe13aee6d81ec0575d93aa7a2d"
)

// The files the command reads and writes, relative to the module root.
const (
	extractPath = "cmd/gen-locales/data/localedata.json"
	tablePath   = "pkg/internal/numfmt/locale_gen.go"
	modelsPath  = "pkg/models/locale_gen.go"
)

// extractFile is data/localedata.json.
type extractFile struct {
	// Commit is the LibreOffice core commit the records come from.
	Commit string `json:"commit"`
	// Locales are the records, by file name.
	Locales []localeRecord `json:"locales"`
}

// localeRecord is one locale, as its file and the files it refers to
// state it. Each field names its element.
type localeRecord struct {
	// File is the locale file's name, without .xml.
	File string `json:"file"`
	// Tag is the BCP 47 tag: LC_INFO's Variant, else LangID and CountryID.
	Tag string `json:"tag"`
	// Language and Country are LC_INFO's DefaultName of each.
	Language string `json:"language"`
	Country  string `json:"country,omitempty"`
	// LCIDs are the Windows locale identifiers isolang.cxx gives the tag,
	// the first the one a format code tag is written with.
	LCIDs []uint16 `json:"lcids,omitempty"`
	// LC_CTYPE/Separators.
	Decimal      string `json:"decimal"`
	DecimalAlt   string `json:"decimalAlt,omitempty"`
	Group        string `json:"group"`
	DateSep      string `json:"dateSep"`
	TimeSep      string `json:"timeSep"`
	Time100Sep   string `json:"time100Sep"`
	ListSep      string `json:"listSep"`
	DayOfWeekSep string `json:"dayOfWeekSep"`
	// LC_CTYPE TimeAM and TimePM.
	AM string `json:"am"`
	PM string `json:"pm"`
	// LC_MISC/ReservedWords trueWord and falseWord.
	True  string `json:"true"`
	False string `json:"false"`
	// Grouping are the sizes of the digit groups from the decimal point,
	// by LibreOffice's rule for the country.
	Grouping []int `json:"grouping"`
	// LC_CALENDAR, the Gregorian calendar: the abbreviated and full names
	// of DaysOfWeek, from Sunday, MonthsOfYear, GenitiveMonths and
	// PartitiveMonths (nil when they are the names they fall back on).
	Days      [7][2]string   `json:"days"`
	Months    [12][2]string  `json:"months"`
	Genitive  *[12][2]string `json:"genitive,omitempty"`
	Partitive *[12][2]string `json:"partitive,omitempty"`
	// Narrow are MonthsOfYear's DefaultNarrowName, nil when the locale
	// states none: a narrow name is then the full name's first letter.
	Narrow *[12]string `json:"narrow,omitempty"`
	// Calendar is the default calendar's unoid when it is not Gregorian.
	Calendar string `json:"calendar,omitempty"`
	// Currency is LC_CURRENCY's default CurrencySymbol.
	Currency string `json:"currency"`
	// CurrencyDigits is that currency's DecimalPlaces.
	CurrencyDigits int `json:"currencyDigits"`
	// Codes are LC_FORMAT's format codes, by formatindex, and the
	// default formats of the DATE, TIME and DATE_TIME usages, in
	// SpreadsheetML syntax.
	Codes map[string]string `json:"codes"`
	// Defaults are the formatindex of the default format of each usage.
	Defaults map[string]int `json:"defaults"`
	// Acceptance are LC_FORMAT's DateAcceptancePattern.
	Acceptance []string `json:"acceptance,omitempty"`
	// Unsupported are the format codes, by formatindex, that hold a
	// keyword numfmt does not read (an era, a calendar letter): the table
	// leaves them out.
	Unsupported []string `json:"unsupported,omitempty"`
}

func main() {
	doExtract := flag.Bool("extract", false, "read LibreOffice core at -src and write the extract")
	src := flag.String("src", "", "a checkout of LibreOffice core at the pinned commit (with -extract)")
	root := flag.String("root", ".", "the module root")
	flag.Parse()
	if err := run(*doExtract, *src, *root); err != nil {
		fmt.Fprintln(os.Stderr, "gen-locales:", err)
		os.Exit(1)
	}
}

// run performs the extract step or the generate step.
func run(doExtract bool, src, root string) error {
	if doExtract {
		if src == "" {
			return fmt.Errorf("-extract needs -src")
		}
		s, err := readSources(src)
		if err != nil {
			return err
		}
		x, err := extract(s)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", " ")
		if err := enc.Encode(x); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, extractPath), buf.Bytes(), 0o600)
	}
	data, err := os.ReadFile(filepath.Join(root, extractPath))
	if err != nil {
		return err
	}
	table, models, err := generate(data)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, tablePath), table, 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, modelsPath), models, 0o600)
}
