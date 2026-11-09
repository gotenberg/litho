package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("sharedarray_2b", sharedarray2b) }

// sharedarray2b builds sharedarray_2b.pdf: pages whose direct /Contents arrays share a
// stream, one of them carrying an invalid rendering intent.
//
// ISO 32000-1, 7.7.3.3 lets /Contents be an array of streams, and nothing
// keeps two pages from naming the same stream in theirs. Aggressive remediation
// replaces the invalid intent (ISO 19005-2, 6.2.6) by rewriting the page's
// content. Written in place, into the array's first stream with the others
// emptied, the rewrite takes a later shared stream off the other page and,
// through a shared first stream, paints this page's content on it.
//
// Each page paints one square of its own and the shared square:
//
// | Page | /Contents  | Streams                                             | Answers    |
// | ---- | ---------- | --------------------------------------------------- | ---------- |
// | 1    | `[A S]`    | A: `/Bogus ri`, a square at 10; S: the shared square | 6.2.6 t1   |
// | 2    | `[B S]`    | B: a square at 60                                    | nothing    |
// | 3    | `[T C]`    | T: the shared square; C: `/Bogus ri`, a square at 10 | 6.2.6 t1   |
// | 4    | `[T D]`    | D: a square at 60                                    | nothing    |
// | 5    | `[E]`      | E: `/Bogus ri`, a square at 10, shared by no page    | 6.2.6 t1   |
//
// Page 2 must keep the shared square after page 1 is repaired, page 4 must not
// gain page 3's square, and page 5 is the control the in-place rewrite keeps.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sharedarray2b() ([]byte, error) {
	pdf := pdfgen.New()

	stream := func(ops string) obj {
		return pdf.MakeIndirect(pdf.Stream([]byte(ops + "\n")))
	}

	const (
		bad    = "/Bogus ri 0 g 10 10 30 30 re f"
		own    = "0 g 60 10 30 30 re f"
		shared = "0.5 g 10 60 80 30 re f"
	)

	page := func(contents ...any) {
		dd := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 100, 100),
			"Resources", d(),
			"Contents", a(contents...),
		)
		pdf.AddPage(pdf.Indirect(dd))
	}

	s := stream(shared)
	page(stream(bad), s)
	page(stream(own), s)

	t := stream(shared)
	page(t, stream(bad))
	page(t, stream(own))

	page(stream(bad))

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "1.7")
}
