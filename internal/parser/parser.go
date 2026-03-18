// Package parser converts a token stream into an AST.
//
// Algorithm: Pratt (top-down operator precedence) for expressions,
// recursive descent for statements.
//
// Key design decisions:
//   - Struct literals (Point { x: 1 }) are only allowed in value contexts.
//     In condition contexts (if/while/for) they are suppressed to avoid
//     ambiguity with block statements, mirroring Rust's grammar.
//   - f-string interpolation expressions are stored as raw text and
//     re-parsed at evaluation time to avoid import cycles.
//
// Usage:
//
//	svc := parser.NewService()
//	program, errs := svc.Parse(tokens)
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/risqinf/ranf/internal/ast"
	"github.com/risqinf/ranf/internal/token"
	"github.com/risqinf/ranf/pkg/errors"
)

// Service is the parser microservice.
type Service struct{}

// NewService returns a new Parser service.
func NewService() *Service { return &Service{} }

// Parse transforms a token slice into a *ast.Program.
func (s *Service) Parse(tokens []token.Token) (*ast.Program, errors.ErrorList) {
	p := &parser{tokens: tokens, pos: 0}
	return p.parseProgram()
}

// ============================================================
// Internal state
// ============================================================

type parser struct {
	tokens []token.Token
	pos    int
	errs   errors.ErrorList
}

func (p *parser) cur() token.Token {
	if p.pos >= len(p.tokens) {
		return token.Token{Type: token.EOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) peek() token.Token {
	if p.pos+1 >= len(p.tokens) {
		return token.Token{Type: token.EOF}
	}
	return p.tokens[p.pos+1]
}

func (p *parser) advance() token.Token {
	tok := p.cur()
	p.pos++
	return tok
}

func (p *parser) skipNewlines() {
	for p.cur().Type == token.NEWLINE || p.cur().Type == token.SEMICOLON {
		p.advance()
	}
}

func (p *parser) expect(t token.Type) (token.Token, bool) {
	if p.cur().Type == t {
		return p.advance(), true
	}
	tok := p.cur()
	p.errs.Add(errors.New(errors.PhaseParse, tok.Line, tok.Col,
		"expected %q, got %q (%q)", t, tok.Type, tok.Literal))
	return tok, false
}

func (p *parser) match(t token.Type) bool {
	if p.cur().Type == t {
		p.advance()
		return true
	}
	return false
}

func (p *parser) atEnd() bool { return p.cur().Type == token.EOF }

func (p *parser) addError(tok token.Token, format string, args ...interface{}) {
	p.errs.Add(errors.New(errors.PhaseParse, tok.Line, tok.Col, format, args...))
}

// ============================================================
// Program
// ============================================================

func (p *parser) parseProgram() (*ast.Program, errors.ErrorList) {
	program := &ast.Program{}
	p.skipNewlines()
	for !p.atEnd() {
		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
		}
		p.skipNewlines()
	}
	return program, p.errs
}

// ============================================================
// Statements
// ============================================================

func (p *parser) parseStatement() ast.Statement {
	tok := p.cur()
	switch tok.Type {
	case token.LET:
		return p.parseLetStatement()
	case token.FN:
		return p.parseFnDeclaration()
	case token.STRUCT:
		return p.parseStructDeclaration()
	case token.IF:
		return p.parseIfStatement()
	case token.WHILE:
		return p.parseWhileStatement()
	case token.FOR:
		return p.parseForStatement()
	case token.LOOP:
		return p.parseLoopStatement()
	case token.MATCH:
		return p.parseMatchStatement()
	case token.RETURN:
		return p.parseReturnStatement()
	case token.BREAK:
		p.advance()
		return &ast.BreakStatement{Tok: tok}
	case token.CONTINUE:
		p.advance()
		return &ast.ContinueStatement{Tok: tok}
	case token.USE:
		return p.parseUseStatement()
	default:
		return p.parseExprOrAssign()
	}
}

func (p *parser) parseLetStatement() *ast.LetStatement {
	tok := p.advance() // 'let'
	mutable := p.match(token.MUT)
	nameTok, ok := p.expect(token.IDENT)
	if !ok {
		return nil
	}
	var ann *ast.TypeAnnotation
	if p.cur().Type == token.COLON {
		p.advance()
		ann = p.parseTypeAnnotation()
	}
	if _, ok := p.expect(token.ASSIGN); !ok {
		return nil
	}
	p.skipNewlines()
	val := p.parseExpression(token.PREC_LOWEST, true) // struct literals allowed in RHS
	return &ast.LetStatement{Tok: tok, Mutable: mutable, Name: nameTok.Literal, Type: ann, Value: val}
}

func (p *parser) parseTypeAnnotation() *ast.TypeAnnotation {
	tok := p.cur()
	name := ""
	switch tok.Type {
	case token.TINT:
		name = "int"
	case token.TFLOAT:
		name = "float"
	case token.TSTR:
		name = "str"
	case token.TBOOL:
		name = "bool"
	case token.IDENT:
		name = tok.Literal
		// Handle "array" as a built-in type name
		if tok.Literal == "array" {
			name = "array"
		}
	default:
		p.addError(tok, "expected type name, got %q", tok.Literal)
		return &ast.TypeAnnotation{Name: "?"}
	}
	p.advance()
	ann := &ast.TypeAnnotation{Name: name}
	// optional generic params: Option<int>, Result<str, str>
	if p.cur().Type == token.LT {
		p.advance()
		for p.cur().Type != token.GT && !p.atEnd() {
			ann.Params = append(ann.Params, p.parseTypeAnnotation())
			p.match(token.COMMA)
		}
		p.expect(token.GT)
	}
	return ann
}

func (p *parser) parseFnDeclaration() *ast.FnDeclaration {
	tok := p.advance() // 'fn'
	nameTok, ok := p.expect(token.IDENT)
	if !ok {
		return nil
	}
	if _, ok := p.expect(token.LPAREN); !ok {
		return nil
	}
	params := p.parseFnParams()
	if _, ok := p.expect(token.RPAREN); !ok {
		return nil
	}
	var retType *ast.TypeAnnotation
	if p.cur().Type == token.ARROW {
		p.advance()
		retType = p.parseTypeAnnotation()
	}
	p.skipNewlines()
	body := p.parseBlock()
	return &ast.FnDeclaration{Tok: tok, Name: nameTok.Literal, Params: params, ReturnType: retType, Body: body}
}

func (p *parser) parseFnParams() []ast.FnParam {
	var params []ast.FnParam
	for p.cur().Type != token.RPAREN && !p.atEnd() {
		nameTok, ok := p.expect(token.IDENT)
		if !ok {
			break
		}
		param := ast.FnParam{Name: nameTok.Literal}
		if p.cur().Type == token.COLON {
			p.advance()
			param.Type = p.parseTypeAnnotation()
		}
		params = append(params, param)
		p.match(token.COMMA)
	}
	return params
}

func (p *parser) parseStructDeclaration() *ast.StructDeclaration {
	tok := p.advance() // 'struct'
	nameTok, ok := p.expect(token.IDENT)
	if !ok {
		return nil
	}
	p.skipNewlines()
	if _, ok := p.expect(token.LBRACE); !ok {
		return nil
	}
	p.skipNewlines()
	var fields []ast.StructField
	for p.cur().Type != token.RBRACE && !p.atEnd() {
		fn, ok := p.expect(token.IDENT)
		if !ok {
			break
		}
		p.expect(token.COLON)
		ft := p.parseTypeAnnotation()
		fields = append(fields, ast.StructField{Name: fn.Literal, Type: ft})
		p.match(token.COMMA)
		p.skipNewlines()
	}
	p.expect(token.RBRACE)
	return &ast.StructDeclaration{Tok: tok, Name: nameTok.Literal, Fields: fields}
}

func (p *parser) parseIfStatement() *ast.IfStatement {
	tok := p.advance() // 'if'
	// Struct literals NOT allowed in condition — prevents { from ambiguity
	cond := p.parseExpression(token.PREC_LOWEST, false)
	p.skipNewlines()
	body := p.parseBlock()
	stmt := &ast.IfStatement{Tok: tok, Condition: cond, Consequence: body}
	for p.cur().Type == token.ELSE {
		p.advance()
		if p.cur().Type == token.IF {
			p.advance()
			ec := p.parseExpression(token.PREC_LOWEST, false)
			p.skipNewlines()
			eb := p.parseBlock()
			stmt.ElseIfs = append(stmt.ElseIfs, ast.ElseIfClause{Condition: ec, Consequence: eb})
		} else {
			p.skipNewlines()
			stmt.Alternative = p.parseBlock()
			break
		}
	}
	return stmt
}

func (p *parser) parseWhileStatement() *ast.WhileStatement {
	tok := p.advance() // 'while'
	cond := p.parseExpression(token.PREC_LOWEST, false) // no struct lit in condition
	p.skipNewlines()
	body := p.parseBlock()
	return &ast.WhileStatement{Tok: tok, Condition: cond, Body: body}
}

func (p *parser) parseForStatement() ast.Statement {
	tok := p.advance() // 'for'
	varTok, ok := p.expect(token.IDENT)
	if !ok {
		return nil
	}
	if _, ok := p.expect(token.IN); !ok {
		return nil
	}
	// No struct lit in for-in expression — { would be the loop body
	start := p.parseExpression(token.PREC_LOWEST, false)
	if p.cur().Type == token.RANGE || p.cur().Type == token.RANGE_INCL {
		incl := p.cur().Type == token.RANGE_INCL
		p.advance()
		end := p.parseExpression(token.PREC_LOWEST, false)
		p.skipNewlines()
		body := p.parseBlock()
		return &ast.ForRangeStatement{Tok: tok, VarName: varTok.Literal, Start: start, End: end, Inclusive: incl, Body: body}
	}
	p.skipNewlines()
	body := p.parseBlock()
	return &ast.ForEachStatement{Tok: tok, VarName: varTok.Literal, Collection: start, Body: body}
}

func (p *parser) parseLoopStatement() *ast.LoopStatement {
	tok := p.advance() // 'loop'
	p.skipNewlines()
	return &ast.LoopStatement{Tok: tok, Body: p.parseBlock()}
}

func (p *parser) parseMatchStatement() *ast.MatchStatement {
	tok := p.advance() // 'match'
	subject := p.parseExpression(token.PREC_LOWEST, false) // no struct lit as subject
	p.skipNewlines()
	if _, ok := p.expect(token.LBRACE); !ok {
		return nil
	}
	p.skipNewlines()
	var arms []ast.MatchArm
	for p.cur().Type != token.RBRACE && !p.atEnd() {
		pat := p.parseMatchPattern()
		p.skipNewlines()
		p.expect(token.FAT_ARROW)
		p.skipNewlines()
		var body ast.Statement
		if p.cur().Type == token.LBRACE {
			body = p.parseBlock()
		} else {
			etok := p.cur()
			expr := p.parseExpression(token.PREC_LOWEST, false)
			body = &ast.ExpressionStatement{Tok: etok, Expr: expr}
		}
		arms = append(arms, ast.MatchArm{Pattern: pat, Body: body})
		p.match(token.COMMA)
		p.skipNewlines()
	}
	p.expect(token.RBRACE)
	return &ast.MatchStatement{Tok: tok, Subject: subject, Arms: arms}
}

func (p *parser) parseMatchPattern() ast.MatchPattern {
	tok := p.cur()
	switch tok.Type {
	case token.IDENT:
		if tok.Literal == "_" {
			p.advance()
			return &ast.WildcardPattern{}
		}
		p.advance()
		return &ast.IdentPattern{Name: tok.Literal}
	case token.SOME:
		p.advance()
		p.expect(token.LPAREN)
		inner := p.parseMatchPattern()
		p.expect(token.RPAREN)
		return &ast.SomePattern{Inner: inner}
	case token.NONE:
		p.advance()
		return &ast.NonePattern{}
	case token.OK:
		p.advance()
		p.expect(token.LPAREN)
		inner := p.parseMatchPattern()
		p.expect(token.RPAREN)
		return &ast.OkPattern{Inner: inner}
	case token.ERR:
		p.advance()
		p.expect(token.LPAREN)
		inner := p.parseMatchPattern()
		p.expect(token.RPAREN)
		return &ast.ErrPattern{Inner: inner}
	default:
		expr := p.parseExpression(token.PREC_LOWEST, false)
		if p.cur().Type == token.RANGE || p.cur().Type == token.RANGE_INCL {
			incl := p.cur().Type == token.RANGE_INCL
			p.advance()
			high := p.parseExpression(token.PREC_LOWEST, false)
			return &ast.RangePattern{Low: expr, High: high, Inclusive: incl}
		}
		return &ast.LiteralPattern{Value: expr}
	}
}

func (p *parser) parseReturnStatement() *ast.ReturnStatement {
	tok := p.advance() // 'return'
	if p.cur().Type == token.NEWLINE || p.cur().Type == token.RBRACE ||
		p.cur().Type == token.EOF || p.cur().Type == token.SEMICOLON {
		return &ast.ReturnStatement{Tok: tok}
	}
	return &ast.ReturnStatement{Tok: tok, Value: p.parseExpression(token.PREC_LOWEST, true)}
}

func (p *parser) parseUseStatement() *ast.UseStatement {
	tok := p.advance() // 'use'
	var path []string
	n, ok := p.expect(token.IDENT)
	if !ok {
		return nil
	}
	path = append(path, n.Literal)
	for p.cur().Type == token.DCOLON {
		p.advance()
		n, ok = p.expect(token.IDENT)
		if !ok {
			break
		}
		path = append(path, n.Literal)
	}
	return &ast.UseStatement{Tok: tok, Path: path}
}

func (p *parser) parseExprOrAssign() ast.Statement {
	tok := p.cur()
	expr := p.parseExpression(token.PREC_LOWEST, true)
	if expr == nil {
		return nil
	}
	cur := p.cur()
	switch cur.Type {
	case token.ASSIGN, token.PLUS_ASSIGN, token.MINUS_ASSIGN, token.STAR_ASSIGN, token.SLASH_ASSIGN:
		op := cur.Literal
		p.advance()
		p.skipNewlines()
		rhs := p.parseExpression(token.PREC_LOWEST, true)
		return &ast.AssignStatement{Tok: tok, Target: expr, Operator: op, Value: rhs}
	}
	return &ast.ExpressionStatement{Tok: tok, Expr: expr}
}

func (p *parser) parseBlock() *ast.BlockStatement {
	tok, ok := p.expect(token.LBRACE)
	if !ok {
		return &ast.BlockStatement{Tok: tok}
	}
	p.skipNewlines()
	blk := &ast.BlockStatement{Tok: tok}
	for p.cur().Type != token.RBRACE && !p.atEnd() {
		stmt := p.parseStatement()
		if stmt != nil {
			blk.Statements = append(blk.Statements, stmt)
		}
		p.skipNewlines()
	}
	p.expect(token.RBRACE)
	return blk
}

// ============================================================
// Expressions — Pratt parser
//
// allowStructLit: when true, "Ident {" is parsed as a struct literal.
//                 when false (condition contexts), "Ident" is just an identifier.
// ============================================================

func (p *parser) parseExpression(minPrec token.Precedence, allowStructLit bool) ast.Expression {
	left := p.parsePrefix(allowStructLit)
	if left == nil {
		return nil
	}
	for {
		prec, ok := token.InfixPrecedence[p.cur().Type]
		if !ok || prec <= minPrec {
			break
		}
		left = p.parseInfix(left, prec, allowStructLit)
	}
	return left
}

func (p *parser) parsePrefix(allowStructLit bool) ast.Expression {
	tok := p.cur()
	switch tok.Type {
	case token.INT:
		p.advance()
		return p.parseInt(tok)
	case token.FLOAT:
		p.advance()
		return p.parseFloat(tok)
	case token.STRING:
		p.advance()
		return &ast.StringLiteral{Tok: tok, Value: tok.Literal}
	case token.FSTRING:
		p.advance()
		return &ast.FStringLiteral{Tok: tok, Parts: parseFStringParts(tok)}
	case token.TRUE:
		p.advance()
		return &ast.BoolLiteral{Tok: tok, Value: true}
	case token.FALSE:
		p.advance()
		return &ast.BoolLiteral{Tok: tok, Value: false}
	case token.NULL:
		p.advance()
		return &ast.NullLiteral{Tok: tok}
	// Primitive type keywords used as conversion functions: int(x), float(x), str(x), bool(x)
	case token.TINT, token.TFLOAT, token.TSTR, token.TBOOL:
		p.advance()
		return &ast.Identifier{Tok: tok, Name: tok.Literal}

	case token.IDENT:
		p.advance()
		// Struct literal: only when IDENT is followed by { AND struct lits are allowed
		if allowStructLit && p.cur().Type == token.LBRACE {
			return p.parseStructLiteral(tok)
		}
		return &ast.Identifier{Tok: tok, Name: tok.Literal}
	case token.SOME:
		p.advance()
		p.expect(token.LPAREN)
		inner := p.parseExpression(token.PREC_LOWEST, true)
		p.expect(token.RPAREN)
		return &ast.SomeExpr{Tok: tok, Value: inner}
	case token.NONE:
		p.advance()
		return &ast.NoneExpr{Tok: tok}
	case token.OK:
		p.advance()
		p.expect(token.LPAREN)
		inner := p.parseExpression(token.PREC_LOWEST, true)
		p.expect(token.RPAREN)
		return &ast.OkExpr{Tok: tok, Value: inner}
	case token.ERR:
		p.advance()
		p.expect(token.LPAREN)
		inner := p.parseExpression(token.PREC_LOWEST, true)
		p.expect(token.RPAREN)
		return &ast.ErrExpr{Tok: tok, Value: inner}
	case token.MINUS, token.NOT:
		p.advance()
		right := p.parseExpression(token.PREC_UNARY, allowStructLit)
		return &ast.UnaryExpr{Tok: tok, Operator: tok.Literal, Right: right}
	case token.LPAREN:
		p.advance()
		p.skipNewlines()
		inner := p.parseExpression(token.PREC_LOWEST, true)
		p.skipNewlines()
		p.expect(token.RPAREN)
		return &ast.GroupExpr{Tok: tok, Inner: inner}
	case token.LBRACKET:
		return p.parseArrayLiteral()
	default:
		p.addError(tok, "unexpected token in expression: %q (%q)", tok.Type, tok.Literal)
		p.advance()
		return nil
	}
}

func (p *parser) parseInt(tok token.Token) *ast.IntLiteral {
	lit := strings.ReplaceAll(tok.Literal, "_", "")
	var val int64
	var err error
	switch {
	case strings.HasPrefix(lit, "0x") || strings.HasPrefix(lit, "0X"):
		val, err = strconv.ParseInt(lit[2:], 16, 64)
	case strings.HasPrefix(lit, "0b") || strings.HasPrefix(lit, "0B"):
		val, err = strconv.ParseInt(lit[2:], 2, 64)
	case strings.HasPrefix(lit, "0o") || strings.HasPrefix(lit, "0O"):
		val, err = strconv.ParseInt(lit[2:], 8, 64)
	default:
		val, err = strconv.ParseInt(lit, 10, 64)
	}
	if err != nil {
		p.addError(tok, "invalid integer: %s", tok.Literal)
		return &ast.IntLiteral{Tok: tok, Value: 0}
	}
	return &ast.IntLiteral{Tok: tok, Value: val}
}

func (p *parser) parseFloat(tok token.Token) *ast.FloatLiteral {
	lit := strings.ReplaceAll(tok.Literal, "_", "")
	val, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		p.addError(tok, "invalid float: %s", tok.Literal)
		return &ast.FloatLiteral{Tok: tok, Value: 0}
	}
	return &ast.FloatLiteral{Tok: tok, Value: val}
}

func (p *parser) parseInfix(left ast.Expression, prec token.Precedence, allowStructLit bool) ast.Expression {
	tok := p.cur()
	switch tok.Type {
	case token.LPAREN:
		p.advance()
		args := p.parseCallArgs()
		p.expect(token.RPAREN)
		return &ast.CallExpr{Tok: tok, Callee: left, Args: args}
	case token.LBRACKET:
		p.advance()
		idx := p.parseExpression(token.PREC_LOWEST, false)
		p.expect(token.RBRACKET)
		return &ast.IndexExpr{Tok: tok, Object: left, Index: idx}
	case token.DOT:
		p.advance()
		fieldTok, ok := p.expect(token.IDENT)
		if !ok {
			return left
		}
		if p.cur().Type == token.LPAREN {
			p.advance()
			args := p.parseCallArgs()
			p.expect(token.RPAREN)
			return &ast.MethodCallExpr{Tok: tok, Object: left, Method: fieldTok.Literal, Args: args}
		}
		return &ast.FieldExpr{Tok: tok, Object: left, Field: fieldTok.Literal}
	default:
		op := tok.Literal
		p.advance()
		right := p.parseExpression(prec, allowStructLit)
		return &ast.BinaryExpr{Tok: tok, Operator: op, Left: left, Right: right}
	}
}

func (p *parser) parseCallArgs() []ast.Expression {
	var args []ast.Expression
	p.skipNewlines()
	for p.cur().Type != token.RPAREN && !p.atEnd() {
		arg := p.parseExpression(token.PREC_LOWEST, true)
		if arg != nil {
			args = append(args, arg)
		}
		p.skipNewlines()
		p.match(token.COMMA)
		p.skipNewlines()
	}
	return args
}

func (p *parser) parseArrayLiteral() *ast.ArrayLiteral {
	tok := p.advance() // '['
	p.skipNewlines()
	var elems []ast.Expression
	for p.cur().Type != token.RBRACKET && !p.atEnd() {
		el := p.parseExpression(token.PREC_LOWEST, true)
		if el != nil {
			elems = append(elems, el)
		}
		p.skipNewlines()
		p.match(token.COMMA)
		p.skipNewlines()
	}
	p.expect(token.RBRACKET)
	return &ast.ArrayLiteral{Tok: tok, Elements: elems}
}

func (p *parser) parseStructLiteral(nameTok token.Token) *ast.StructLiteral {
	p.advance() // '{'
	p.skipNewlines()
	var fields []ast.StructFieldValue
	for p.cur().Type != token.RBRACE && !p.atEnd() {
		fn, ok := p.expect(token.IDENT)
		if !ok {
			break
		}
		p.expect(token.COLON)
		val := p.parseExpression(token.PREC_LOWEST, true)
		fields = append(fields, ast.StructFieldValue{Name: fn.Literal, Value: val})
		p.skipNewlines()
		p.match(token.COMMA)
		p.skipNewlines()
	}
	p.expect(token.RBRACE)
	return &ast.StructLiteral{Tok: nameTok, TypeName: nameTok.Literal, Fields: fields}
}

// parseFStringParts splits the raw f-string template into text/placeholder segments.
// Embedded expressions are stored as raw text strings and re-parsed at eval time.
func parseFStringParts(tok token.Token) []ast.FStringPart {
	raw := tok.Literal
	var parts []ast.FStringPart
	i := 0
	for i < len(raw) {
		if raw[i] == '{' {
			depth := 0
			j := i
			for j < len(raw) {
				if raw[j] == '{' {
					depth++
				} else if raw[j] == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
				j++
			}
			inner := raw[i+1 : j]
			parts = append(parts, ast.FStringPart{IsExpr: true, Text: inner})
			i = j + 1
		} else {
			j := i
			for j < len(raw) && raw[j] != '{' {
				j++
			}
			if j > i {
				parts = append(parts, ast.FStringPart{IsExpr: false, Text: raw[i:j]})
			}
			i = j
		}
	}
	return parts
}

var _ = fmt.Sprintf
