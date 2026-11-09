package main

import (
	"slices"
	"testing"
)

// TestWeasyPrintModule pins which assert_pixels calls of a draw test module
// become fixtures: literal arguments, or names bound once to a string
// constant, at module level or in the test, and not a parameter pytest may
// fill.
func TestWeasyPrintModule(t *testing.T) {
	t.Parallel()

	src := `import pytest

GRID = '''
    RR
    RR  # a comment
'''
TWICE = '__'
TWICE = 'RR'
HTML = ('<style>@page { size: 2px }</style>'
        '<body style="background: red">')


@assert_no_logs
def test_named(assert_pixels):
    assert_pixels(GRID, HTML)


@pytest.mark.xfail
def test_two_calls(assert_pixels):
    local = """
        R_
        _R
    """
    assert_pixels(local, '<p>\x41é</p>')
    assert_pixels(GRID, r'<p>\n</p>')


@pytest.mark.parametrize('html', ['<p>'])
def test_parametrized(assert_pixels, html):
    assert_pixels(GRID, html)


def test_computed(assert_pixels):
    assert_pixels(TWICE, HTML)
    assert_pixels(GRID, f'<p>{1}</p>')
    assert_pixels(GRID, HTML.strip())
    assert_pixels(GRID, html=HTML)
    if True: other = 'x'


class TestNot:
    def test_method(self, assert_pixels):
        assert_pixels(GRID, HTML)
`
	pixels := map[string][]int{"_": {255, 255, 255}, "R": {255, 0, 0}}
	found, skipped, bad, err := weasyPrintModule("test_x.py", []byte(src), pixels)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range found {
		names = append(names, f.Name)
	}
	want := []string{"test_x__test_named", "test_x__test_two_calls", "test_x__test_two_calls_2"}
	if !slices.Equal(names, want) {
		t.Fatalf("fixtures %v, want %v", names, want)
	}
	if got := found[0]; got.HTML != `<style>@page { size: 2px }</style><body style="background: red">` ||
		!slices.Equal(got.Expected, []string{"RR", "RR"}) || got.XFail || got.Source != "tests/draw/test_x.py::test_named" {
		t.Errorf("test_named: %+v", got)
	}
	if got := found[1]; got.HTML != "<p>Aé</p>" || !slices.Equal(got.Expected, []string{"R_", "_R"}) || !got.XFail {
		t.Errorf("test_two_calls: %+v", got)
	}
	if got := found[2].HTML; got != `<p>\n</p>` {
		t.Errorf("a raw string keeps its backslashes: %q", got)
	}
	wantSkipped := []string{
		"test_x__test_parametrized",
		"test_x__test_computed", "test_x__test_computed_2", "test_x__test_computed_3", "test_x__test_computed_4",
	}
	if !slices.Equal(skipped, wantSkipped) || len(bad) != 0 {
		t.Errorf("skipped %v, bad %v, want %v and none", skipped, bad, wantSkipped)
	}
}

// TestPyJSONString pins Python's json escaping with ensure_ascii off.
func TestPyJSONString(t *testing.T) {
	t.Parallel()

	if got, want := pyJSONString("a\"\\\n\t\x01é< "), "\"a\\\"\\\\\\n\\t\\u0001é< \""; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
