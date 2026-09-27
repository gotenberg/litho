package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The tests here pin that the options given to [NewWriter] are defaults for
// [Writer.WriteTo], and that an option given to WriteTo overrides the same
// setting. NewWriter read [WithTempDir] alone and dropped every other option,
// so NewWriter(r, WithEncryption(cfg)) wrote a plaintext file and returned no
// error.

// twinPagesPDF builds two pages whose content streams are byte-identical and
// the only streams in the file, under a PDF 1.4 header, so a deduplication, a
// raised version, a cross-reference stream and an object stream each show in
// the output bytes.
func twinPagesPDF() []byte {
	const content = "BT /F1 12 Tf 100 700 Td (twin) Tj ET\n"
	stream := fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
	page := func(contents int) string {
		return fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 7 0 R >> >> >>", contents)
	}
	data := buildTestPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		page(5),
		page(6),
		stream,
		stream,
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Title (Twins) >>",
	}, "/Info 8 0 R")
	// Same length as the header buildTestPDF writes, so the offsets hold.
	return bytes.Replace(data, []byte("%PDF-1.7"), []byte("%PDF-1.4"), 1)
}

// writeWithOptions writes data through a Writer given atNew, after edit
// queued its edits, with atWrite passed to [Writer.WriteTo].
func writeWithOptions(t *testing.T, data []byte, edit func(*Writer), atNew, atWrite []WriteOption) ([]byte, error) {
	t.Helper()

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	w := NewWriter(r, atNew...)
	t.Cleanup(func() { _ = w.Close() })
	if edit != nil {
		edit(w)
	}
	var out bytes.Buffer
	err = w.WriteTo(ctx, &out, atWrite...)
	return out.Bytes(), err
}

// passwordOf is the user password [TestNewWriterOptionsAreWriteToDefaults]
// encrypts under for the value v of the option.
func passwordOf(v int) string { return fmt.Sprintf("user-%d", v) }

// TestNewWriterOptionsAreWriteToDefaults pins every write option a Writer
// honours given to [NewWriter] alone, to [Writer.WriteTo] alone and to both, with
// a control given to neither. Each option has two values where it takes one:
// NewWriter gets the first, WriteTo the second, and the output must show the
// first when only NewWriter had it and the second otherwise.
func TestNewWriterOptionsAreWriteToDefaults(t *testing.T) {
	t.Parallel()

	xrefStream := regexp.MustCompile(`/Type\s*/XRef\b`)
	objStm := regexp.MustCompile(`/Type\s*/ObjStm\b`)
	// Clearing the metadata forces a full rewrite, which the version, packing
	// and deduplication options need, and leaves both content streams.
	rewrite := func(w *Writer) { w.ClearMetadata() }

	for _, tc := range []struct {
		name string
		edit func(*Writer)
		// opt returns the option for value 1 (NewWriter's) or 2 (WriteTo's).
		opt func(v int) WriteOption
		// check inspects out for value v, 0 when no option was given.
		check func(t *testing.T, src, out []byte, v int)
	}{
		{
			name: "WithEncryption",
			opt: func(v int) WriteOption {
				return WithEncryption(EncryptConfig{UserPassword: passwordOf(v), OwnerPassword: "owner", Permissions: -1})
			},
			check: func(t *testing.T, _, out []byte, v int) {
				ctx := context.Background()
				r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
				if v == 0 {
					if err != nil {
						t.Fatalf("the control does not open: %v", err)
					}
					if r.IsEncrypted() {
						t.Error("the control is encrypted")
					}
					return
				}
				if err == nil {
					t.Fatal("the output opens with no password: it is not encrypted")
				}
				other := 3 - v
				if _, err := Open(ctx, bytes.NewReader(out), int64(len(out)), WithPassword(passwordOf(other))); err == nil {
					t.Errorf("the output opens under %q, the encryption the other option asked for", passwordOf(other))
				}
				if _, err := Open(ctx, bytes.NewReader(out), int64(len(out)), WithPassword(passwordOf(v))); err != nil {
					t.Errorf("the output does not open under %q: %v", passwordOf(v), err)
				}
			},
		},
		{
			name: "WithFullRewrite",
			opt:  func(int) WriteOption { return WithFullRewrite() },
			check: func(t *testing.T, src, out []byte, v int) {
				if passedThrough := bytes.Equal(out, src); passedThrough != (v == 0) {
					t.Errorf("the output is the source byte for byte = %v, want %v", passedThrough, v == 0)
				}
			},
		},
		{
			name: "WithVersion",
			edit: rewrite,
			opt: func(v int) WriteOption {
				return WithVersion([]string{"", "1.6", "2.0"}[v])
			},
			check: func(t *testing.T, _, out []byte, v int) {
				want := []string{"%PDF-1.4", "%PDF-1.6", "%PDF-2.0"}[v]
				if !bytes.HasPrefix(out, []byte(want)) {
					t.Errorf("the output declares %q, want %s", pdfHeader(out), want)
				}
			},
		},
		{
			name: "WithXRefStream",
			edit: rewrite,
			opt:  func(int) WriteOption { return WithXRefStream() },
			check: func(t *testing.T, _, out []byte, v int) {
				if got := xrefStream.Match(out); got != (v != 0) {
					t.Errorf("the output has a cross-reference stream = %v, want %v", got, v != 0)
				}
			},
		},
		{
			name: "WithObjectStreams",
			edit: rewrite,
			opt:  func(int) WriteOption { return WithObjectStreams() },
			check: func(t *testing.T, _, out []byte, v int) {
				if got := objStm.Match(out); got != (v != 0) {
					t.Errorf("the output has an object stream = %v, want %v", got, v != 0)
				}
			},
		},
		{
			name: "WithDedup",
			edit: rewrite,
			opt:  func(int) WriteOption { return WithDedup() },
			check: func(t *testing.T, _, out []byte, v int) {
				want := 2
				if v != 0 {
					want = 1
				}
				if got := bytes.Count(out, []byte("endstream")); got != want {
					t.Errorf("the output holds %d streams, want %d", got, want)
				}
			},
		},
	} {
		for _, pl := range []struct {
			name           string
			atNew, atWrite int
			want           int
		}{
			{name: "neither", want: 0},
			{name: "NewWriter", atNew: 1, want: 1},
			{name: "WriteTo", atWrite: 2, want: 2},
			{name: "both", atNew: 1, atWrite: 2, want: 2},
		} {
			t.Run(tc.name+"/"+pl.name, func(t *testing.T) {
				t.Parallel()

				data := twinPagesPDF()
				var atNew, atWrite []WriteOption
				if pl.atNew != 0 {
					atNew = append(atNew, tc.opt(pl.atNew))
				}
				if pl.atWrite != 0 {
					atWrite = append(atWrite, tc.opt(pl.atWrite))
				}
				out, err := writeWithOptions(t, data, tc.edit, atNew, atWrite)
				if err != nil {
					t.Fatalf("WriteTo: %v", err)
				}
				tc.check(t, data, out, pl.want)
			})
		}
	}
}

// TestNewWriterLoggerIsAWriteToDefault pins [WithWriteLogger] given to
// [NewWriter]: the write logs to it, unless [Writer.WriteTo] names a logger of
// its own, which then takes every record.
func TestNewWriterLoggerIsAWriteToDefault(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                 string
		atNew, atWrite       bool
		wantNew, wantWriteTo bool
	}{
		{name: "NewWriter", atNew: true, wantNew: true},
		{name: "WriteTo", atWrite: true, wantWriteTo: true},
		{name: "both", atNew: true, atWrite: true, wantWriteTo: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var fromNew, fromWriteTo bytes.Buffer
			logger := func(buf *bytes.Buffer) WriteOption {
				return WithWriteLogger(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
			}
			var atNew, atWrite []WriteOption
			if tc.atNew {
				atNew = append(atNew, logger(&fromNew))
			}
			if tc.atWrite {
				atWrite = append(atWrite, logger(&fromWriteTo))
			}
			if _, err := writeWithOptions(t, twinPagesPDF(), func(w *Writer) { w.ClearMetadata() }, atNew, atWrite); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			const record = "writer.WriteTo started"
			if got := bytes.Contains(fromNew.Bytes(), []byte(record)); got != tc.wantNew {
				t.Errorf("the logger given to NewWriter logged the write = %v, want %v", got, tc.wantNew)
			}
			if got := bytes.Contains(fromWriteTo.Bytes(), []byte(record)); got != tc.wantWriteTo {
				t.Errorf("the logger given to WriteTo logged the write = %v, want %v", got, tc.wantWriteTo)
			}
		})
	}
}

// TestNewWriterTempDirIsAWriteToDefault pins [WithTempDir] on the temp file
// [Writer.WriteTo] writes when it signs a rewrite: it goes where the option
// given to WriteTo says, and where the one given to [NewWriter] says when
// WriteTo names none. A directory that does not exist shows where the file
// was asked for, since WriteTo fails creating it.
func TestNewWriterTempDirIsAWriteToDefault(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		atNew, atWrite string
		wantMissing    bool
	}{
		{name: "missing at NewWriter", atNew: "missing", wantMissing: true},
		{name: "missing at WriteTo", atWrite: "missing", wantMissing: true},
		{name: "missing at NewWriter, present at WriteTo", atNew: "missing", atWrite: "present"},
		{name: "present at NewWriter, missing at WriteTo", atNew: "present", atWrite: "missing", wantMissing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "present"), 0o700); err != nil {
				t.Fatalf("Mkdir: %v", err)
			}
			var atNew, atWrite []WriteOption
			if tc.atNew != "" {
				atNew = append(atNew, WithTempDir(filepath.Join(root, tc.atNew)))
			}
			if tc.atWrite != "" {
				atWrite = append(atWrite, WithTempDir(filepath.Join(root, tc.atWrite)))
			}
			signer := newCMSTestSigner(t)
			out, err := writeWithOptions(t, twinPagesPDF(), func(w *Writer) {
				w.ClearMetadata()
				w.Sign(signer)
			}, atNew, atWrite)
			if tc.wantMissing {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("WriteTo returned %v, want the missing directory's os.ErrNotExist", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			checkSignedOutput(t, out, signer)
		})
	}
}

// TestNewWriterEncryptionMeetsTheWriteToGuards pins that the encryption given
// to [NewWriter] reaches every guard the encryption given to [Writer.WriteTo]
// or [Writer.Encrypt] reaches: a signature beside it is refused, a pipeline
// intermediate is written in the clear all the same, and a later
// [Writer.Encrypt] replaces it.
func TestNewWriterEncryptionMeetsTheWriteToGuards(t *testing.T) {
	t.Parallel()

	first := EncryptConfig{UserPassword: passwordOf(1), OwnerPassword: "owner", Permissions: -1}
	second := EncryptConfig{UserPassword: passwordOf(2), OwnerPassword: "owner", Permissions: -1}

	t.Run("a signature beside it is refused", func(t *testing.T) {
		t.Parallel()

		out, err := writeWithOptions(t, twinPagesPDF(), func(w *Writer) { w.Sign(newCMSTestSigner(t)) }, []WriteOption{WithEncryption(first)}, nil)
		if !errors.Is(err, ErrSignFullRewriteIncompatible) {
			t.Fatalf("WriteTo returned %v, want ErrSignFullRewriteIncompatible", err)
		}
		if len(out) != 0 {
			t.Errorf("WriteTo wrote %d bytes", len(out))
		}
	})

	t.Run("a signature beside it is refused on an intermediate", func(t *testing.T) {
		t.Parallel()

		_, err := writeWithOptions(t, twinPagesPDF(), func(w *Writer) { w.Sign(newCMSTestSigner(t)) }, []WriteOption{WithEncryption(first)}, []WriteOption{inTheClear()})
		if !errors.Is(err, ErrSignFullRewriteIncompatible) {
			t.Fatalf("WriteTo returned %v, want ErrSignFullRewriteIncompatible", err)
		}
	})

	t.Run("an intermediate is written in the clear", func(t *testing.T) {
		t.Parallel()

		out, err := writeWithOptions(t, twinPagesPDF(), nil, []WriteOption{WithEncryption(first)}, []WriteOption{inTheClear()})
		if err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		r, err := Open(context.Background(), bytes.NewReader(out), int64(len(out)))
		if err != nil {
			t.Fatalf("the intermediate does not open without a password: %v", err)
		}
		if r.IsEncrypted() {
			t.Error("the intermediate is encrypted")
		}
	})

	t.Run("a later Encrypt replaces it", func(t *testing.T) {
		t.Parallel()

		out, err := writeWithOptions(t, twinPagesPDF(), func(w *Writer) { w.Encrypt(second) }, []WriteOption{WithEncryption(first)}, nil)
		if err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		ctx := context.Background()
		if _, err := Open(ctx, bytes.NewReader(out), int64(len(out)), WithPassword(passwordOf(1))); err == nil {
			t.Error("the output opens under the encryption given to NewWriter")
		}
		if _, err := Open(ctx, bytes.NewReader(out), int64(len(out)), WithPassword(passwordOf(2))); err != nil {
			t.Errorf("the output does not open under the encryption Encrypt asked for: %v", err)
		}
	})
}
