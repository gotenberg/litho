package css

import (
	"reflect"
	"strconv"
	"testing"
	"unsafe"
)

// fieldStep is one step of the path from a [Style] to one of its values:
// a struct field, an array element, the first element of a slice of one
// element, or the nil-ness of a slice.
type fieldStep struct {
	kind  int
	index int
}

const (
	stepField = iota
	stepIndex
	stepSliceElem
	stepSliceNil
)

// fieldPath is a path from a [Style] to a value the comparison reads, and
// its name.
type fieldPath struct {
	name  string
	steps []fieldStep
}

// fieldPaths returns the path to every value of type t the comparison of
// [Style] reads: each field of each struct, each element of each array,
// the element of a slice of one element, and whether a slice is nil.
func fieldPaths(t reflect.Type, prefix fieldPath, out *[]fieldPath) {
	with := func(name string, s fieldStep) fieldPath {
		return fieldPath{name: prefix.name + name, steps: append(append([]fieldStep(nil), prefix.steps...), s)}
	}
	switch t.Kind() { //nolint:exhaustive // every other kind is a value the comparison reads whole
	case reflect.Struct:
		for i := range t.NumField() {
			fieldPaths(t.Field(i).Type, with("."+t.Field(i).Name, fieldStep{kind: stepField, index: i}), out)
		}
	case reflect.Array:
		for i := range t.Len() {
			fieldPaths(t.Elem(), with("["+strconv.Itoa(i)+"]", fieldStep{kind: stepIndex, index: i}), out)
		}
	case reflect.Slice:
		*out = append(*out, with(" nil", fieldStep{kind: stepSliceNil}))
		fieldPaths(t.Elem(), with("[0]", fieldStep{kind: stepSliceElem}), out)
	default:
		*out = append(*out, prefix)
	}
}

// settable returns v, a value inside a Style, as one the test can set,
// unexported fields included.
func settable(v reflect.Value) reflect.Value {
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem() //nolint:gosec // a test reaching the unexported fields of a Style it owns
}

// follow walks p from st, giving each slice on the way one zero element of
// its own, and returns the value at its end; for a stepSliceNil end, the
// slice.
func follow(st *Style, p fieldPath) reflect.Value {
	v := reflect.ValueOf(st).Elem()
	for _, s := range p.steps {
		switch s.kind {
		case stepField:
			v = settable(v.Field(s.index))
		case stepIndex:
			v = v.Index(s.index)
		case stepSliceElem:
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
			v = v.Index(0)
		case stepSliceNil:
		}
	}
	return v
}

// mutate changes the value v, or the nil-ness of the slice v when nilness
// is set.
func mutate(t *testing.T, v reflect.Value, nilness bool) {
	t.Helper()
	if nilness {
		if v.IsNil() {
			v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		} else {
			v.Set(reflect.Zero(v.Type()))
		}
		return
	}
	switch v.Kind() { //nolint:exhaustive // the kinds a Style holds; the default fails the test
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() + 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(v.Uint() + 1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(v.Float() + 1)
	case reflect.String:
		v.SetString(v.String() + "x")
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		} else {
			v.Set(reflect.Zero(v.Type()))
		}
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
	default:
		t.Fatalf("a Style holds a %s the test cannot change", v.Kind())
	}
}

// TestEqualStyleReadsEveryField changes each value of a Style in turn and
// fails when the generated comparison or hash misses it: a field added to
// Style without running go generate fails here.
func TestEqualStyleReadsEveryField(t *testing.T) {
	var paths []fieldPath
	fieldPaths(reflect.TypeFor[Style](), fieldPath{name: "Style"}, &paths)
	if len(paths) < 200 {
		t.Fatalf("only %d paths", len(paths))
	}
	for _, p := range paths {
		a, b := Initial(), Initial()
		follow(a, p)
		end := follow(b, p)
		if !equalStyle(a, b) || hashStyle(fnvOffset64, a) != hashStyle(fnvOffset64, b) {
			t.Errorf("%s: equal values compare or hash apart", p.name)
			continue
		}
		mutate(t, end, p.steps[len(p.steps)-1].kind == stepSliceNil)
		if equalStyle(a, b) || equalStyle(b, a) {
			t.Errorf("%s: the comparison does not read it", p.name)
		}
		if hashStyle(fnvOffset64, a) == hashStyle(fnvOffset64, b) {
			t.Errorf("%s: the hash does not read it", p.name)
		}
	}
}

func TestEqualStyleSlicesOfOneArray(t *testing.T) {
	fams := []string{"serif", "sans-serif"}
	a, b := Initial(), Initial()
	a.FontFamilies, b.FontFamilies = fams, fams[:1]
	if equalStyle(a, b) {
		t.Error("slices of one array and two lengths compare equal")
	}
	b.FontFamilies = fams
	if !equalStyle(a, b) {
		t.Error("one slice compares apart from itself")
	}
}
