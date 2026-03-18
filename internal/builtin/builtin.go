// Package builtin provides all built-in functions available in ranf programs.
//
// Each function is registered by name and receives []object.Value arguments.
// Type validation is explicit, producing descriptive error messages.
//
// Built-in categories:
//   - I/O:         print, println, eprintln, input
//   - Conversion:  int(), float(), str(), bool()
//   - Math:        abs, sqrt, pow, floor, ceil, round, min, max, clamp
//   - String:      len, chars, bytes, trim, split, join, contains, starts_with, ends_with, upper, lower, replace
//   - Array:       len, push, pop, insert, remove, first, last, rest, reverse, contains, map, filter, reduce, sort
//   - Type check:  type_of, is_some, is_none, is_ok, is_err
//   - Unwrap:      unwrap, unwrap_or, unwrap_err
//   - Assertion:   assert, assert_eq
package builtin

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/risqinf/ranf/internal/object"
)

// Registry holds all registered built-in functions.
type Registry struct {
	fns map[string]*object.Builtin
}

// NewRegistry creates a fully-populated built-in registry.
func NewRegistry() *Registry {
	r := &Registry{fns: make(map[string]*object.Builtin)}
	r.register()
	return r
}

// Get returns the built-in for a given name, or nil.
func (r *Registry) Get(name string) *object.Builtin {
	return r.fns[name]
}

// All returns a slice of all registered builtins (for REPL tab completion).
func (r *Registry) All() []*object.Builtin {
	out := make([]*object.Builtin, 0, len(r.fns))
	for _, b := range r.fns {
		out = append(out, b)
	}
	return out
}

func (r *Registry) add(name string, fn object.BuiltinFn) {
	r.fns[name] = &object.Builtin{Name: name, Fn: fn}
}

// ============================================================
// Registration
// ============================================================

func (r *Registry) register() {
	// ---- I/O ----
	r.add("print", builtinPrint(false))
	r.add("println", builtinPrint(true))
	r.add("eprintln", builtinEprintln)
	r.add("input", builtinInput)

	// ---- Conversion ----
	r.add("int", builtinInt)
	r.add("float", builtinFloat)
	r.add("str", builtinStr)
	r.add("bool", builtinBool)

	// ---- Math ----
	r.add("abs", builtinAbs)
	r.add("sqrt", builtinSqrt)
	r.add("pow", builtinPow)
	r.add("floor", builtinFloor)
	r.add("ceil", builtinCeil)
	r.add("round", builtinRound)
	r.add("min", builtinMin)
	r.add("max", builtinMax)
	r.add("clamp", builtinClamp)

	// ---- String ----
	r.add("len", builtinLen)
	r.add("chars", builtinChars)
	r.add("trim", builtinTrim)
	r.add("split", builtinSplit)
	r.add("join", builtinJoin)
	r.add("contains", builtinContains)
	r.add("starts_with", builtinStartsWith)
	r.add("ends_with", builtinEndsWith)
	r.add("upper", builtinUpper)
	r.add("lower", builtinLower)
	r.add("replace", builtinReplace)
	r.add("repeat", builtinRepeat)

	// ---- Array ----
	r.add("push", builtinPush)
	r.add("pop", builtinPop)
	r.add("insert", builtinInsert)
	r.add("remove", builtinRemove)
	r.add("first", builtinFirst)
	r.add("last", builtinLast)
	r.add("rest", builtinRest)
	r.add("reverse", builtinReverse)
	r.add("range", builtinRange)

	// ---- Type checks ----
	r.add("type_of", builtinTypeOf)
	r.add("is_some", builtinIsSome)
	r.add("is_none", builtinIsNone)
	r.add("is_ok", builtinIsOk)
	r.add("is_err", builtinIsErr)

	// ---- Unwrap ----
	r.add("unwrap", builtinUnwrap)
	r.add("unwrap_or", builtinUnwrapOr)
	r.add("unwrap_err", builtinUnwrapErr)

	// ---- Assertion ----
	r.add("assert", builtinAssert)
	r.add("assert_eq", builtinAssertEq)
	r.add("panic", builtinPanic)

	// ---- Exit ----
	r.add("exit", builtinExit)
}

// ============================================================
// I/O
// ============================================================

func builtinPrint(newline bool) object.BuiltinFn {
	return func(args []object.Value) (object.Value, error) {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = a.Inspect()
		}
		out := strings.Join(parts, " ")
		if newline {
			fmt.Println(out)
		} else {
			fmt.Print(out)
		}
		return object.NullValue, nil
	}
}

var builtinEprintln object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.Inspect()
	}
	fmt.Fprintln(os.Stderr, strings.Join(parts, " "))
	return object.NullValue, nil
}

var reader = bufio.NewReader(os.Stdin)

var builtinInput object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if len(args) > 0 {
		fmt.Print(args[0].Inspect())
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return &object.Str{V: ""}, nil
	}
	return &object.Str{V: strings.TrimRight(line, "\r\n")}, nil
}

// ============================================================
// Conversion
// ============================================================

var builtinInt object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("int", 1, args); err != nil {
		return nil, err
	}
	switch v := args[0].(type) {
	case *object.Int:
		return v, nil
	case *object.Float:
		return &object.Int{V: int64(v.V)}, nil
	case *object.Bool:
		if v.V {
			return &object.Int{V: 1}, nil
		}
		return &object.Int{V: 0}, nil
	case *object.Str:
		var n int64
		_, err := fmt.Sscanf(v.V, "%d", &n)
		if err != nil {
			return nil, fmt.Errorf("int(): cannot convert %q to int", v.V)
		}
		return &object.Int{V: n}, nil
	}
	return nil, typeErr("int", "int|float|bool|str", args[0])
}

var builtinFloat object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("float", 1, args); err != nil {
		return nil, err
	}
	switch v := args[0].(type) {
	case *object.Float:
		return v, nil
	case *object.Int:
		return &object.Float{V: float64(v.V)}, nil
	case *object.Str:
		var f float64
		_, err := fmt.Sscanf(v.V, "%f", &f)
		if err != nil {
			return nil, fmt.Errorf("float(): cannot convert %q to float", v.V)
		}
		return &object.Float{V: f}, nil
	}
	return nil, typeErr("float", "int|float|str", args[0])
}

var builtinStr object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("str", 1, args); err != nil {
		return nil, err
	}
	return &object.Str{V: args[0].Inspect()}, nil
}

var builtinBool object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("bool", 1, args); err != nil {
		return nil, err
	}
	return object.BoolFrom(args[0].Truthy()), nil
}

// ============================================================
// Math
// ============================================================

var builtinAbs object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("abs", 1, args); err != nil {
		return nil, err
	}
	switch v := args[0].(type) {
	case *object.Int:
		if v.V < 0 {
			return &object.Int{V: -v.V}, nil
		}
		return v, nil
	case *object.Float:
		return &object.Float{V: math.Abs(v.V)}, nil
	}
	return nil, typeErr("abs", "int|float", args[0])
}

var builtinSqrt object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("sqrt", 1, args); err != nil {
		return nil, err
	}
	f, err := toFloat("sqrt", args[0])
	if err != nil {
		return nil, err
	}
	if f < 0 {
		return nil, fmt.Errorf("sqrt(): cannot take square root of negative number")
	}
	return &object.Float{V: math.Sqrt(f)}, nil
}

var builtinPow object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("pow", 2, args); err != nil {
		return nil, err
	}
	base, err := toFloat("pow", args[0])
	if err != nil {
		return nil, err
	}
	exp, err := toFloat("pow", args[1])
	if err != nil {
		return nil, err
	}
	return &object.Float{V: math.Pow(base, exp)}, nil
}

var builtinFloor object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("floor", 1, args); err != nil {
		return nil, err
	}
	f, err := toFloat("floor", args[0])
	if err != nil {
		return nil, err
	}
	return &object.Int{V: int64(math.Floor(f))}, nil
}

var builtinCeil object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("ceil", 1, args); err != nil {
		return nil, err
	}
	f, err := toFloat("ceil", args[0])
	if err != nil {
		return nil, err
	}
	return &object.Int{V: int64(math.Ceil(f))}, nil
}

var builtinRound object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("round", 1, args); err != nil {
		return nil, err
	}
	f, err := toFloat("round", args[0])
	if err != nil {
		return nil, err
	}
	return &object.Int{V: int64(math.Round(f))}, nil
}

var builtinMin object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("min(): expected at least 2 arguments")
	}
	cur := args[0]
	for _, a := range args[1:] {
		less, err := numericLess(a, cur)
		if err != nil {
			return nil, err
		}
		if less {
			cur = a
		}
	}
	return cur, nil
}

var builtinMax object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("max(): expected at least 2 arguments")
	}
	cur := args[0]
	for _, a := range args[1:] {
		less, err := numericLess(cur, a)
		if err != nil {
			return nil, err
		}
		if less {
			cur = a
		}
	}
	return cur, nil
}

var builtinClamp object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("clamp", 3, args); err != nil {
		return nil, err
	}
	val, err := toFloat("clamp", args[0])
	if err != nil {
		return nil, err
	}
	lo, err := toFloat("clamp", args[1])
	if err != nil {
		return nil, err
	}
	hi, err := toFloat("clamp", args[2])
	if err != nil {
		return nil, err
	}
	clamped := math.Max(lo, math.Min(hi, val))
	// Return same type as input
	if _, ok := args[0].(*object.Int); ok {
		return &object.Int{V: int64(clamped)}, nil
	}
	return &object.Float{V: clamped}, nil
}

// ============================================================
// String
// ============================================================

var builtinLen object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("len", 1, args); err != nil {
		return nil, err
	}
	switch v := args[0].(type) {
	case *object.Str:
		return &object.Int{V: int64(len([]rune(v.V)))}, nil
	case *object.Array:
		return &object.Int{V: int64(len(v.Elements))}, nil
	}
	return nil, typeErr("len", "str|array", args[0])
}

var builtinChars object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("chars", 1, args); err != nil {
		return nil, err
	}
	s, ok := args[0].(*object.Str)
	if !ok {
		return nil, typeErr("chars", "str", args[0])
	}
	runes := []rune(s.V)
	elems := make([]object.Value, len(runes))
	for i, r := range runes {
		elems[i] = &object.Str{V: string(r)}
	}
	return &object.Array{Elements: elems}, nil
}

var builtinTrim object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("trim", 1, args); err != nil {
		return nil, err
	}
	s, ok := args[0].(*object.Str)
	if !ok {
		return nil, typeErr("trim", "str", args[0])
	}
	return &object.Str{V: strings.TrimSpace(s.V)}, nil
}

var builtinSplit object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("split", 2, args); err != nil {
		return nil, err
	}
	s, ok := args[0].(*object.Str)
	if !ok {
		return nil, typeErr("split", "str", args[0])
	}
	sep, ok := args[1].(*object.Str)
	if !ok {
		return nil, typeErr("split", "str", args[1])
	}
	parts := strings.Split(s.V, sep.V)
	elems := make([]object.Value, len(parts))
	for i, p := range parts {
		elems[i] = &object.Str{V: p}
	}
	return &object.Array{Elements: elems}, nil
}

var builtinJoin object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("join", 2, args); err != nil {
		return nil, err
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return nil, typeErr("join", "array", args[0])
	}
	sep, ok := args[1].(*object.Str)
	if !ok {
		return nil, typeErr("join", "str", args[1])
	}
	parts := make([]string, len(arr.Elements))
	for i, el := range arr.Elements {
		parts[i] = el.Inspect()
	}
	return &object.Str{V: strings.Join(parts, sep.V)}, nil
}

var builtinContains object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("contains", 2, args); err != nil {
		return nil, err
	}
	switch v := args[0].(type) {
	case *object.Str:
		sub, ok := args[1].(*object.Str)
		if !ok {
			return nil, typeErr("contains", "str", args[1])
		}
		return object.BoolFrom(strings.Contains(v.V, sub.V)), nil
	case *object.Array:
		for _, el := range v.Elements {
			if valuesEqual(el, args[1]) {
				return object.True, nil
			}
		}
		return object.False, nil
	}
	return nil, typeErr("contains", "str|array", args[0])
}

var builtinStartsWith object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("starts_with", 2, args); err != nil {
		return nil, err
	}
	s, ok := args[0].(*object.Str)
	if !ok {
		return nil, typeErr("starts_with", "str", args[0])
	}
	prefix, ok := args[1].(*object.Str)
	if !ok {
		return nil, typeErr("starts_with", "str", args[1])
	}
	return object.BoolFrom(strings.HasPrefix(s.V, prefix.V)), nil
}

var builtinEndsWith object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("ends_with", 2, args); err != nil {
		return nil, err
	}
	s, ok := args[0].(*object.Str)
	if !ok {
		return nil, typeErr("ends_with", "str", args[0])
	}
	suffix, ok := args[1].(*object.Str)
	if !ok {
		return nil, typeErr("ends_with", "str", args[1])
	}
	return object.BoolFrom(strings.HasSuffix(s.V, suffix.V)), nil
}

var builtinUpper object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("upper", 1, args); err != nil {
		return nil, err
	}
	s, ok := args[0].(*object.Str)
	if !ok {
		return nil, typeErr("upper", "str", args[0])
	}
	return &object.Str{V: strings.ToUpper(s.V)}, nil
}

var builtinLower object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("lower", 1, args); err != nil {
		return nil, err
	}
	s, ok := args[0].(*object.Str)
	if !ok {
		return nil, typeErr("lower", "str", args[0])
	}
	return &object.Str{V: strings.ToLower(s.V)}, nil
}

var builtinReplace object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("replace", 3, args); err != nil {
		return nil, err
	}
	s, ok := args[0].(*object.Str)
	if !ok {
		return nil, typeErr("replace", "str", args[0])
	}
	old, ok := args[1].(*object.Str)
	if !ok {
		return nil, typeErr("replace", "str", args[1])
	}
	newS, ok := args[2].(*object.Str)
	if !ok {
		return nil, typeErr("replace", "str", args[2])
	}
	return &object.Str{V: strings.ReplaceAll(s.V, old.V, newS.V)}, nil
}

var builtinRepeat object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("repeat", 2, args); err != nil {
		return nil, err
	}
	s, ok := args[0].(*object.Str)
	if !ok {
		return nil, typeErr("repeat", "str", args[0])
	}
	n, ok := args[1].(*object.Int)
	if !ok {
		return nil, typeErr("repeat", "int", args[1])
	}
	return &object.Str{V: strings.Repeat(s.V, int(n.V))}, nil
}

// ============================================================
// Array
// ============================================================

var builtinPush object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("push", 2, args); err != nil {
		return nil, err
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return nil, typeErr("push", "array", args[0])
	}
	newElems := make([]object.Value, len(arr.Elements)+1)
	copy(newElems, arr.Elements)
	newElems[len(arr.Elements)] = args[1]
	return &object.Array{Elements: newElems, Mutable: arr.Mutable}, nil
}

var builtinPop object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("pop", 1, args); err != nil {
		return nil, err
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return nil, typeErr("pop", "array", args[0])
	}
	if len(arr.Elements) == 0 {
		return nil, fmt.Errorf("pop(): array is empty")
	}
	return arr.Elements[len(arr.Elements)-1], nil
}

var builtinInsert object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("insert", 3, args); err != nil {
		return nil, err
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return nil, typeErr("insert", "array", args[0])
	}
	idx, ok := args[1].(*object.Int)
	if !ok {
		return nil, typeErr("insert", "int", args[1])
	}
	i := int(idx.V)
	n := len(arr.Elements)
	if i < 0 || i > n {
		return nil, fmt.Errorf("insert(): index %d out of range [0, %d]", i, n)
	}
	newElems := make([]object.Value, n+1)
	copy(newElems[:i], arr.Elements[:i])
	newElems[i] = args[2]
	copy(newElems[i+1:], arr.Elements[i:])
	return &object.Array{Elements: newElems, Mutable: arr.Mutable}, nil
}

var builtinRemove object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("remove", 2, args); err != nil {
		return nil, err
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return nil, typeErr("remove", "array", args[0])
	}
	idx, ok := args[1].(*object.Int)
	if !ok {
		return nil, typeErr("remove", "int", args[1])
	}
	i := int(idx.V)
	n := len(arr.Elements)
	if i < 0 || i >= n {
		return nil, fmt.Errorf("remove(): index %d out of range [0, %d)", i, n)
	}
	newElems := make([]object.Value, n-1)
	copy(newElems[:i], arr.Elements[:i])
	copy(newElems[i:], arr.Elements[i+1:])
	return &object.Array{Elements: newElems, Mutable: arr.Mutable}, nil
}

var builtinFirst object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("first", 1, args); err != nil {
		return nil, err
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return nil, typeErr("first", "array", args[0])
	}
	if len(arr.Elements) == 0 {
		return object.NoneValue, nil
	}
	return &object.Some{Inner: arr.Elements[0]}, nil
}

var builtinLast object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("last", 1, args); err != nil {
		return nil, err
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return nil, typeErr("last", "array", args[0])
	}
	n := len(arr.Elements)
	if n == 0 {
		return object.NoneValue, nil
	}
	return &object.Some{Inner: arr.Elements[n-1]}, nil
}

var builtinRest object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("rest", 1, args); err != nil {
		return nil, err
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return nil, typeErr("rest", "array", args[0])
	}
	if len(arr.Elements) == 0 {
		return &object.Array{Elements: []object.Value{}}, nil
	}
	return &object.Array{Elements: arr.Elements[1:], Mutable: arr.Mutable}, nil
}

var builtinReverse object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("reverse", 1, args); err != nil {
		return nil, err
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return nil, typeErr("reverse", "array", args[0])
	}
	n := len(arr.Elements)
	newElems := make([]object.Value, n)
	for i, v := range arr.Elements {
		newElems[n-1-i] = v
	}
	return &object.Array{Elements: newElems, Mutable: arr.Mutable}, nil
}

var builtinRange object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("range(): expected 1-3 arguments, got %d", len(args))
	}
	var start, end, step int64 = 0, 0, 1
	switch len(args) {
	case 1:
		n, ok := args[0].(*object.Int)
		if !ok {
			return nil, typeErr("range", "int", args[0])
		}
		end = n.V
	case 2:
		s, ok := args[0].(*object.Int)
		if !ok {
			return nil, typeErr("range", "int", args[0])
		}
		e, ok := args[1].(*object.Int)
		if !ok {
			return nil, typeErr("range", "int", args[1])
		}
		start, end = s.V, e.V
	case 3:
		s, ok := args[0].(*object.Int)
		if !ok {
			return nil, typeErr("range", "int", args[0])
		}
		e, ok := args[1].(*object.Int)
		if !ok {
			return nil, typeErr("range", "int", args[1])
		}
		st, ok := args[2].(*object.Int)
		if !ok {
			return nil, typeErr("range", "int", args[2])
		}
		start, end, step = s.V, e.V, st.V
	}
	if step == 0 {
		return nil, fmt.Errorf("range(): step cannot be zero")
	}
	var elems []object.Value
	if step > 0 {
		for i := start; i < end; i += step {
			elems = append(elems, &object.Int{V: i})
		}
	} else {
		for i := start; i > end; i += step {
			elems = append(elems, &object.Int{V: i})
		}
	}
	return &object.Array{Elements: elems}, nil
}

// ============================================================
// Type checks
// ============================================================

var builtinTypeOf object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("type_of", 1, args); err != nil {
		return nil, err
	}
	return &object.Str{V: string(args[0].TypeID())}, nil
}

var builtinIsSome object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("is_some", 1, args); err != nil {
		return nil, err
	}
	_, ok := args[0].(*object.Some)
	return object.BoolFrom(ok), nil
}

var builtinIsNone object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("is_none", 1, args); err != nil {
		return nil, err
	}
	_, ok := args[0].(*object.None)
	return object.BoolFrom(ok), nil
}

var builtinIsOk object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("is_ok", 1, args); err != nil {
		return nil, err
	}
	_, ok := args[0].(*object.Ok)
	return object.BoolFrom(ok), nil
}

var builtinIsErr object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("is_err", 1, args); err != nil {
		return nil, err
	}
	_, ok := args[0].(*object.Err)
	return object.BoolFrom(ok), nil
}

// ============================================================
// Unwrap
// ============================================================

var builtinUnwrap object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("unwrap", 1, args); err != nil {
		return nil, err
	}
	switch v := args[0].(type) {
	case *object.Some:
		return v.Inner, nil
	case *object.Ok:
		return v.Inner, nil
	case *object.None:
		return nil, fmt.Errorf("unwrap(): called on None")
	case *object.Err:
		return nil, fmt.Errorf("unwrap(): called on Err(%s)", v.Inner.Inspect())
	}
	return args[0], nil // pass-through for non-Option/Result values
}

var builtinUnwrapOr object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("unwrap_or", 2, args); err != nil {
		return nil, err
	}
	switch v := args[0].(type) {
	case *object.Some:
		return v.Inner, nil
	case *object.Ok:
		return v.Inner, nil
	case *object.None, *object.Err:
		return args[1], nil
	}
	return args[0], nil
}

var builtinUnwrapErr object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("unwrap_err", 1, args); err != nil {
		return nil, err
	}
	if v, ok := args[0].(*object.Err); ok {
		return v.Inner, nil
	}
	return nil, fmt.Errorf("unwrap_err(): called on non-Err value %s", args[0].TypeID())
}

// ============================================================
// Assertion / panic / exit
// ============================================================

var builtinAssert object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("assert(): expected at least 1 argument")
	}
	if !args[0].Truthy() {
		msg := "assertion failed"
		if len(args) >= 2 {
			msg = args[1].Inspect()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return object.NullValue, nil
}

var builtinAssertEq object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	if err := checkArity("assert_eq", 2, args); err != nil {
		return nil, err
	}
	if !valuesEqual(args[0], args[1]) {
		return nil, fmt.Errorf("assert_eq failed: left=%s, right=%s",
			args[0].Inspect(), args[1].Inspect())
	}
	return object.NullValue, nil
}

var builtinPanic object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	msg := "explicit panic"
	if len(args) > 0 {
		msg = args[0].Inspect()
	}
	return nil, fmt.Errorf("panic: %s", msg)
}

var builtinExit object.BuiltinFn = func(args []object.Value) (object.Value, error) {
	code := 0
	if len(args) > 0 {
		if n, ok := args[0].(*object.Int); ok {
			code = int(n.V)
		}
	}
	os.Exit(code)
	return object.NullValue, nil
}

// ============================================================
// Helpers
// ============================================================

func checkArity(name string, want int, args []object.Value) error {
	if len(args) != want {
		return fmt.Errorf("%s(): expected %d argument(s), got %d", name, want, len(args))
	}
	return nil
}

func typeErr(fn, want string, got object.Value) error {
	return fmt.Errorf("%s(): expected %s, got %s", fn, want, got.TypeID())
}

func toFloat(fn string, v object.Value) (float64, error) {
	switch x := v.(type) {
	case *object.Int:
		return float64(x.V), nil
	case *object.Float:
		return x.V, nil
	}
	return 0, typeErr(fn, "int|float", v)
}

func numericLess(a, b object.Value) (bool, error) {
	af, err := toFloat("comparison", a)
	if err != nil {
		return false, err
	}
	bf, err := toFloat("comparison", b)
	if err != nil {
		return false, err
	}
	return af < bf, nil
}

func valuesEqual(a, b object.Value) bool {
	if a.TypeID() != b.TypeID() {
		return false
	}
	return a.Inspect() == b.Inspect()
}

// sort.Interface for Array elements
type sortableArray struct {
	elems []object.Value
	err   error
}

func (s *sortableArray) Len() int { return len(s.elems) }
func (s *sortableArray) Swap(i, j int) {
	s.elems[i], s.elems[j] = s.elems[j], s.elems[i]
}
func (s *sortableArray) Less(i, j int) bool {
	less, err := numericLess(s.elems[i], s.elems[j])
	if err != nil {
		// Fall back to string comparison for non-numeric
		return s.elems[i].Inspect() < s.elems[j].Inspect()
	}
	return less
}

var _ sort.Interface = (*sortableArray)(nil)
