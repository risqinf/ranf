// Package ast defines the Abstract Syntax Tree nodes for ranf.
//
// Every language construct maps to a Node. Nodes are divided into:
//   - Statement  (produces no value; controls program flow)
//   - Expression (produces a value)
//
// All node types implement the Node interface.
// The Visitor pattern is supported via the Accept method for tree traversal
// (used by the analyzer, compiler, and pretty-printer).
package ast

import (
	"fmt"
	"strings"

	"github.com/risqinf/ranf/internal/token"
)

// ============================================================
// Core interfaces
// ============================================================

// Node is the base interface for all AST nodes.
type Node interface {
	TokenLiteral() string // raw source text of the leading token
	String() string       // pretty-printed representation
	Pos() (line, col int) // source position of the leading token
}

// Statement is a Node that does not directly produce a value.
type Statement interface {
	Node
	stmtNode()
}

// Expression is a Node that produces a value.
type Expression interface {
	Node
	exprNode()
}

// ============================================================
// Top-level
// ============================================================

// Program is the root of the AST.
type Program struct {
	Statements []Statement
}

func (p *Program) TokenLiteral() string {
	if len(p.Statements) > 0 {
		return p.Statements[0].TokenLiteral()
	}
	return ""
}
func (p *Program) String() string {
	var sb strings.Builder
	for _, s := range p.Statements {
		sb.WriteString(s.String())
		sb.WriteString("\n")
	}
	return sb.String()
}
func (p *Program) Pos() (int, int) { return 1, 1 }

// ============================================================
// Type annotations
// ============================================================

// TypeAnnotation represents an optional explicit type (e.g., `: int`, `: Option<str>`).
type TypeAnnotation struct {
	Name   string          // "int", "float", "str", "bool", "Option", "Result", or user struct name
	Params []*TypeAnnotation // generic parameters, e.g. <int> in Option<int>
}

func (ta *TypeAnnotation) String() string {
	if len(ta.Params) == 0 {
		return ta.Name
	}
	params := make([]string, len(ta.Params))
	for i, p := range ta.Params {
		params[i] = p.String()
	}
	return ta.Name + "<" + strings.Join(params, ", ") + ">"
}

// ============================================================
// Statements
// ============================================================

// LetStatement: let [mut] name [: Type] = expr
type LetStatement struct {
	Tok     token.Token     // 'let'
	Mutable bool            // true if 'mut' was present
	Name    string          // binding name
	Type    *TypeAnnotation // optional type annotation
	Value   Expression      // initializer expression
}

func (s *LetStatement) stmtNode()          {}
func (s *LetStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *LetStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *LetStatement) String() string {
	mut := ""
	if s.Mutable {
		mut = "mut "
	}
	ann := ""
	if s.Type != nil {
		ann = ": " + s.Type.String()
	}
	return fmt.Sprintf("let %s%s%s = %s", mut, s.Name, ann, s.Value.String())
}

// AssignStatement: name = expr  or  name += expr  etc.
type AssignStatement struct {
	Tok      token.Token // identifier token
	Target   Expression  // lvalue (ident, index expr, or field expr)
	Operator string      // "=", "+=", "-=", etc.
	Value    Expression
}

func (s *AssignStatement) stmtNode()          {}
func (s *AssignStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *AssignStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *AssignStatement) String() string {
	return fmt.Sprintf("%s %s %s", s.Target.String(), s.Operator, s.Value.String())
}

// ReturnStatement: return [expr]
type ReturnStatement struct {
	Tok   token.Token
	Value Expression // nil for bare return
}

func (s *ReturnStatement) stmtNode()          {}
func (s *ReturnStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *ReturnStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *ReturnStatement) String() string {
	if s.Value != nil {
		return "return " + s.Value.String()
	}
	return "return"
}

// BreakStatement: break
type BreakStatement struct{ Tok token.Token }

func (s *BreakStatement) stmtNode()          {}
func (s *BreakStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *BreakStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *BreakStatement) String() string     { return "break" }

// ContinueStatement: continue
type ContinueStatement struct{ Tok token.Token }

func (s *ContinueStatement) stmtNode()          {}
func (s *ContinueStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *ContinueStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *ContinueStatement) String() string     { return "continue" }

// ExpressionStatement wraps a standalone expression (e.g., a function call).
type ExpressionStatement struct {
	Tok  token.Token
	Expr Expression
}

func (s *ExpressionStatement) stmtNode()          {}
func (s *ExpressionStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *ExpressionStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *ExpressionStatement) String() string     { return s.Expr.String() }

// BlockStatement is a { ... } sequence of statements.
type BlockStatement struct {
	Tok        token.Token // '{'
	Statements []Statement
}

func (s *BlockStatement) stmtNode()          {}
func (s *BlockStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *BlockStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *BlockStatement) String() string {
	var sb strings.Builder
	sb.WriteString("{\n")
	for _, st := range s.Statements {
		sb.WriteString("  ")
		sb.WriteString(st.String())
		sb.WriteString("\n")
	}
	sb.WriteString("}")
	return sb.String()
}

// IfStatement: if expr { ... } [else if expr { ... }]* [else { ... }]
type IfStatement struct {
	Tok         token.Token
	Condition   Expression
	Consequence *BlockStatement
	ElseIfs     []ElseIfClause
	Alternative *BlockStatement // nil if no else
}

// ElseIfClause represents a single else if branch.
type ElseIfClause struct {
	Condition   Expression
	Consequence *BlockStatement
}

func (s *IfStatement) stmtNode()          {}
func (s *IfStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *IfStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *IfStatement) String() string {
	str := "if " + s.Condition.String() + " " + s.Consequence.String()
	for _, ei := range s.ElseIfs {
		str += " else if " + ei.Condition.String() + " " + ei.Consequence.String()
	}
	if s.Alternative != nil {
		str += " else " + s.Alternative.String()
	}
	return str
}

// WhileStatement: while expr { ... }
type WhileStatement struct {
	Tok       token.Token
	Condition Expression
	Body      *BlockStatement
}

func (s *WhileStatement) stmtNode()          {}
func (s *WhileStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *WhileStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *WhileStatement) String() string {
	return "while " + s.Condition.String() + " " + s.Body.String()
}

// ForRangeStatement: for i in start..end { ... }
type ForRangeStatement struct {
	Tok       token.Token
	VarName   string
	Start     Expression
	End       Expression
	Inclusive bool // true for ..=
	Body      *BlockStatement
}

func (s *ForRangeStatement) stmtNode()          {}
func (s *ForRangeStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *ForRangeStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *ForRangeStatement) String() string {
	op := ".."
	if s.Inclusive {
		op = "..="
	}
	return fmt.Sprintf("for %s in %s%s%s %s", s.VarName, s.Start.String(), op, s.End.String(), s.Body.String())
}

// ForEachStatement: for item in collection { ... }
type ForEachStatement struct {
	Tok        token.Token
	VarName    string
	Collection Expression
	Body       *BlockStatement
}

func (s *ForEachStatement) stmtNode()          {}
func (s *ForEachStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *ForEachStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *ForEachStatement) String() string {
	return fmt.Sprintf("for %s in %s %s", s.VarName, s.Collection.String(), s.Body.String())
}

// LoopStatement: loop { ... }
type LoopStatement struct {
	Tok  token.Token
	Body *BlockStatement
}

func (s *LoopStatement) stmtNode()          {}
func (s *LoopStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *LoopStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *LoopStatement) String() string     { return "loop " + s.Body.String() }

// FnDeclaration: fn name(params) [-> ReturnType] { ... }
type FnDeclaration struct {
	Tok        token.Token
	Name       string
	Params     []FnParam
	ReturnType *TypeAnnotation // nil for void
	Body       *BlockStatement
}

// FnParam is one function parameter: name [: Type]
type FnParam struct {
	Name string
	Type *TypeAnnotation
}

func (s *FnDeclaration) stmtNode()          {}
func (s *FnDeclaration) TokenLiteral() string { return s.Tok.Literal }
func (s *FnDeclaration) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *FnDeclaration) String() string {
	params := make([]string, len(s.Params))
	for i, p := range s.Params {
		if p.Type != nil {
			params[i] = p.Name + ": " + p.Type.String()
		} else {
			params[i] = p.Name
		}
	}
	ret := ""
	if s.ReturnType != nil {
		ret = " -> " + s.ReturnType.String()
	}
	return fmt.Sprintf("fn %s(%s)%s %s", s.Name, strings.Join(params, ", "), ret, s.Body.String())
}

// StructDeclaration: struct Name { field: Type, ... }
type StructDeclaration struct {
	Tok    token.Token
	Name   string
	Fields []StructField
}

// StructField is one field in a struct definition.
type StructField struct {
	Name string
	Type *TypeAnnotation
}

func (s *StructDeclaration) stmtNode()          {}
func (s *StructDeclaration) TokenLiteral() string { return s.Tok.Literal }
func (s *StructDeclaration) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *StructDeclaration) String() string {
	fields := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		fields[i] = "  " + f.Name + ": " + f.Type.String()
	}
	return fmt.Sprintf("struct %s {\n%s\n}", s.Name, strings.Join(fields, "\n"))
}

// MatchStatement: match expr { pattern => body, ... }
type MatchStatement struct {
	Tok     token.Token
	Subject Expression
	Arms    []MatchArm
}

// MatchArm is one arm of a match expression.
type MatchArm struct {
	Pattern MatchPattern
	Body    Statement // ExpressionStatement or BlockStatement
}

// MatchPattern variants
type MatchPattern interface {
	patternNode()
	String() string
}

type LiteralPattern struct{ Value Expression }        // 42, "hello", true, null
type IdentPattern struct{ Name string }               // variable binding
type WildcardPattern struct{}                         // _
type SomePattern struct{ Inner MatchPattern }         // Some(x)
type NonePattern struct{}                             // None
type OkPattern struct{ Inner MatchPattern }           // Ok(x)
type ErrPattern struct{ Inner MatchPattern }          // Err(e)
type RangePattern struct{ Low, High Expression; Inclusive bool } // 1..5 or 1..=5

func (p *LiteralPattern) patternNode()  {}
func (p *IdentPattern) patternNode()    {}
func (p *WildcardPattern) patternNode() {}
func (p *SomePattern) patternNode()     {}
func (p *NonePattern) patternNode()     {}
func (p *OkPattern) patternNode()       {}
func (p *ErrPattern) patternNode()      {}
func (p *RangePattern) patternNode()    {}

func (p *LiteralPattern) String() string  { return p.Value.String() }
func (p *IdentPattern) String() string    { return p.Name }
func (p *WildcardPattern) String() string { return "_" }
func (p *SomePattern) String() string     { return "Some(" + p.Inner.String() + ")" }
func (p *NonePattern) String() string     { return "None" }
func (p *OkPattern) String() string       { return "Ok(" + p.Inner.String() + ")" }
func (p *ErrPattern) String() string      { return "Err(" + p.Inner.String() + ")" }
func (p *RangePattern) String() string {
	op := ".."
	if p.Inclusive {
		op = "..="
	}
	return p.Low.String() + op + p.High.String()
}

func (s *MatchStatement) stmtNode()          {}
func (s *MatchStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *MatchStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *MatchStatement) String() string {
	return fmt.Sprintf("match %s { ... }", s.Subject.String())
}

// UseStatement: use path::to::module
type UseStatement struct {
	Tok  token.Token
	Path []string
}

func (s *UseStatement) stmtNode()          {}
func (s *UseStatement) TokenLiteral() string { return s.Tok.Literal }
func (s *UseStatement) Pos() (int, int)    { return s.Tok.Line, s.Tok.Col }
func (s *UseStatement) String() string     { return "use " + strings.Join(s.Path, "::") }

// ============================================================
// Expressions
// ============================================================

// IntLiteral: 42
type IntLiteral struct {
	Tok   token.Token
	Value int64
}

func (e *IntLiteral) exprNode()          {}
func (e *IntLiteral) TokenLiteral() string { return e.Tok.Literal }
func (e *IntLiteral) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *IntLiteral) String() string     { return fmt.Sprintf("%d", e.Value) }

// FloatLiteral: 3.14
type FloatLiteral struct {
	Tok   token.Token
	Value float64
}

func (e *FloatLiteral) exprNode()          {}
func (e *FloatLiteral) TokenLiteral() string { return e.Tok.Literal }
func (e *FloatLiteral) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *FloatLiteral) String() string     { return fmt.Sprintf("%g", e.Value) }

// StringLiteral: "hello"
type StringLiteral struct {
	Tok   token.Token
	Value string
}

func (e *StringLiteral) exprNode()          {}
func (e *StringLiteral) TokenLiteral() string { return e.Tok.Literal }
func (e *StringLiteral) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *StringLiteral) String() string     { return `"` + e.Value + `"` }

// FStringLiteral: f"hello {name}, you are {age} years old"
type FStringLiteral struct {
	Tok   token.Token
	Parts []FStringPart // alternating string segments and expressions
}

// FStringPart is either a raw string segment or an embedded expression.
type FStringPart struct {
	IsExpr bool
	Text   string     // non-empty when IsExpr == false
	Expr   Expression // non-nil when IsExpr == true
}

func (e *FStringLiteral) exprNode()          {}
func (e *FStringLiteral) TokenLiteral() string { return e.Tok.Literal }
func (e *FStringLiteral) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *FStringLiteral) String() string {
	var sb strings.Builder
	sb.WriteString("f\"")
	for _, p := range e.Parts {
		if p.IsExpr {
			sb.WriteString("{")
			sb.WriteString(p.Expr.String())
			sb.WriteString("}")
		} else {
			sb.WriteString(p.Text)
		}
	}
	sb.WriteString("\"")
	return sb.String()
}

// BoolLiteral: true / false
type BoolLiteral struct {
	Tok   token.Token
	Value bool
}

func (e *BoolLiteral) exprNode()          {}
func (e *BoolLiteral) TokenLiteral() string { return e.Tok.Literal }
func (e *BoolLiteral) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *BoolLiteral) String() string {
	if e.Value {
		return "true"
	}
	return "false"
}

// NullLiteral: null
type NullLiteral struct{ Tok token.Token }

func (e *NullLiteral) exprNode()          {}
func (e *NullLiteral) TokenLiteral() string { return e.Tok.Literal }
func (e *NullLiteral) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *NullLiteral) String() string     { return "null" }

// Identifier: foo, bar_baz
type Identifier struct {
	Tok  token.Token
	Name string
}

func (e *Identifier) exprNode()          {}
func (e *Identifier) TokenLiteral() string { return e.Tok.Literal }
func (e *Identifier) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *Identifier) String() string     { return e.Name }

// BinaryExpr: left op right
type BinaryExpr struct {
	Tok      token.Token
	Operator string
	Left     Expression
	Right    Expression
}

func (e *BinaryExpr) exprNode()          {}
func (e *BinaryExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *BinaryExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *BinaryExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", e.Left.String(), e.Operator, e.Right.String())
}

// UnaryExpr: op right
type UnaryExpr struct {
	Tok      token.Token
	Operator string
	Right    Expression
}

func (e *UnaryExpr) exprNode()          {}
func (e *UnaryExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *UnaryExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *UnaryExpr) String() string     { return fmt.Sprintf("(%s%s)", e.Operator, e.Right.String()) }

// CallExpr: callee(args...)
type CallExpr struct {
	Tok    token.Token
	Callee Expression
	Args   []Expression
}

func (e *CallExpr) exprNode()          {}
func (e *CallExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *CallExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *CallExpr) String() string {
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		args[i] = a.String()
	}
	return fmt.Sprintf("%s(%s)", e.Callee.String(), strings.Join(args, ", "))
}

// IndexExpr: collection[index]
type IndexExpr struct {
	Tok    token.Token
	Object Expression
	Index  Expression
}

func (e *IndexExpr) exprNode()          {}
func (e *IndexExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *IndexExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *IndexExpr) String() string {
	return fmt.Sprintf("%s[%s]", e.Object.String(), e.Index.String())
}

// FieldExpr: object.field
type FieldExpr struct {
	Tok    token.Token
	Object Expression
	Field  string
}

func (e *FieldExpr) exprNode()          {}
func (e *FieldExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *FieldExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *FieldExpr) String() string     { return e.Object.String() + "." + e.Field }

// MethodCallExpr: object.method(args...)
type MethodCallExpr struct {
	Tok    token.Token
	Object Expression
	Method string
	Args   []Expression
}

func (e *MethodCallExpr) exprNode()          {}
func (e *MethodCallExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *MethodCallExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *MethodCallExpr) String() string {
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		args[i] = a.String()
	}
	return fmt.Sprintf("%s.%s(%s)", e.Object.String(), e.Method, strings.Join(args, ", "))
}

// ArrayLiteral: [e1, e2, e3]
type ArrayLiteral struct {
	Tok      token.Token
	Elements []Expression
}

func (e *ArrayLiteral) exprNode()          {}
func (e *ArrayLiteral) TokenLiteral() string { return e.Tok.Literal }
func (e *ArrayLiteral) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *ArrayLiteral) String() string {
	elems := make([]string, len(e.Elements))
	for i, el := range e.Elements {
		elems[i] = el.String()
	}
	return "[" + strings.Join(elems, ", ") + "]"
}

// StructLiteral: Point { x: 10, y: 20 }
type StructLiteral struct {
	Tok      token.Token
	TypeName string
	Fields   []StructFieldValue
}

// StructFieldValue is a name: expr pair in a struct literal.
type StructFieldValue struct {
	Name  string
	Value Expression
}

func (e *StructLiteral) exprNode()          {}
func (e *StructLiteral) TokenLiteral() string { return e.Tok.Literal }
func (e *StructLiteral) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *StructLiteral) String() string {
	fields := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		fields[i] = f.Name + ": " + f.Value.String()
	}
	return fmt.Sprintf("%s { %s }", e.TypeName, strings.Join(fields, ", "))
}

// SomeExpr: Some(expr)
type SomeExpr struct {
	Tok   token.Token
	Value Expression
}

func (e *SomeExpr) exprNode()          {}
func (e *SomeExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *SomeExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *SomeExpr) String() string     { return "Some(" + e.Value.String() + ")" }

// NoneExpr: None
type NoneExpr struct{ Tok token.Token }

func (e *NoneExpr) exprNode()          {}
func (e *NoneExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *NoneExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *NoneExpr) String() string     { return "None" }

// OkExpr: Ok(expr)
type OkExpr struct {
	Tok   token.Token
	Value Expression
}

func (e *OkExpr) exprNode()          {}
func (e *OkExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *OkExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *OkExpr) String() string     { return "Ok(" + e.Value.String() + ")" }

// ErrExpr: Err(expr)
type ErrExpr struct {
	Tok   token.Token
	Value Expression
}

func (e *ErrExpr) exprNode()          {}
func (e *ErrExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *ErrExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *ErrExpr) String() string     { return "Err(" + e.Value.String() + ")" }

// GroupExpr: (expr) — explicit parenthesization
type GroupExpr struct {
	Tok   token.Token
	Inner Expression
}

func (e *GroupExpr) exprNode()          {}
func (e *GroupExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *GroupExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *GroupExpr) String() string     { return "(" + e.Inner.String() + ")" }

// RangeExpr: start..end or start..=end (used in for loops)
type RangeExpr struct {
	Tok       token.Token
	Start     Expression
	End       Expression
	Inclusive bool
}

func (e *RangeExpr) exprNode()          {}
func (e *RangeExpr) TokenLiteral() string { return e.Tok.Literal }
func (e *RangeExpr) Pos() (int, int)    { return e.Tok.Line, e.Tok.Col }
func (e *RangeExpr) String() string {
	op := ".."
	if e.Inclusive {
		op = "..="
	}
	return e.Start.String() + op + e.End.String()
}
