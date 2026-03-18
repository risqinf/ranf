// Package vm implements the ranf tree-walking evaluator.
//
// The VM service walks the AST produced by the parser, evaluates every node,
// and returns a runtime value. It is the execution engine of ranf.
//
// Design:
//   - One eval* method per AST node type (exhaustive switch)
//   - Control flow (return/break/continue) propagates via signal values
//   - f-string parts are re-lexed and re-evaluated inline
//   - Method calls dispatch to a built-in method table per value type
//
// Usage:
//
//	svc := vm.NewService(builtinRegistry)
//	result, err := svc.Run(program, env)
package vm

import (
	"fmt"
	"math"
	"strings"

	"github.com/risqinf/ranf/internal/ast"
	"github.com/risqinf/ranf/internal/builtin"
	"github.com/risqinf/ranf/internal/lexer"
	"github.com/risqinf/ranf/internal/object"
	"github.com/risqinf/ranf/internal/parser"
	"github.com/risqinf/ranf/internal/token"
	"github.com/risqinf/ranf/pkg/errors"
)

// Service is the VM / evaluator microservice.
type Service struct {
	builtins *builtin.Registry
	structs  *object.StructRegistry
}

// NewService creates a VM service wired to the given built-in registry.
func NewService(b *builtin.Registry) *Service {
	return &Service{
		builtins: b,
		structs:  object.NewStructRegistry(),
	}
}

// Run evaluates a full program in the given environment and returns the last value.
func (s *Service) Run(prog *ast.Program, env *object.Environment) (object.Value, *errors.RanfError) {
	var last object.Value = object.NullValue
	for _, stmt := range prog.Statements {
		val := s.evalStmt(stmt, env)
		if val == nil {
			continue
		}
		if sig, ok := val.(*object.ReturnSignal); ok {
			if sig.Value != nil {
				return sig.Value, nil
			}
			return object.NullValue, nil
		}
		if isRuntimeError(val) {
			return nil, toRanfError(val)
		}
		last = val
	}
	return last, nil
}

// ============================================================
// Statement evaluation
// ============================================================

func (s *Service) evalStmt(stmt ast.Statement, env *object.Environment) object.Value {
	if stmt == nil {
		return object.NullValue
	}
	switch st := stmt.(type) {
	case *ast.LetStatement:
		return s.evalLet(st, env)
	case *ast.AssignStatement:
		return s.evalAssign(st, env)
	case *ast.ExpressionStatement:
		return s.evalExpr(st.Expr, env)
	case *ast.ReturnStatement:
		return s.evalReturn(st, env)
	case *ast.BreakStatement:
		return &object.BreakSignal{}
	case *ast.ContinueStatement:
		return &object.ContinueSignal{}
	case *ast.IfStatement:
		return s.evalIf(st, env)
	case *ast.WhileStatement:
		return s.evalWhile(st, env)
	case *ast.ForRangeStatement:
		return s.evalForRange(st, env)
	case *ast.ForEachStatement:
		return s.evalForEach(st, env)
	case *ast.LoopStatement:
		return s.evalLoop(st, env)
	case *ast.FnDeclaration:
		return s.evalFnDecl(st, env)
	case *ast.StructDeclaration:
		return s.evalStructDecl(st, env)
	case *ast.MatchStatement:
		return s.evalMatch(st, env)
	case *ast.BlockStatement:
		return s.evalBlock(st, object.NewEnclosed(env))
	case *ast.UseStatement:
		return object.NullValue // module system placeholder
	}
	return runtimeErr("unknown statement type: %T", stmt)
}

func (s *Service) evalLet(st *ast.LetStatement, env *object.Environment) object.Value {
	val := s.evalExpr(st.Value, env)
	if isSignal(val) {
		return val
	}
	if err := env.Define(st.Name, val, st.Mutable); err != nil {
		return runtimeErr("%s", err.Error())
	}
	return object.NullValue
}

func (s *Service) evalAssign(st *ast.AssignStatement, env *object.Environment) object.Value {
	rhs := s.evalExpr(st.Value, env)
	if isSignal(rhs) {
		return rhs
	}

	// Compound assignment: expand rhs
	if st.Operator != "=" {
		lhs := s.evalExpr(st.Target, env)
		if isSignal(lhs) {
			return lhs
		}
		op := strings.TrimSuffix(st.Operator, "=")
		var err error
		rhs, err = s.applyBinaryOp(op, lhs, rhs)
		if err != nil {
			return runtimeErr("%s", err)
		}
	}

	// Dispatch based on lvalue type
	switch target := st.Target.(type) {
	case *ast.Identifier:
		if err := env.Set(target.Name, rhs); err != nil {
			return runtimeErr("%s", err.Error())
		}
	case *ast.IndexExpr:
		obj := s.evalExpr(target.Object, env)
		if isSignal(obj) {
			return obj
		}
		idx := s.evalExpr(target.Index, env)
		if isSignal(idx) {
			return idx
		}
		arr, ok := obj.(*object.Array)
		if !ok {
			return runtimeErr("index assignment only supported on arrays, got %s", obj.TypeID())
		}
		if !arr.Mutable {
			return runtimeErr("cannot index-assign into immutable array")
		}
		i, ok := idx.(*object.Int)
		if !ok {
			return runtimeErr("array index must be int, got %s", idx.TypeID())
		}
		n := int64(len(arr.Elements))
		pos := i.V
		if pos < 0 {
			pos = n + pos
		}
		if pos < 0 || pos >= n {
			return runtimeErr("index %d out of bounds [0, %d)", i.V, n)
		}
		arr.Elements[pos] = rhs
	case *ast.FieldExpr:
		obj := s.evalExpr(target.Object, env)
		if isSignal(obj) {
			return obj
		}
		si, ok := obj.(*object.StructInstance)
		if !ok {
			return runtimeErr("field assignment only supported on struct instances")
		}
		if !si.Mutable {
			return runtimeErr("cannot assign to field of immutable struct")
		}
		si.Fields[target.Field] = rhs
	default:
		return runtimeErr("invalid assignment target: %T", st.Target)
	}
	return object.NullValue
}

func (s *Service) evalReturn(st *ast.ReturnStatement, env *object.Environment) object.Value {
	if st.Value == nil {
		return &object.ReturnSignal{Value: object.NullValue}
	}
	val := s.evalExpr(st.Value, env)
	if isSignal(val) {
		return val
	}
	return &object.ReturnSignal{Value: val}
}

func (s *Service) evalIf(st *ast.IfStatement, env *object.Environment) object.Value {
	cond := s.evalExpr(st.Condition, env)
	if isSignal(cond) {
		return cond
	}
	if cond.Truthy() {
		return s.evalBlock(st.Consequence, object.NewEnclosed(env))
	}
	for _, ei := range st.ElseIfs {
		c := s.evalExpr(ei.Condition, env)
		if isSignal(c) {
			return c
		}
		if c.Truthy() {
			return s.evalBlock(ei.Consequence, object.NewEnclosed(env))
		}
	}
	if st.Alternative != nil {
		return s.evalBlock(st.Alternative, object.NewEnclosed(env))
	}
	return object.NullValue
}

func (s *Service) evalWhile(st *ast.WhileStatement, env *object.Environment) object.Value {
	for {
		cond := s.evalExpr(st.Condition, env)
		if isSignal(cond) {
			return cond
		}
		if !cond.Truthy() {
			break
		}
		result := s.evalBlock(st.Body, object.NewEnclosed(env))
		if _, ok := result.(*object.BreakSignal); ok {
			break
		}
		if _, ok := result.(*object.ContinueSignal); ok {
			continue
		}
		if _, ok := result.(*object.ReturnSignal); ok {
			return result
		}
		if isRuntimeError(result) {
			return result
		}
	}
	return object.NullValue
}

func (s *Service) evalForRange(st *ast.ForRangeStatement, env *object.Environment) object.Value {
	startVal := s.evalExpr(st.Start, env)
	if isSignal(startVal) {
		return startVal
	}
	endVal := s.evalExpr(st.End, env)
	if isSignal(endVal) {
		return endVal
	}
	startInt, ok := startVal.(*object.Int)
	if !ok {
		return runtimeErr("for range start must be int, got %s", startVal.TypeID())
	}
	endInt, ok := endVal.(*object.Int)
	if !ok {
		return runtimeErr("for range end must be int, got %s", endVal.TypeID())
	}
	end := endInt.V
	if st.Inclusive {
		end++
	}
	for i := startInt.V; i < end; i++ {
		loopEnv := object.NewEnclosed(env)
		loopEnv.Define(st.VarName, &object.Int{V: i}, false)
		result := s.evalBlock(st.Body, loopEnv)
		if _, ok := result.(*object.BreakSignal); ok {
			break
		}
		if _, ok := result.(*object.ContinueSignal); ok {
			continue
		}
		if _, ok := result.(*object.ReturnSignal); ok {
			return result
		}
		if isRuntimeError(result) {
			return result
		}
	}
	return object.NullValue
}

func (s *Service) evalForEach(st *ast.ForEachStatement, env *object.Environment) object.Value {
	coll := s.evalExpr(st.Collection, env)
	if isSignal(coll) {
		return coll
	}
	var elements []object.Value
	switch c := coll.(type) {
	case *object.Array:
		elements = c.Elements
	case *object.Str:
		for _, r := range c.V {
			elements = append(elements, &object.Str{V: string(r)})
		}
	default:
		return runtimeErr("for-each requires array or str, got %s", coll.TypeID())
	}
	for _, el := range elements {
		loopEnv := object.NewEnclosed(env)
		loopEnv.Define(st.VarName, el, false)
		result := s.evalBlock(st.Body, loopEnv)
		if _, ok := result.(*object.BreakSignal); ok {
			break
		}
		if _, ok := result.(*object.ContinueSignal); ok {
			continue
		}
		if _, ok := result.(*object.ReturnSignal); ok {
			return result
		}
		if isRuntimeError(result) {
			return result
		}
	}
	return object.NullValue
}

func (s *Service) evalLoop(st *ast.LoopStatement, env *object.Environment) object.Value {
	for {
		result := s.evalBlock(st.Body, object.NewEnclosed(env))
		if _, ok := result.(*object.BreakSignal); ok {
			break
		}
		if _, ok := result.(*object.ContinueSignal); ok {
			continue
		}
		if _, ok := result.(*object.ReturnSignal); ok {
			return result
		}
		if isRuntimeError(result) {
			return result
		}
	}
	return object.NullValue
}

func (s *Service) evalFnDecl(st *ast.FnDeclaration, env *object.Environment) object.Value {
	params := make([]string, len(st.Params))
	for i, p := range st.Params {
		params[i] = p.Name
	}
	fn := &object.Function{
		Name:    st.Name,
		Params:  params,
		Body:    st.Body,
		Closure: env,
	}
	if err := env.Define(st.Name, fn, false); err != nil {
		// Allow redefinition at top level (repl convenience)
	}
	return object.NullValue
}

func (s *Service) evalStructDecl(st *ast.StructDeclaration, env *object.Environment) object.Value {
	fields := make([]object.StructFieldDef, len(st.Fields))
	for i, f := range st.Fields {
		typeName := "any"
		if f.Type != nil {
			typeName = f.Type.Name
		}
		fields[i] = object.StructFieldDef{Name: f.Name, TypeName: typeName}
	}
	def := &object.StructDef{Name: st.Name, Fields: fields}
	s.structs.Register(def)
	// Also expose the struct name as a constructor-like callable
	ctor := &object.Builtin{
		Name: st.Name,
		Fn: func(args []object.Value) (object.Value, error) {
			return nil, fmt.Errorf("use struct literal syntax: %s { field: value, ... }", st.Name)
		},
	}
	env.Define(st.Name, ctor, false)
	return object.NullValue
}

func (s *Service) evalMatch(st *ast.MatchStatement, env *object.Environment) object.Value {
	subject := s.evalExpr(st.Subject, env)
	if isSignal(subject) {
		return subject
	}
	for _, arm := range st.Arms {
		armEnv := object.NewEnclosed(env)
		if s.matchPattern(arm.Pattern, subject, armEnv) {
			result := s.evalStmt(arm.Body, armEnv)
			return result
		}
	}
	return object.NullValue
}

// matchPattern returns true if val matches pat, and populates env with bindings.
func (s *Service) matchPattern(pat ast.MatchPattern, val object.Value, env *object.Environment) bool {
	switch p := pat.(type) {
	case *ast.WildcardPattern:
		return true
	case *ast.IdentPattern:
		env.Define(p.Name, val, false)
		return true
	case *ast.NonePattern:
		_, ok := val.(*object.None)
		return ok
	case *ast.SomePattern:
		some, ok := val.(*object.Some)
		if !ok {
			return false
		}
		return s.matchPattern(p.Inner, some.Inner, env)
	case *ast.OkPattern:
		ok2, ok := val.(*object.Ok)
		if !ok {
			return false
		}
		return s.matchPattern(p.Inner, ok2.Inner, env)
	case *ast.ErrPattern:
		errVal, ok := val.(*object.Err)
		if !ok {
			return false
		}
		return s.matchPattern(p.Inner, errVal.Inner, env)
	case *ast.RangePattern:
		lo := s.evalExpr(p.Low, env)
		hi := s.evalExpr(p.High, env)
		loI, ok1 := lo.(*object.Int)
		hiI, ok2 := hi.(*object.Int)
		valI, ok3 := val.(*object.Int)
		if !ok1 || !ok2 || !ok3 {
			return false
		}
		if p.Inclusive {
			return valI.V >= loI.V && valI.V <= hiI.V
		}
		return valI.V >= loI.V && valI.V < hiI.V
	case *ast.LiteralPattern:
		patVal := s.evalExpr(p.Value, env)
		return valuesEqual(patVal, val)
	}
	return false
}

func (s *Service) evalBlock(blk *ast.BlockStatement, env *object.Environment) object.Value {
	if blk == nil {
		return object.NullValue
	}
	var last object.Value = object.NullValue
	for _, stmt := range blk.Statements {
		result := s.evalStmt(stmt, env)
		if result == nil {
			continue
		}
		if object.IsSignal(result) || isRuntimeError(result) {
			return result
		}
		last = result
	}
	return last
}

// ============================================================
// Expression evaluation
// ============================================================

func (s *Service) evalExpr(expr ast.Expression, env *object.Environment) object.Value {
	if expr == nil {
		return object.NullValue
	}
	switch e := expr.(type) {
	case *ast.IntLiteral:
		return &object.Int{V: e.Value}
	case *ast.FloatLiteral:
		return &object.Float{V: e.Value}
	case *ast.StringLiteral:
		return &object.Str{V: e.Value}
	case *ast.BoolLiteral:
		return object.BoolFrom(e.Value)
	case *ast.NullLiteral:
		return object.NullValue
	case *ast.NoneExpr:
		return object.NoneValue
	case *ast.SomeExpr:
		inner := s.evalExpr(e.Value, env)
		if isSignal(inner) {
			return inner
		}
		return &object.Some{Inner: inner}
	case *ast.OkExpr:
		inner := s.evalExpr(e.Value, env)
		if isSignal(inner) {
			return inner
		}
		return &object.Ok{Inner: inner}
	case *ast.ErrExpr:
		inner := s.evalExpr(e.Value, env)
		if isSignal(inner) {
			return inner
		}
		return &object.Err{Inner: inner}
	case *ast.Identifier:
		return s.evalIdent(e, env)
	case *ast.GroupExpr:
		return s.evalExpr(e.Inner, env)
	case *ast.UnaryExpr:
		return s.evalUnary(e, env)
	case *ast.BinaryExpr:
		return s.evalBinary(e, env)
	case *ast.ArrayLiteral:
		return s.evalArrayLiteral(e, env)
	case *ast.StructLiteral:
		return s.evalStructLiteral(e, env)
	case *ast.CallExpr:
		return s.evalCall(e, env)
	case *ast.MethodCallExpr:
		return s.evalMethodCall(e, env)
	case *ast.IndexExpr:
		return s.evalIndex(e, env)
	case *ast.FieldExpr:
		return s.evalField(e, env)
	case *ast.FStringLiteral:
		return s.evalFString(e, env)
	case *ast.RangeExpr:
		return s.evalRangeExpr(e, env)
	}
	return runtimeErr("unknown expression type: %T", expr)
}

func (s *Service) evalIdent(e *ast.Identifier, env *object.Environment) object.Value {
	// Check user environment
	if b, ok := env.Get(e.Name); ok {
		return b.Value
	}
	// Check built-ins
	if b := s.builtins.Get(e.Name); b != nil {
		return b
	}
	return runtimeErr("undefined variable %q", e.Name)
}

func (s *Service) evalUnary(e *ast.UnaryExpr, env *object.Environment) object.Value {
	right := s.evalExpr(e.Right, env)
	if isSignal(right) {
		return right
	}
	switch e.Operator {
	case "-":
		switch v := right.(type) {
		case *object.Int:
			return &object.Int{V: -v.V}
		case *object.Float:
			return &object.Float{V: -v.V}
		}
		return runtimeErr("unary '-' not supported on %s", right.TypeID())
	case "!":
		return object.BoolFrom(!right.Truthy())
	}
	return runtimeErr("unknown unary operator: %s", e.Operator)
}

func (s *Service) evalBinary(e *ast.BinaryExpr, env *object.Environment) object.Value {
	// Short-circuit logical operators
	if e.Operator == "&&" {
		left := s.evalExpr(e.Left, env)
		if isSignal(left) {
			return left
		}
		if !left.Truthy() {
			return object.False
		}
		right := s.evalExpr(e.Right, env)
		if isSignal(right) {
			return right
		}
		return object.BoolFrom(right.Truthy())
	}
	if e.Operator == "||" {
		left := s.evalExpr(e.Left, env)
		if isSignal(left) {
			return left
		}
		if left.Truthy() {
			return object.True
		}
		right := s.evalExpr(e.Right, env)
		if isSignal(right) {
			return right
		}
		return object.BoolFrom(right.Truthy())
	}

	left := s.evalExpr(e.Left, env)
	if isSignal(left) {
		return left
	}
	right := s.evalExpr(e.Right, env)
	if isSignal(right) {
		return right
	}

	result, err := s.applyBinaryOp(e.Operator, left, right)
	if err != nil {
		return runtimeErr("%s", err)
	}
	return result
}

func (s *Service) applyBinaryOp(op string, left, right object.Value) (object.Value, error) {
	// String concatenation
	if op == "+" {
		ls, lok := left.(*object.Str)
		rs, rok := right.(*object.Str)
		if lok && rok {
			return &object.Str{V: ls.V + rs.V}, nil
		}
	}

	// Numeric arithmetic
	switch lv := left.(type) {
	case *object.Int:
		switch rv := right.(type) {
		case *object.Int:
			return applyIntOp(op, lv.V, rv.V)
		case *object.Float:
			return applyFloatOp(op, float64(lv.V), rv.V)
		}
	case *object.Float:
		switch rv := right.(type) {
		case *object.Float:
			return applyFloatOp(op, lv.V, rv.V)
		case *object.Int:
			return applyFloatOp(op, lv.V, float64(rv.V))
		}
	}

	// Equality (any types)
	switch op {
	case "==":
		return object.BoolFrom(valuesEqual(left, right)), nil
	case "!=":
		return object.BoolFrom(!valuesEqual(left, right)), nil
	}

	return nil, fmt.Errorf("operator %q not supported between %s and %s", op, left.TypeID(), right.TypeID())
}

func applyIntOp(op string, a, b int64) (object.Value, error) {
	switch op {
	case "+":
		return &object.Int{V: a + b}, nil
	case "-":
		return &object.Int{V: a - b}, nil
	case "*":
		return &object.Int{V: a * b}, nil
	case "/":
		if b == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return &object.Int{V: a / b}, nil
	case "%":
		if b == 0 {
			return nil, fmt.Errorf("modulo by zero")
		}
		return &object.Int{V: a % b}, nil
	case "**":
		return &object.Int{V: int64(math.Pow(float64(a), float64(b)))}, nil
	case "<":
		return object.BoolFrom(a < b), nil
	case ">":
		return object.BoolFrom(a > b), nil
	case "<=":
		return object.BoolFrom(a <= b), nil
	case ">=":
		return object.BoolFrom(a >= b), nil
	case "==":
		return object.BoolFrom(a == b), nil
	case "!=":
		return object.BoolFrom(a != b), nil
	case "&":
		return &object.Int{V: a & b}, nil
	case "|":
		return &object.Int{V: a | b}, nil
	case "^":
		return &object.Int{V: a ^ b}, nil
	case "<<":
		return &object.Int{V: a << uint(b)}, nil
	case ">>":
		return &object.Int{V: a >> uint(b)}, nil
	}
	return nil, fmt.Errorf("unsupported int operator: %s", op)
}

func applyFloatOp(op string, a, b float64) (object.Value, error) {
	switch op {
	case "+":
		return &object.Float{V: a + b}, nil
	case "-":
		return &object.Float{V: a - b}, nil
	case "*":
		return &object.Float{V: a * b}, nil
	case "/":
		if b == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		return &object.Float{V: a / b}, nil
	case "%":
		return &object.Float{V: math.Mod(a, b)}, nil
	case "**":
		return &object.Float{V: math.Pow(a, b)}, nil
	case "<":
		return object.BoolFrom(a < b), nil
	case ">":
		return object.BoolFrom(a > b), nil
	case "<=":
		return object.BoolFrom(a <= b), nil
	case ">=":
		return object.BoolFrom(a >= b), nil
	case "==":
		return object.BoolFrom(a == b), nil
	case "!=":
		return object.BoolFrom(a != b), nil
	}
	return nil, fmt.Errorf("unsupported float operator: %s", op)
}

func (s *Service) evalArrayLiteral(e *ast.ArrayLiteral, env *object.Environment) object.Value {
	elems := make([]object.Value, len(e.Elements))
	for i, el := range e.Elements {
		v := s.evalExpr(el, env)
		if isSignal(v) {
			return v
		}
		elems[i] = v
	}
	return &object.Array{Elements: elems}
}

func (s *Service) evalStructLiteral(e *ast.StructLiteral, env *object.Environment) object.Value {
	fields := make(map[string]object.Value, len(e.Fields))
	for _, f := range e.Fields {
		v := s.evalExpr(f.Value, env)
		if isSignal(v) {
			return v
		}
		fields[f.Name] = v
	}
	return &object.StructInstance{TypeName: e.TypeName, Fields: fields}
}

func (s *Service) evalCall(e *ast.CallExpr, env *object.Environment) object.Value {
	callee := s.evalExpr(e.Callee, env)
	if isSignal(callee) {
		return callee
	}
	args := s.evalArgs(e.Args, env)
	if len(args) == 1 {
		if isSignal(args[0]) {
			return args[0]
		}
	}
	return s.applyFunction(callee, args)
}

func (s *Service) evalMethodCall(e *ast.MethodCallExpr, env *object.Environment) object.Value {
	obj := s.evalExpr(e.Object, env)
	if isSignal(obj) {
		return obj
	}
	args := s.evalArgs(e.Args, env)
	return s.applyMethod(obj, e.Method, args)
}

func (s *Service) evalArgs(exprs []ast.Expression, env *object.Environment) []object.Value {
	vals := make([]object.Value, len(exprs))
	for i, ex := range exprs {
		v := s.evalExpr(ex, env)
		if isSignal(v) {
			return []object.Value{v}
		}
		vals[i] = v
	}
	return vals
}

func (s *Service) applyFunction(fn object.Value, args []object.Value) object.Value {
	switch f := fn.(type) {
	case *object.Builtin:
		result, err := f.Fn(args)
		if err != nil {
			return runtimeErr("%s", err)
		}
		if result == nil {
			return object.NullValue
		}
		return result
	case *object.Function:
		fnEnv := object.NewEnclosed(f.Closure)
		for i, param := range f.Params {
			if i >= len(args) {
				fnEnv.Define(param, object.NullValue, false)
			} else {
				fnEnv.Define(param, args[i], false)
			}
		}
		body, ok := f.Body.(*ast.BlockStatement)
		if !ok {
			return runtimeErr("function body is not a block")
		}
		result := s.evalBlock(body, fnEnv)
		if sig, ok := result.(*object.ReturnSignal); ok {
			return sig.Value
		}
		return result
	}
	return runtimeErr("not a callable: %s", fn.TypeID())
}

func (s *Service) evalIndex(e *ast.IndexExpr, env *object.Environment) object.Value {
	obj := s.evalExpr(e.Object, env)
	if isSignal(obj) {
		return obj
	}
	idx := s.evalExpr(e.Index, env)
	if isSignal(idx) {
		return idx
	}
	switch o := obj.(type) {
	case *object.Array:
		i, ok := idx.(*object.Int)
		if !ok {
			return runtimeErr("array index must be int, got %s", idx.TypeID())
		}
		pos := i.V
		n := int64(len(o.Elements))
		if pos < 0 {
			pos = n + pos
		}
		if pos < 0 || pos >= n {
			return runtimeErr("index %d out of bounds [0, %d)", i.V, n)
		}
		return o.Elements[pos]
	case *object.Str:
		i, ok := idx.(*object.Int)
		if !ok {
			return runtimeErr("string index must be int, got %s", idx.TypeID())
		}
		runes := []rune(o.V)
		pos := i.V
		n := int64(len(runes))
		if pos < 0 {
			pos = n + pos
		}
		if pos < 0 || pos >= n {
			return runtimeErr("index %d out of bounds [0, %d)", i.V, n)
		}
		return &object.Str{V: string(runes[pos])}
	}
	return runtimeErr("index operator not supported on %s", obj.TypeID())
}

func (s *Service) evalField(e *ast.FieldExpr, env *object.Environment) object.Value {
	obj := s.evalExpr(e.Object, env)
	if isSignal(obj) {
		return obj
	}
	si, ok := obj.(*object.StructInstance)
	if !ok {
		return runtimeErr("field access not supported on %s", obj.TypeID())
	}
	val, exists := si.Fields[e.Field]
	if !exists {
		return runtimeErr("struct %s has no field %q", si.TypeName, e.Field)
	}
	return val
}

func (s *Service) evalFString(e *ast.FStringLiteral, env *object.Environment) object.Value {
	var sb strings.Builder
	for _, part := range e.Parts {
		if !part.IsExpr {
			sb.WriteString(part.Text)
			continue
		}
		// Re-lex and re-parse the embedded expression
		lexSvc := lexer.NewService()
		toks, lexErrs := lexSvc.Tokenize("<fstring>", part.Text)
		if lexErrs.HasErrors() {
			return runtimeErr("f-string interpolation error: %s", lexErrs.Error())
		}
		parseSvc := parser.NewService()
		prog, parseErrs := parseSvc.Parse(toks)
		if parseErrs.HasErrors() {
			return runtimeErr("f-string interpolation error: %s", parseErrs.Error())
		}
		if len(prog.Statements) == 0 {
			continue
		}
		exprStmt, ok := prog.Statements[0].(*ast.ExpressionStatement)
		if !ok {
			return runtimeErr("f-string interpolation must be an expression")
		}
		val := s.evalExpr(exprStmt.Expr, env)
		if isSignal(val) {
			return val
		}
		sb.WriteString(val.Inspect())
	}
	return &object.Str{V: sb.String()}
}

func (s *Service) evalRangeExpr(e *ast.RangeExpr, env *object.Environment) object.Value {
	start := s.evalExpr(e.Start, env)
	if isSignal(start) {
		return start
	}
	end := s.evalExpr(e.End, env)
	if isSignal(end) {
		return end
	}
	startI, ok := start.(*object.Int)
	if !ok {
		return runtimeErr("range start must be int")
	}
	endI, ok := end.(*object.Int)
	if !ok {
		return runtimeErr("range end must be int")
	}
	hi := endI.V
	if e.Inclusive {
		hi++
	}
	elems := make([]object.Value, 0, hi-startI.V)
	for i := startI.V; i < hi; i++ {
		elems = append(elems, &object.Int{V: i})
	}
	return &object.Array{Elements: elems}
}

// ============================================================
// Method dispatch
// ============================================================

func (s *Service) applyMethod(obj object.Value, method string, args []object.Value) object.Value {
	switch v := obj.(type) {
	case *object.Str:
		return s.stringMethod(v, method, args)
	case *object.Array:
		return s.arrayMethod(v, method, args)
	case *object.Some:
		return s.optionMethod(v, method, args)
	case *object.None:
		return s.noneMethod(method, args)
	case *object.Ok:
		return s.okMethod(v, method, args)
	case *object.Err:
		return s.errMethod(v, method, args)
	case *object.Int:
		return s.intMethod(v, method, args)
	case *object.Float:
		return s.floatMethod(v, method, args)
	}
	return runtimeErr("type %s has no method %q", obj.TypeID(), method)
}

func (s *Service) stringMethod(v *object.Str, method string, args []object.Value) object.Value {
	switch method {
	case "len":
		return &object.Int{V: int64(len([]rune(v.V)))}
	case "upper":
		return &object.Str{V: strings.ToUpper(v.V)}
	case "lower":
		return &object.Str{V: strings.ToLower(v.V)}
	case "trim":
		return &object.Str{V: strings.TrimSpace(v.V)}
	case "contains":
		if len(args) < 1 {
			return runtimeErr("str.contains() requires 1 argument")
		}
		sub, ok := args[0].(*object.Str)
		if !ok {
			return runtimeErr("str.contains() argument must be str")
		}
		return object.BoolFrom(strings.Contains(v.V, sub.V))
	case "starts_with":
		if len(args) < 1 {
			return runtimeErr("str.starts_with() requires 1 argument")
		}
		prefix, ok := args[0].(*object.Str)
		if !ok {
			return runtimeErr("str.starts_with() argument must be str")
		}
		return object.BoolFrom(strings.HasPrefix(v.V, prefix.V))
	case "ends_with":
		if len(args) < 1 {
			return runtimeErr("str.ends_with() requires 1 argument")
		}
		suffix, ok := args[0].(*object.Str)
		if !ok {
			return runtimeErr("str.ends_with() argument must be str")
		}
		return object.BoolFrom(strings.HasSuffix(v.V, suffix.V))
	case "split":
		if len(args) < 1 {
			return runtimeErr("str.split() requires 1 argument")
		}
		sep, ok := args[0].(*object.Str)
		if !ok {
			return runtimeErr("str.split() argument must be str")
		}
		parts := strings.Split(v.V, sep.V)
		elems := make([]object.Value, len(parts))
		for i, p := range parts {
			elems[i] = &object.Str{V: p}
		}
		return &object.Array{Elements: elems}
	case "replace":
		if len(args) < 2 {
			return runtimeErr("str.replace() requires 2 arguments")
		}
		old, ok1 := args[0].(*object.Str)
		new_, ok2 := args[1].(*object.Str)
		if !ok1 || !ok2 {
			return runtimeErr("str.replace() arguments must be str")
		}
		return &object.Str{V: strings.ReplaceAll(v.V, old.V, new_.V)}
	case "chars":
		runes := []rune(v.V)
		elems := make([]object.Value, len(runes))
		for i, r := range runes {
			elems[i] = &object.Str{V: string(r)}
		}
		return &object.Array{Elements: elems}
	case "repeat":
		if len(args) < 1 {
			return runtimeErr("str.repeat() requires 1 argument")
		}
		n, ok := args[0].(*object.Int)
		if !ok {
			return runtimeErr("str.repeat() argument must be int")
		}
		return &object.Str{V: strings.Repeat(v.V, int(n.V))}
	case "parse_int":
		var n int64
		_, err := fmt.Sscanf(v.V, "%d", &n)
		if err != nil {
			return &object.Err{Inner: &object.Str{V: "parse_int failed: " + err.Error()}}
		}
		return &object.Ok{Inner: &object.Int{V: n}}
	case "parse_float":
		var f float64
		_, err := fmt.Sscanf(v.V, "%f", &f)
		if err != nil {
			return &object.Err{Inner: &object.Str{V: "parse_float failed: " + err.Error()}}
		}
		return &object.Ok{Inner: &object.Float{V: f}}
	}
	return runtimeErr("str has no method %q", method)
}

func (s *Service) arrayMethod(v *object.Array, method string, args []object.Value) object.Value {
	switch method {
	case "len":
		return &object.Int{V: int64(len(v.Elements))}
	case "push":
		if len(args) < 1 {
			return runtimeErr("array.push() requires 1 argument")
		}
		newElems := make([]object.Value, len(v.Elements)+1)
		copy(newElems, v.Elements)
		newElems[len(v.Elements)] = args[0]
		return &object.Array{Elements: newElems, Mutable: v.Mutable}
	case "pop":
		if len(v.Elements) == 0 {
			return object.NoneValue
		}
		return &object.Some{Inner: v.Elements[len(v.Elements)-1]}
	case "contains":
		if len(args) < 1 {
			return runtimeErr("array.contains() requires 1 argument")
		}
		for _, el := range v.Elements {
			if valuesEqual(el, args[0]) {
				return object.True
			}
		}
		return object.False
	case "first":
		if len(v.Elements) == 0 {
			return object.NoneValue
		}
		return &object.Some{Inner: v.Elements[0]}
	case "last":
		if len(v.Elements) == 0 {
			return object.NoneValue
		}
		return &object.Some{Inner: v.Elements[len(v.Elements)-1]}
	case "reverse":
		n := len(v.Elements)
		newElems := make([]object.Value, n)
		for i, el := range v.Elements {
			newElems[n-1-i] = el
		}
		return &object.Array{Elements: newElems, Mutable: v.Mutable}
	case "join":
		sep := ""
		if len(args) > 0 {
			sv, ok := args[0].(*object.Str)
			if ok {
				sep = sv.V
			}
		}
		parts := make([]string, len(v.Elements))
		for i, el := range v.Elements {
			parts[i] = el.Inspect()
		}
		return &object.Str{V: strings.Join(parts, sep)}
	}
	return runtimeErr("array has no method %q", method)
}

func (s *Service) optionMethod(v *object.Some, method string, args []object.Value) object.Value {
	switch method {
	case "unwrap":
		return v.Inner
	case "unwrap_or":
		return v.Inner
	case "is_some":
		return object.True
	case "is_none":
		return object.False
	}
	return runtimeErr("Some has no method %q", method)
}

func (s *Service) noneMethod(method string, args []object.Value) object.Value {
	switch method {
	case "unwrap":
		return runtimeErr("called unwrap() on None")
	case "unwrap_or":
		if len(args) < 1 {
			return runtimeErr("None.unwrap_or() requires 1 argument")
		}
		return args[0]
	case "is_some":
		return object.False
	case "is_none":
		return object.True
	}
	return runtimeErr("None has no method %q", method)
}

func (s *Service) okMethod(v *object.Ok, method string, args []object.Value) object.Value {
	switch method {
	case "unwrap":
		return v.Inner
	case "unwrap_or":
		return v.Inner
	case "is_ok":
		return object.True
	case "is_err":
		return object.False
	}
	return runtimeErr("Ok has no method %q", method)
}

func (s *Service) errMethod(v *object.Err, method string, args []object.Value) object.Value {
	switch method {
	case "unwrap":
		return runtimeErr("called unwrap() on Err(%s)", v.Inner.Inspect())
	case "unwrap_or":
		if len(args) < 1 {
			return runtimeErr("Err.unwrap_or() requires 1 argument")
		}
		return args[0]
	case "unwrap_err":
		return v.Inner
	case "is_ok":
		return object.False
	case "is_err":
		return object.True
	}
	return runtimeErr("Err has no method %q", method)
}

func (s *Service) intMethod(v *object.Int, method string, _ []object.Value) object.Value {
	switch method {
	case "abs":
		if v.V < 0 {
			return &object.Int{V: -v.V}
		}
		return v
	case "to_float":
		return &object.Float{V: float64(v.V)}
	case "to_str":
		return &object.Str{V: fmt.Sprintf("%d", v.V)}
	}
	return runtimeErr("int has no method %q", method)
}

func (s *Service) floatMethod(v *object.Float, method string, _ []object.Value) object.Value {
	switch method {
	case "floor":
		return &object.Int{V: int64(math.Floor(v.V))}
	case "ceil":
		return &object.Int{V: int64(math.Ceil(v.V))}
	case "round":
		return &object.Int{V: int64(math.Round(v.V))}
	case "abs":
		return &object.Float{V: math.Abs(v.V)}
	case "sqrt":
		return &object.Float{V: math.Sqrt(v.V)}
	case "to_int":
		return &object.Int{V: int64(v.V)}
	case "to_str":
		return &object.Str{V: v.Inspect()}
	}
	return runtimeErr("float has no method %q", method)
}

// ============================================================
// Helpers
// ============================================================

// runtimeErrorTag is a sentinel Str value used as an internal error carrier.
// Using a dedicated type avoids needing a separate interface.
type runtimeErrorValue struct{ msg string }

func (v *runtimeErrorValue) TypeID() object.TypeID  { return "<runtime-error>" }
func (v *runtimeErrorValue) Truthy() bool             { return false }
func (v *runtimeErrorValue) Inspect() string          { return "RuntimeError: " + v.msg }

func runtimeErr(format string, args ...interface{}) object.Value {
	return &runtimeErrorValue{msg: fmt.Sprintf(format, args...)}
}

func isRuntimeError(v object.Value) bool {
	if v == nil {
		return false
	}
	_, ok := v.(*runtimeErrorValue)
	return ok
}

func toRanfError(v object.Value) *errors.RanfError {
	if re, ok := v.(*runtimeErrorValue); ok {
		return errors.NewRuntime("%s", re.msg)
	}
	return errors.NewRuntime("unknown runtime error")
}

func isSignal(v object.Value) bool {
	if v == nil {
		return false
	}
	return object.IsSignal(v) || isRuntimeError(v)
}

func valuesEqual(a, b object.Value) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.TypeID() != b.TypeID() {
		// Int vs Float equality
		ai, aiOk := a.(*object.Int)
		af, afOk := a.(*object.Float)
		bi, biOk := b.(*object.Int)
		bf, bfOk := b.(*object.Float)
		if aiOk && bfOk {
			return float64(ai.V) == bf.V
		}
		if afOk && biOk {
			return af.V == float64(bi.V)
		}
		return false
	}
	return a.Inspect() == b.Inspect()
}

// Ensure token package is referenced (needed for type token.Precedence usage via parser).
var _ = token.PREC_LOWEST
