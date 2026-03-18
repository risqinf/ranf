// Package analyzer implements the semantic analysis pass for ranf.
//
// The analyzer runs after parsing and before evaluation.
// Safety checks performed:
//
//  1. Immutability — detect assignment to 'let' (non-mut) bindings
//  2. Undefined variables — catch use-before-define
//  3. Use-before-init — catch let x = x + 1 style bugs
//  4. Unreachable code — code after return/break/continue
//  5. Return outside function
//  6. Break/continue outside loop
//
// All built-in functions are pre-declared so they never appear as undefined.
//
// Usage:
//
//	svc := analyzer.NewService()
//	errs := svc.Analyze(program)
package analyzer

import (
	"github.com/risqinf/ranf/internal/ast"
	"github.com/risqinf/ranf/pkg/errors"
)

// builtinNames lists every name pre-declared in the global scope.
// These must stay in sync with internal/builtin/builtin.go.
var builtinNames = []string{
	// I/O
	"print", "println", "eprintln", "input",
	// Conversion
	"int", "float", "str", "bool",
	// Math
	"abs", "sqrt", "pow", "floor", "ceil", "round", "min", "max", "clamp",
	// String
	"len", "chars", "trim", "split", "join",
	"contains", "starts_with", "ends_with",
	"upper", "lower", "replace", "repeat",
	// Array
	"push", "pop", "insert", "remove",
	"first", "last", "rest", "reverse", "range",
	// Type checks
	"type_of", "is_some", "is_none", "is_ok", "is_err",
	// Unwrap
	"unwrap", "unwrap_or", "unwrap_err",
	// Assertion / panic
	"assert", "assert_eq", "panic", "exit",
}

// Service is the semantic analysis microservice.
type Service struct{}

// NewService returns a new Analyzer service.
func NewService() *Service { return &Service{} }

// Analyze runs all semantic checks and returns any errors found.
func (s *Service) Analyze(program *ast.Program) errors.ErrorList {
	a := &analyzer{
		scopes:    []scope{newScope()},
		fnDepth:   0,
		loopDepth: 0,
	}
	// Pre-populate the global scope with all built-ins
	for _, name := range builtinNames {
		a.declareGlobal(name)
	}
	a.analyzeProgram(program)
	return a.errs
}

// ============================================================
// Internal state
// ============================================================

type scope map[string]bindingInfo

type bindingInfo struct {
	mutable bool
	defined bool
	isFunc  bool
	arity   int // -1 = variadic/unknown
}

func newScope() scope { return make(scope) }

type analyzer struct {
	scopes    []scope
	fnDepth   int
	loopDepth int
	errs      errors.ErrorList
}

func (a *analyzer) addError(line, col int, msg string) {
	a.errs.Add(errors.New(errors.PhaseAnalysis, line, col, "%s", msg))
}

func (a *analyzer) pushScope() { a.scopes = append(a.scopes, newScope()) }
func (a *analyzer) popScope()  { a.scopes = a.scopes[:len(a.scopes)-1] }

// declareGlobal pre-declares a name in the bottom (global) scope as defined.
func (a *analyzer) declareGlobal(name string) {
	a.scopes[0][name] = bindingInfo{mutable: false, defined: true, isFunc: true, arity: -1}
}

func (a *analyzer) declare(name string, mutable, isFunc bool, arity int, line, col int) {
	top := a.scopes[len(a.scopes)-1]
	if _, exists := top[name]; exists {
		a.addError(line, col, "variable '"+name+"' already declared in this scope")
		return
	}
	top[name] = bindingInfo{mutable: mutable, defined: false, isFunc: isFunc, arity: arity}
}

func (a *analyzer) define(name string) {
	for i := len(a.scopes) - 1; i >= 0; i-- {
		if b, ok := a.scopes[i][name]; ok {
			b.defined = true
			a.scopes[i][name] = b
			return
		}
	}
}

func (a *analyzer) resolve(name string, line, col int) (bindingInfo, bool) {
	for i := len(a.scopes) - 1; i >= 0; i-- {
		if b, ok := a.scopes[i][name]; ok {
			if !b.defined {
				a.addError(line, col, "variable '"+name+"' used before it was fully initialized")
			}
			return b, true
		}
	}
	a.addError(line, col, "undefined variable '"+name+"'")
	return bindingInfo{}, false
}

func (a *analyzer) checkMutable(name string, line, col int) {
	for i := len(a.scopes) - 1; i >= 0; i-- {
		if b, ok := a.scopes[i][name]; ok {
			if !b.mutable {
				a.addError(line, col, "cannot assign to immutable variable '"+name+"' (declare with 'let mut')")
			}
			return
		}
	}
}

// ============================================================
// Analysis
// ============================================================

func (a *analyzer) analyzeProgram(prog *ast.Program) {
	// First pass: collect all top-level fn/struct names
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *ast.FnDeclaration:
			a.declare(s.Name, false, true, len(s.Params), s.Tok.Line, s.Tok.Col)
			a.define(s.Name)
		case *ast.StructDeclaration:
			a.declare(s.Name, false, false, 0, s.Tok.Line, s.Tok.Col)
			a.define(s.Name)
		}
	}
	// Second pass: full analysis
	for _, stmt := range prog.Statements {
		a.analyzeStmt(stmt)
	}
}

func (a *analyzer) analyzeStmt(stmt ast.Statement) (terminated bool) {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case *ast.LetStatement:
		a.declare(s.Name, s.Mutable, false, -1, s.Tok.Line, s.Tok.Col)
		if s.Value != nil {
			a.analyzeExpr(s.Value)
		}
		a.define(s.Name)

	case *ast.AssignStatement:
		a.analyzeExpr(s.Value)
		if ident, ok := s.Target.(*ast.Identifier); ok {
			a.checkMutable(ident.Name, s.Tok.Line, s.Tok.Col)
		}
		a.analyzeExpr(s.Target)

	case *ast.ExpressionStatement:
		a.analyzeExpr(s.Expr)

	case *ast.ReturnStatement:
		if a.fnDepth == 0 {
			a.addError(s.Tok.Line, s.Tok.Col, "'return' used outside of a function")
		}
		if s.Value != nil {
			a.analyzeExpr(s.Value)
		}
		return true

	case *ast.BreakStatement:
		if a.loopDepth == 0 {
			a.addError(s.Tok.Line, s.Tok.Col, "'break' used outside of a loop")
		}
		return true

	case *ast.ContinueStatement:
		if a.loopDepth == 0 {
			a.addError(s.Tok.Line, s.Tok.Col, "'continue' used outside of a loop")
		}
		return true

	case *ast.IfStatement:
		a.analyzeExpr(s.Condition)
		a.analyzeBlock(s.Consequence)
		for _, ei := range s.ElseIfs {
			a.analyzeExpr(ei.Condition)
			a.analyzeBlock(ei.Consequence)
		}
		if s.Alternative != nil {
			a.analyzeBlock(s.Alternative)
		}

	case *ast.WhileStatement:
		a.analyzeExpr(s.Condition)
		a.loopDepth++
		a.analyzeBlock(s.Body)
		a.loopDepth--

	case *ast.ForRangeStatement:
		a.analyzeExpr(s.Start)
		a.analyzeExpr(s.End)
		a.pushScope()
		a.declare(s.VarName, false, false, -1, s.Tok.Line, s.Tok.Col)
		a.define(s.VarName)
		a.loopDepth++
		a.analyzeBlockNoScope(s.Body)
		a.loopDepth--
		a.popScope()

	case *ast.ForEachStatement:
		a.analyzeExpr(s.Collection)
		a.pushScope()
		a.declare(s.VarName, false, false, -1, s.Tok.Line, s.Tok.Col)
		a.define(s.VarName)
		a.loopDepth++
		a.analyzeBlockNoScope(s.Body)
		a.loopDepth--
		a.popScope()

	case *ast.LoopStatement:
		a.loopDepth++
		a.analyzeBlock(s.Body)
		a.loopDepth--

	case *ast.FnDeclaration:
		a.analyzeFnDecl(s)

	case *ast.MatchStatement:
		a.analyzeExpr(s.Subject)
		for _, arm := range s.Arms {
			a.pushScope()
			a.analyzeMatchPattern(arm.Pattern, s.Tok.Line, s.Tok.Col)
			a.analyzeStmt(arm.Body)
			a.popScope()
		}

	case *ast.BlockStatement:
		a.analyzeBlock(s)

	case *ast.StructDeclaration, *ast.UseStatement:
		// no-op: handled in first pass or future module system
	}
	return false
}

func (a *analyzer) analyzeFnDecl(fn *ast.FnDeclaration) {
	a.pushScope()
	for _, p := range fn.Params {
		a.declare(p.Name, true, false, -1, fn.Tok.Line, fn.Tok.Col)
		a.define(p.Name)
	}
	a.fnDepth++
	a.analyzeBlockNoScope(fn.Body)
	a.fnDepth--
	a.popScope()
}

func (a *analyzer) analyzeBlock(blk *ast.BlockStatement) {
	a.pushScope()
	a.analyzeBlockNoScope(blk)
	a.popScope()
}

func (a *analyzer) analyzeBlockNoScope(blk *ast.BlockStatement) {
	if blk == nil {
		return
	}
	terminated := false
	for _, stmt := range blk.Statements {
		if terminated {
			line, col := stmt.Pos()
			a.addError(line, col, "unreachable code after return/break/continue")
			break
		}
		terminated = a.analyzeStmt(stmt)
	}
}

func (a *analyzer) analyzeMatchPattern(pat ast.MatchPattern, line, col int) {
	switch p := pat.(type) {
	case *ast.IdentPattern:
		a.declare(p.Name, false, false, -1, line, col)
		a.define(p.Name)
	case *ast.SomePattern:
		a.analyzeMatchPattern(p.Inner, line, col)
	case *ast.OkPattern:
		a.analyzeMatchPattern(p.Inner, line, col)
	case *ast.ErrPattern:
		a.analyzeMatchPattern(p.Inner, line, col)
	case *ast.LiteralPattern:
		a.analyzeExpr(p.Value)
	case *ast.RangePattern:
		a.analyzeExpr(p.Low)
		a.analyzeExpr(p.High)
	}
}

func (a *analyzer) analyzeExpr(expr ast.Expression) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		a.resolve(e.Name, e.Tok.Line, e.Tok.Col)
	case *ast.BinaryExpr:
		a.analyzeExpr(e.Left)
		a.analyzeExpr(e.Right)
	case *ast.UnaryExpr:
		a.analyzeExpr(e.Right)
	case *ast.CallExpr:
		a.analyzeExpr(e.Callee)
		for _, arg := range e.Args {
			a.analyzeExpr(arg)
		}
	case *ast.MethodCallExpr:
		a.analyzeExpr(e.Object)
		for _, arg := range e.Args {
			a.analyzeExpr(arg)
		}
	case *ast.IndexExpr:
		a.analyzeExpr(e.Object)
		a.analyzeExpr(e.Index)
	case *ast.FieldExpr:
		a.analyzeExpr(e.Object)
	case *ast.ArrayLiteral:
		for _, el := range e.Elements {
			a.analyzeExpr(el)
		}
	case *ast.StructLiteral:
		for _, f := range e.Fields {
			a.analyzeExpr(f.Value)
		}
	case *ast.SomeExpr:
		a.analyzeExpr(e.Value)
	case *ast.OkExpr:
		a.analyzeExpr(e.Value)
	case *ast.ErrExpr:
		a.analyzeExpr(e.Value)
	case *ast.GroupExpr:
		a.analyzeExpr(e.Inner)
	// Literals need no analysis
	case *ast.IntLiteral, *ast.FloatLiteral, *ast.StringLiteral,
		*ast.BoolLiteral, *ast.NullLiteral, *ast.NoneExpr,
		*ast.FStringLiteral, *ast.RangeExpr:
	}
}
