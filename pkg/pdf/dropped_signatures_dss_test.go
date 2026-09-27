package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// dssVRIKey is the /VRI key of a signature whose /Contents is <30820001>, as
// every signature of the fixtures here is: the uppercase hex SHA-1 of those
// bytes (ISO 32000-2, 12.8.4.3).
const dssVRIKey = "0C18A37BF7C4821E9F671B75282931292677869F"

// dssMarker opens every stream of the store [securityStoreObjs] builds, so a
// test finds any of them left in an unencrypted output.
const dssMarker = "litho-dss-payload"

// securityStoreObjs returns the objects of a document security store numbered
// from first: the /DSS dictionary, a certificate, an OCSP response and a CRL,
// and the /VRI dictionary whose one entry, keyed by [dssVRIKey], names the
// three.
func securityStoreObjs(first int) []string {
	stream := func(kind string) string {
		content := dssMarker + " " + kind
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
	}
	return []string{
		fmt.Sprintf("<< /Type /DSS /Certs [%d 0 R] /OCSPs [%d 0 R] /CRLs [%d 0 R] /VRI %d 0 R >>",
			first+1, first+2, first+3, first+4),
		stream("cert"),
		stream("ocsp"),
		stream("crl"),
		fmt.Sprintf("<< /%s << /Type /VRI /Cert [%d 0 R] /OCSP [%d 0 R] /CRL [%d 0 R] >> >>",
			dssVRIKey, first+1, first+2, first+3),
	}
}

// withSecurityStore returns objs, whose first object is the catalog, with the
// store [securityStoreObjs] builds appended and named by the catalog's /DSS.
func withSecurityStore(objs []string) []string {
	out := slices.Clone(objs)
	first := len(out) + 1
	out[0] = strings.TrimSuffix(strings.TrimSpace(out[0]), ">>") + fmt.Sprintf("/DSS %d 0 R >>", first)
	return append(out, securityStoreObjs(first)...)
}

// certifiedDSSPDF is [certifiedPDF] with a document security store.
func certifiedDSSPDF() []byte {
	return buildPDF(withSecurityStore(certifiedObjs()))
}

// checkNoSecurityStore checks the catalog of data, opened with password when
// it is not empty, names no /DSS, and that an unencrypted data holds none of
// the store's streams.
func checkNoSecurityStore(ctx context.Context, t *testing.T, data []byte, password string) {
	t.Helper()
	if dss, has := catalogOf(ctx, t, data, password)["DSS"]; has {
		t.Errorf("the output keeps /DSS %v, the validation data of signatures it does not hold", dss)
	}
	if password == "" && bytes.Contains(data, []byte(dssMarker)) {
		t.Error("the output holds a stream of the source's document security store")
	}
}

// checkSecurityStore checks data carries the store [securityStoreObjs] builds:
// a /DSS whose /VRI keeps the entry keyed by [dssVRIKey].
func checkSecurityStore(ctx context.Context, t *testing.T, data []byte) {
	t.Helper()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open output: %v", err)
	}
	defer r.Close()
	info, err := r.DSSInfo(ctx)
	if err != nil {
		t.Fatalf("DSSInfo: %v", err)
	}
	if info == nil {
		t.Fatal("the output lost its /DSS")
	}
	if info.VRICount != 1 {
		t.Errorf("/VRI holds %d entries, want the 1 keyed %s", info.VRICount, dssVRIKey)
	}
}

// TestWriter_FullRewriteDropsTheSecurityStore pins that a full rewrite of a
// signed document drops its /DSS with its signatures, for each edit that
// writes one.
//
// The store's certificates, OCSP responses, CRLs and /VRI entries validate the
// document's signatures (ISO 32000-2, 12.8.4.3; ETSI EN 319 142-1, 5.4.2),
// and a rewrite keeps none of them. The rewrite used to copy the store whole,
// validation data for signatures the output no longer holds.
func TestWriter_FullRewriteDropsTheSecurityStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	encrypt := EncryptConfig{UserPassword: "user", OwnerPassword: "owner", Algorithm: EncryptAES256, Permissions: -1}

	for _, tc := range []struct {
		name     string
		password string
		edit     func(t *testing.T, w *Writer) []WriteOption
	}{
		{"WithFullRewrite", "", func(*testing.T, *Writer) []WriteOption {
			return []WriteOption{WithFullRewrite()}
		}},
		{"DeletePages", "", func(t *testing.T, w *Writer) []WriteOption {
			if err := w.DeletePages(2); err != nil {
				t.Fatalf("DeletePages: %v", err)
			}
			return nil
		}},
		{"InsertPages", "", func(t *testing.T, w *Writer) []WriteOption {
			blank := openDeclaration(t, blankPagePDF(), false)
			t.Cleanup(func() { blank.Close() })
			if err := w.InsertPages(2, blank); err != nil {
				t.Fatalf("InsertPages: %v", err)
			}
			return nil
		}},
		{"FlattenForm", "", func(_ *testing.T, w *Writer) []WriteOption {
			w.FlattenForm()
			return nil
		}},
		{"WithEncryption", "user", func(*testing.T, *Writer) []WriteOption {
			return []WriteOption{WithEncryption(encrypt)}
		}},
		// The signature the Writer adds stays, but no store names it.
		{"Sign", "", func(t *testing.T, w *Writer) []WriteOption {
			w.Sign(newCMSTestSigner(t))
			if err := w.DeletePages(2); err != nil {
				t.Fatalf("DeletePages: %v", err)
			}
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, dropped := writeSigned(ctx, t, certifiedDSSPDF(), func(w *Writer) []WriteOption { return tc.edit(t, w) })
			checkNoSecurityStore(ctx, t, out, tc.password)
			if !slices.Equal(dropped, certifiedDropped) {
				t.Errorf("DroppedSignatures = %v, want %v", dropped, certifiedDropped)
			}
		})
	}
}

// signedPageTwoDSSPDF is a two-page document whose one signature, named by no
// /Perms entry, is presented by a widget on page 2 alone, with a document
// security store.
func signedPageTwoDSSPDF() []byte {
	return buildPDF(withSecurityStore([]string{
		// 1: catalog.
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /SigFlags 3 >> >>",
		// 2: pages.
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		// 3: page 1.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		// 4: page 2.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
		// 5: the signed signature field, merged with its widget.
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /V 6 0 R /Rect [50 700 250 750] /F 4 /P 4 0 R >>",
		// 6: the signature.
		"<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached " +
			"/ByteRange [0 100 200 300] /Contents <30820001> >>",
	}))
}

// TestWriter_RewriteDropsTheStoreOfSignaturesAnEditRemoved pins that the
// store goes when the edits before the rewrite already took every signature
// out: the file held them, the output does not, and the report names them.
func TestWriter_RewriteDropsTheStoreOfSignaturesAnEditRemoved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	out, dropped := writeSigned(ctx, t, signedPageTwoDSSPDF(), func(w *Writer) []WriteOption {
		if err := w.DeletePages(2); err != nil {
			t.Fatalf("DeletePages: %v", err)
		}
		return nil
	})
	checkNoSecurityStore(ctx, t, out, "")
	if want := []DroppedSignature{{Field: "approval"}}; !slices.Equal(dropped, want) {
		t.Errorf("DroppedSignatures = %v, want %v", dropped, want)
	}
}

// TestWriter_KeepsTheSecurityStoreItsSignaturesKeep pins the two writes that
// keep the store: an incremental update, which keeps every signature, and a
// rewrite of a document that holds no signature, whose store validates none
// the rewrite drops.
func TestWriter_KeepsTheSecurityStoreItsSignaturesKeep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("incremental update", func(t *testing.T) {
		t.Parallel()
		out, dropped := writeSigned(ctx, t, certifiedDSSPDF(), func(w *Writer) []WriteOption {
			if err := w.RotatePages(90, 1); err != nil {
				t.Fatalf("RotatePages: %v", err)
			}
			return nil
		})
		checkSecurityStore(ctx, t, out)
		if dropped != nil {
			t.Errorf("DroppedSignatures = %v, want none", dropped)
		}
	})

	t.Run("unsigned rewrite", func(t *testing.T) {
		t.Parallel()
		unsigned := buildPDF(withSecurityStore([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		}))
		out, dropped := writeSigned(ctx, t, unsigned, func(*Writer) []WriteOption {
			return []WriteOption{WithFullRewrite()}
		})
		checkSecurityStore(ctx, t, out)
		if len(dropped) != 0 {
			t.Errorf("DroppedSignatures = %v, want none", dropped)
		}
	})
}

// TestSplitMergeInsert_LeaveTheSecurityStoreOut pins that a split, a merge and
// a page insertion carry none of a signed source's store, as they carry none
// of its signatures.
func TestSplitMergeInsert_LeaveTheSecurityStoreOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("Split", func(t *testing.T) {
		t.Parallel()
		src := openDeclaration(t, certifiedDSSPDF(), false)
		defer src.Close()
		var out bytes.Buffer
		var dropped []DroppedSignature
		if err := Split(ctx, src, []int{1, 2}, &out, WithDroppedSignatures(&dropped)); err != nil {
			t.Fatalf("Split: %v", err)
		}
		checkNoSecurityStore(ctx, t, out.Bytes(), "")
		if !slices.Equal(dropped, certifiedDropped) {
			t.Errorf("Split dropped %v, want %v", dropped, certifiedDropped)
		}
	})

	t.Run("Merge", func(t *testing.T) {
		t.Parallel()
		src := openDeclaration(t, certifiedDSSPDF(), false)
		defer src.Close()
		blank := openDeclaration(t, blankPagePDF(), false)
		defer blank.Close()
		var out bytes.Buffer
		var dropped []DroppedSignature
		if err := Merge(ctx, []*Reader{src, blank}, &out, WithDroppedSignatures(&dropped)); err != nil {
			t.Fatalf("Merge: %v", err)
		}
		checkNoSecurityStore(ctx, t, out.Bytes(), "")
		if !slices.Equal(dropped, certifiedDropped) {
			t.Errorf("Merge dropped %v, want %v", dropped, certifiedDropped)
		}
	})

	t.Run("InsertPages", func(t *testing.T) {
		t.Parallel()
		src := openDeclaration(t, certifiedDSSPDF(), false)
		defer src.Close()
		out, dropped := writeSigned(ctx, t, blankPagePDF(), func(w *Writer) []WriteOption {
			if err := w.InsertPages(1, src); err != nil {
				t.Fatalf("InsertPages: %v", err)
			}
			return nil
		})
		checkNoSecurityStore(ctx, t, out, "")
		if !slices.Equal(dropped, certifiedDropped) {
			t.Errorf("InsertPages dropped %v, want %v", dropped, certifiedDropped)
		}
	})
}
