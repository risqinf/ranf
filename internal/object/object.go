// Package object defines the runtime value type system for ranf.
//
// Every value in a running ranf program is represented as a Value interface.
// The concrete types cover: Int, Float, Str, Bool, Null, Array, Struct,
// Function, Builtin, Some, None, Ok, Err.
//
// Control flow signals (Return, Break, Continue) are also values so that
// they can propagate cleanly through the evaluator/VM call stack.
package object

import (
	"fmt"
	"strings"
)

// ============================================================
// Type IDs
// ============================================================

// TypeID identifies the concrete type of a Value.
type TypeID string

const (
	TypeInt      TypeID = "int"
	TypeFloat    TypeID = "float"
	TypeStr      TypeID = "str"
	TypeBool     TypeID = "bool"
	TypeNull     TypeID = "null"
	TypeArray    TypeID = "array"
	TypeStruct   TypeID = "struct"
	TypeFunction TypeID = "fn"
	TypeBuiltin  TypeID = "builtin"
	TypeSome     TypeID = "Some"
	TypeNone     TypeID = "None"
	TypeOk       TypeID = "Ok"
	TypeErr      TypeID = "Err"
	// Internal control signals
	TypeReturn   TypeID = "<return>"
	TypeBreak    TypeID = "<break>"
	TypeContinue TypeID = "<continue>"
)

// ============================================================
// Value interface
// ============================================================

// Value is implemented by every ranf runtime value.
type Value interface {
	TypeID() TypeID
	Inspect() string // human-readable representation
	Truthy() bool    // truthiness for conditionals
}

// ============================================================
// Primitive values
// ============================================================

// Int is a 64-bit signed integer.
type Int struct{ V int64 }

func (v *Int) TypeID() TypeID { return TypeInt }
func (v *Int) Truthy() bool   { return v.V != 0 }
func (v *Int) Inspect() string {
	return fmt.Sprintf("%d", v.V)
}

// Float is a 64-bit IEEE-754 float.
type Float struct{ V float64 }

func (v *Float) TypeID() TypeID { return TypeFloat }
func (v *Float) Truthy() bool   { return v.V != 0.0 }
func (v *Float) Inspect() string {
	s := fmt.Sprintf("%g", v.V)
	if !strings.Contains(s, ".") && !strings.Contains(s, "e") {
		s += ".0"
	}
	return s
}

// Str is a UTF-8 string.
type Str struct{ V string }

func (v *Str) TypeID() TypeID  { return TypeStr }
func (v *Str) Truthy() bool    { return len(v.V) > 0 }
func (v *Str) Inspect() string { return v.V }

// Bool is true or false.
type Bool struct{ V bool }

func (v *Bool) TypeID() TypeID { return TypeBool }
func (v *Bool) Truthy() bool   { return v.V }
func (v *Bool) Inspect() string {
	if v.V {
		return "true"
	}
	return "false"
}

// Singleton booleans — reuse to avoid allocations.
var (
	True  = &Bool{V: true}
	False = &Bool{V: false}
)

// BoolFrom returns the singleton for a Go bool.
func BoolFrom(b bool) *Bool {
	if b {
		return True
	}
	return False
}

// Null represents the absence of a value.
type Null struct{}

var NullValue = &Null{}

func (v *Null) TypeID() TypeID  { return TypeNull }
func (v *Null) Truthy() bool    { return false }
func (v *Null) Inspect() string { return "null" }

// ============================================================
// Collection values
// ============================================================

// Array is a mutable, ordered sequence.
type Array struct {
	Elements []Value
	Mutable  bool // false if bound with 'let' (no mut)
}

func (v *Array) TypeID() TypeID { return TypeArray }
func (v *Array) Truthy() bool   { return len(v.Elements) > 0 }
func (v *Array) Inspect() string {
	parts := make([]string, len(v.Elements))
	for i, el := range v.Elements {
		parts[i] = el.Inspect()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// StructInstance is an instance of a user-defined struct.
type StructInstance struct {
	TypeName string
	Fields   map[string]Value
	Mutable  bool
}

func (v *StructInstance) TypeID() TypeID  { return TypeStruct }
func (v *StructInstance) Truthy() bool    { return true }
func (v *StructInstance) Inspect() string {
	parts := []string{}
	for k, val := range v.Fields {
		parts = append(parts, k+": "+val.Inspect())
	}
	return v.TypeName + " { " + strings.Join(parts, ", ") + " }"
}

// ============================================================
// Callable values
// ============================================================

// Function is a user-defined function including its closure environment.
type Function struct {
	Name    string
	Params  []string
	Body    interface{} // *ast.BlockStatement — stored as interface{} to avoid import cycle
	Closure *Environment
}

func (v *Function) TypeID() TypeID  { return TypeFunction }
func (v *Function) Truthy() bool    { return true }
func (v *Function) Inspect() string { return fmt.Sprintf("fn %s(%s) { ... }", v.Name, strings.Join(v.Params, ", ")) }

// BuiltinFn is the signature for built-in function implementations.
type BuiltinFn func(args []Value) (Value, error)

// Builtin wraps a Go function as a ranf callable.
type Builtin struct {
	Name string
	Fn   BuiltinFn
}

func (v *Builtin) TypeID() TypeID  { return TypeBuiltin }
func (v *Builtin) Truthy() bool    { return true }
func (v *Builtin) Inspect() string { return fmt.Sprintf("<builtin: %s>", v.Name) }

// ============================================================
// Option / Result types (safety wrappers)
// ============================================================

// Some wraps a value inside the Option type.
type Some struct{ Inner Value }

func (v *Some) TypeID() TypeID  { return TypeSome }
func (v *Some) Truthy() bool    { return true }
func (v *Some) Inspect() string { return "Some(" + v.Inner.Inspect() + ")" }

// None represents the absence of an optional value.
type None struct{}

var NoneValue = &None{}

func (v *None) TypeID() TypeID  { return TypeNone }
func (v *None) Truthy() bool    { return false }
func (v *None) Inspect() string { return "None" }

// Ok wraps a success value inside the Result type.
type Ok struct{ Inner Value }

func (v *Ok) TypeID() TypeID  { return TypeOk }
func (v *Ok) Truthy() bool    { return true }
func (v *Ok) Inspect() string { return "Ok(" + v.Inner.Inspect() + ")" }

// Err wraps an error value inside the Result type.
type Err struct{ Inner Value }

func (v *Err) TypeID() TypeID  { return TypeErr }
func (v *Err) Truthy() bool    { return false }
func (v *Err) Inspect() string { return "Err(" + v.Inner.Inspect() + ")" }

// ============================================================
// Control flow signals (internal use only)
// ============================================================

// ReturnSignal carries a return value up the call stack.
type ReturnSignal struct{ Value Value }

func (v *ReturnSignal) TypeID() TypeID { return TypeReturn }
func (v *ReturnSignal) Truthy() bool   { return false }
func (v *ReturnSignal) Inspect() string {
	if v.Value == nil {
		return "<return>"
	}
	return "<return " + v.Value.Inspect() + ">"
}

// BreakSignal propagates a break out of a loop.
type BreakSignal struct{}

func (v *BreakSignal) TypeID() TypeID  { return TypeBreak }
func (v *BreakSignal) Truthy() bool    { return false }
func (v *BreakSignal) Inspect() string { return "<break>" }

// ContinueSignal propagates a continue to the next loop iteration.
type ContinueSignal struct{}

func (v *ContinueSignal) TypeID() TypeID  { return TypeContinue }
func (v *ContinueSignal) Truthy() bool    { return false }
func (v *ContinueSignal) Inspect() string { return "<continue>" }

// ---- helpers ----

// IsSignal returns true for control-flow signals (return/break/continue).
func IsSignal(v Value) bool {
	if v == nil {
		return false
	}
	t := v.TypeID()
	return t == TypeReturn || t == TypeBreak || t == TypeContinue
}

// IsError returns true if v is an Err value (not a Go error).
func IsError(v Value) bool {
	if v == nil {
		return false
	}
	return v.TypeID() == TypeErr
}

// ============================================================
// Environment (variable scope / closure)
// ============================================================

// Binding stores a value together with its mutability flag.
type Binding struct {
	Value   Value
	Mutable bool
}

// Environment is a lexical scope that chains to an outer scope.
type Environment struct {
	store map[string]*Binding
	outer *Environment
}

// NewEnvironment creates a top-level (global) environment.
func NewEnvironment() *Environment {
	return &Environment{store: make(map[string]*Binding)}
}

// NewEnclosed creates a child environment that delegates to outer.
func NewEnclosed(outer *Environment) *Environment {
	return &Environment{store: make(map[string]*Binding), outer: outer}
}

// Get retrieves a binding, walking up the scope chain.
func (e *Environment) Get(name string) (*Binding, bool) {
	b, ok := e.store[name]
	if !ok && e.outer != nil {
		return e.outer.Get(name)
	}
	return b, ok
}

// Define creates a new binding in the current scope.
// Returns an error if the name already exists in this scope.
func (e *Environment) Define(name string, val Value, mutable bool) error {
	if _, exists := e.store[name]; exists {
		return fmt.Errorf("variable %q already defined in this scope", name)
	}
	e.store[name] = &Binding{Value: val, Mutable: mutable}
	return nil
}

// Set updates an existing binding. Searches the scope chain.
// Returns an error if the variable is not found or is immutable.
func (e *Environment) Set(name string, val Value) error {
	if b, ok := e.store[name]; ok {
		if !b.Mutable {
			return fmt.Errorf("cannot assign to immutable variable %q (declare with 'let mut')", name)
		}
		b.Value = val
		return nil
	}
	if e.outer != nil {
		return e.outer.Set(name, val)
	}
	return fmt.Errorf("undefined variable %q", name)
}

// ============================================================
// Struct definition registry
// ============================================================

// StructDef describes the shape of a user-defined struct.
type StructDef struct {
	Name   string
	Fields []StructFieldDef
}

// StructFieldDef is one field in a struct definition.
type StructFieldDef struct {
	Name     string
	TypeName string // "int", "float", "str", "bool", or another struct name
}

// StructRegistry holds all registered struct definitions.
type StructRegistry struct {
	defs map[string]*StructDef
}

// NewStructRegistry creates an empty registry.
func NewStructRegistry() *StructRegistry {
	return &StructRegistry{defs: make(map[string]*StructDef)}
}

// Register adds a struct definition.
func (r *StructRegistry) Register(def *StructDef) {
	r.defs[def.Name] = def
}

// Get retrieves a struct definition by name.
func (r *StructRegistry) Get(name string) (*StructDef, bool) {
	d, ok := r.defs[name]
	return d, ok
}
