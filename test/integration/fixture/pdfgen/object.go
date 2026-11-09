package pdfgen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Object is any PDF object pdfgen can write: Null, Bool, Int, Real, Name,
// String, *Array, *Dict, *Stream and *Ref.
type Object interface {
	isObject()
}

// Null is the PDF null object. qpdf drops a dictionary entry whose value is
// null and keeps a null array element.
type Null struct{}

// Bool is a PDF boolean.
type Bool bool

// Int is a PDF integer, written in decimal.
type Int int64

// Real is a PDF real, kept as the exact token qpdf writes. Use Float for a
// value pikepdf would have converted from a Python float.
type Real string

// Name is a PDF name, held decoded and without its leading slash. The writer
// applies qpdf's #xx escaping.
type Name string

// String is a PDF string, held as raw bytes. The writer picks the literal
// or the hexadecimal form the way qpdf does. Use Text for a Python str.
type String []byte

func (Null) isObject()    {}
func (Bool) isObject()    {}
func (Int) isObject()     {}
func (Real) isObject()    {}
func (Name) isObject()    {}
func (String) isObject()  {}
func (*Array) isObject()  {}
func (*Dict) isObject()   {}
func (*Stream) isObject() {}
func (*Ref) isObject()    {}

// indirect is the identity of an indirect object. Its id is the object
// number in the source document, the one qpdf would hold before writing; the
// writer renumbers on output.
type indirect struct {
	id int
}

// N returns the name s. One leading slash is dropped, so N("/Helvetica") and
// N("Helvetica") are the same name, which keeps pikepdf's Name("/X") portable
// as is.
func N(s string) Name {
	return Name(strings.TrimPrefix(s, "/"))
}

// Float returns the real pikepdf writes for a Python float: qpdf formats it
// with six decimal places, then trims trailing zeros and a trailing point, so
// 0.5 is 0.5, 2.0 is 2 and 1/3 is 0.333333.
func Float(f float64) Real {
	s := strconv.FormatFloat(f, 'f', 6, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	return Real(s)
}

// Array is a PDF array. It is a reference type like pikepdf's Array: the same
// *Array placed in two containers is one shared object.
type Array struct {
	Items []Object
	ind   *indirect
}

// A builds an array, converting each value with Obj.
func A(items ...any) *Array {
	a := &Array{Items: make([]Object, 0, len(items))}
	for _, it := range items {
		a.Items = append(a.Items, Obj(it))
	}
	return a
}

// Append adds values to the array, converting each with Obj.
func (a *Array) Append(items ...any) *Array {
	for _, it := range items {
		a.Items = append(a.Items, Obj(it))
	}
	return a
}

// Len returns the number of elements.
func (a *Array) Len() int { return len(a.Items) }

// Dict is a PDF dictionary. Keys are names without their slash. The writer
// sorts keys bytewise, as qpdf does, so insertion order never matters.
type Dict struct {
	m   map[string]Object
	ind *indirect
}

// D builds a dictionary from alternating keys and values: D("Type",
// N("Page"), "Count", 1). Keys are strings (an optional leading slash is
// dropped) and values are converted with Obj.
func D(kv ...any) *Dict {
	if len(kv)%2 != 0 {
		panic("pdfgen.D: odd number of arguments")
	}
	d := &Dict{m: make(map[string]Object, len(kv)/2)}
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			panic(fmt.Sprintf("pdfgen.D: key %v is %T, not string", kv[i], kv[i]))
		}
		d.Set(k, kv[i+1]) //nolint:gosec // i+1 < len(kv): len(kv) is even, checked above.
	}
	return d
}

// Set stores v under key, converting it with Obj, and returns d.
func (d *Dict) Set(key string, v any) *Dict {
	if d.m == nil {
		d.m = map[string]Object{}
	}
	d.m[strings.TrimPrefix(key, "/")] = Obj(v)
	return d
}

// Get returns the value under key, or nil.
func (d *Dict) Get(key string) Object {
	return d.m[strings.TrimPrefix(key, "/")]
}

// Has reports whether key is present.
func (d *Dict) Has(key string) bool {
	_, ok := d.m[strings.TrimPrefix(key, "/")]
	return ok
}

// Del removes key.
func (d *Dict) Del(key string) {
	delete(d.m, strings.TrimPrefix(key, "/"))
}

// Keys returns the keys in the order qpdf writes them.
func (d *Dict) Keys() []string {
	keys := make([]string, 0, len(d.m))
	for k := range d.m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Len returns the number of entries.
func (d *Dict) Len() int { return len(d.m) }

// Stream is a PDF stream. Like a qpdf stream it is always indirect: Doc.Stream
// numbers it on creation. Data is written verbatim; the writer adds /Length.
type Stream struct {
	Dict
	Data []byte
}

// Ref is an indirect object that is not a dictionary, array or stream, made
// by Doc.MakeIndirect on a scalar.
type Ref struct {
	Obj Object
	ind *indirect
}

// Obj converts a Go value to an Object: nil is Null, bool is Bool, the
// integer kinds are Int, float64 and float32 are Float, []any is an array and
// an Object passes through. A Go string panics, since it could be a name or a
// string: wrap it in N, Text or String.
func Obj(v any) Object {
	switch x := v.(type) {
	case nil:
		return Null{}
	case Object:
		if x == nil {
			return Null{}
		}
		return x
	case bool:
		return Bool(x)
	case int:
		return Int(x)
	case int8:
		return Int(x)
	case int16:
		return Int(x)
	case int32:
		return Int(x)
	case int64:
		return Int(x)
	case uint8:
		return Int(x)
	case uint16:
		return Int(x)
	case uint32:
		return Int(x)
	case uint:
		return Int(x) //nolint:gosec // fixture values stay far below 2^63.
	case float64:
		return Float(x)
	case float32:
		return Float(float64(x))
	case []any:
		return A(x...)
	case []int:
		a := &Array{}
		for _, i := range x {
			a.Items = append(a.Items, Int(i))
		}
		return a
	case []float64:
		a := &Array{}
		for _, f := range x {
			a.Items = append(a.Items, Float(f))
		}
		return a
	case []Object:
		return &Array{Items: append([]Object(nil), x...)}
	case string:
		panic(fmt.Sprintf("pdfgen: ambiguous Go string %q: use N, Text or String", x))
	default:
		panic(fmt.Sprintf("pdfgen: cannot convert %T to a PDF object", v))
	}
}

// indOf returns the indirect identity of o, or nil for a direct object.
func indOf(o Object) *indirect {
	switch x := o.(type) {
	case *Dict:
		return x.ind
	case *Array:
		return x.ind
	case *Stream:
		return x.ind
	case *Ref:
		return x.ind
	}
	return nil
}

// resolve follows a *Ref to its value.
func resolve(o Object) Object {
	if r, ok := o.(*Ref); ok {
		return r.Obj
	}
	return o
}

// isNull reports whether o is null, directly or through a *Ref.
func isNull(o Object) bool {
	if o == nil {
		return true
	}
	_, ok := resolve(o).(Null)
	return ok
}

// IsIndirect reports whether o is an indirect object.
func IsIndirect(o Object) bool {
	return indOf(o) != nil
}
