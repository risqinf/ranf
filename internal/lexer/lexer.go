// Package lexer implements the ranf tokenizer.
//
// The Lexer service transforms raw source text into a flat Token stream.
// It handles:
//   - Line/column tracking for precise error messages
//   - f-strings with nested expression scanning
//   - Single-line (//) and block (/* */) comments
//   - All compound operators (==, !=, ->, =>, .., ..=, **, +=, etc.)
//   - Numeric literals: integers (42, 0xFF, 0b1010, 0o17) and floats (3.14, 1e10)
//
// Usage:
//
//	svc := lexer.NewService()
//	tokens, errs := svc.Tokenize("source.ranf", src)
package lexer

import (
	"fmt"
	"strings"

	"github.com/risqinf/ranf/internal/token"
	"github.com/risqinf/ranf/pkg/errors"
)

// Service is the lexer microservice. Create with NewService().
type Service struct{}

// NewService returns a new Lexer service instance.
func NewService() *Service { return &Service{} }

// Tokenize converts src into a token slice.
// Returns the token list and any lexical errors encountered.
// Errors are non-fatal: scanning continues after an ILLEGAL token.
func (s *Service) Tokenize(filename, src string) ([]token.Token, errors.ErrorList) {
	l := &lexer{
		src:      src,
		filename: filename,
		pos:      0,
		line:     1,
		col:      1,
	}
	return l.scanAll()
}

// ============================================================
// Internal lexer state
// ============================================================

type lexer struct {
	src      string
	filename string
	pos      int // current read position
	line     int
	col      int
	errs     errors.ErrorList
}

// scanAll runs the full scan and returns the token stream.
func (l *lexer) scanAll() ([]token.Token, errors.ErrorList) {
	var tokens []token.Token
	for {
		tok := l.nextToken()
		tokens = append(tokens, tok)
		if tok.Type == token.EOF {
			break
		}
	}
	return tokens, l.errs
}

// ---- character helpers ----

func (l *lexer) cur() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *lexer) peek() byte {
	if l.pos+1 >= len(l.src) {
		return 0
	}
	return l.src[l.pos+1]
}

func (l *lexer) peek2() byte {
	if l.pos+2 >= len(l.src) {
		return 0
	}
	return l.src[l.pos+2]
}

func (l *lexer) advance() byte {
	ch := l.src[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return ch
}

func (l *lexer) match(ch byte) bool {
	if l.pos < len(l.src) && l.src[l.pos] == ch {
		l.advance()
		return true
	}
	return false
}

func (l *lexer) makeTok(t token.Type, lit string, line, col int) token.Token {
	return token.Token{Type: t, Literal: lit, Line: line, Col: col}
}

// ---- whitespace / comments ----

func (l *lexer) skipWhitespaceAndComments() {
	for l.pos < len(l.src) {
		ch := l.cur()
		switch {
		case ch == ' ' || ch == '\t' || ch == '\r':
			l.advance()
		case ch == '/' && l.peek() == '/':
			// single-line comment
			for l.pos < len(l.src) && l.cur() != '\n' {
				l.advance()
			}
		case ch == '/' && l.peek() == '*':
			// block comment
			l.advance(); l.advance() // consume /*
			for l.pos < len(l.src) {
				if l.cur() == '*' && l.peek() == '/' {
					l.advance(); l.advance() // consume */
					break
				}
				l.advance()
			}
		default:
			return
		}
	}
}

// ---- scanners ----

func (l *lexer) nextToken() token.Token {
	l.skipWhitespaceAndComments()

	if l.pos >= len(l.src) {
		return l.makeTok(token.EOF, "", l.line, l.col)
	}

	line, col := l.line, l.col
	ch := l.cur()

	// newline (statement separator)
	if ch == '\n' {
		l.advance()
		return l.makeTok(token.NEWLINE, "\\n", line, col)
	}

	// numeric literals
	if isDigit(ch) {
		return l.scanNumber(line, col)
	}

	// f-strings — must check BEFORE the general identifier path
	// because 'f' is a valid identifier start character
	if ch == 'f' && l.peek() == '"' {
		return l.scanFString(line, col)
	}

	// identifiers / keywords
	if isLetter(ch) {
		return l.scanIdent(line, col)
	}

	// string literals
	if ch == '"' {
		return l.scanString(line, col)
	}

	// operators / punctuation
	l.advance()
	switch ch {
	case '+':
		if l.match('=') {
			return l.makeTok(token.PLUS_ASSIGN, "+=", line, col)
		}
		return l.makeTok(token.PLUS, "+", line, col)
	case '-':
		if l.match('>') {
			return l.makeTok(token.ARROW, "->", line, col)
		}
		if l.match('=') {
			return l.makeTok(token.MINUS_ASSIGN, "-=", line, col)
		}
		return l.makeTok(token.MINUS, "-", line, col)
	case '*':
		if l.match('*') {
			return l.makeTok(token.POWER, "**", line, col)
		}
		if l.match('=') {
			return l.makeTok(token.STAR_ASSIGN, "*=", line, col)
		}
		return l.makeTok(token.STAR, "*", line, col)
	case '/':
		if l.match('=') {
			return l.makeTok(token.SLASH_ASSIGN, "/=", line, col)
		}
		return l.makeTok(token.SLASH, "/", line, col)
	case '%':
		return l.makeTok(token.PERCENT, "%", line, col)
	case '=':
		if l.match('=') {
			return l.makeTok(token.EQ, "==", line, col)
		}
		if l.match('>') {
			return l.makeTok(token.FAT_ARROW, "=>", line, col)
		}
		return l.makeTok(token.ASSIGN, "=", line, col)
	case '!':
		if l.match('=') {
			return l.makeTok(token.NEQ, "!=", line, col)
		}
		return l.makeTok(token.NOT, "!", line, col)
	case '<':
		if l.match('<') {
			return l.makeTok(token.SHL, "<<", line, col)
		}
		if l.match('=') {
			return l.makeTok(token.LTE, "<=", line, col)
		}
		return l.makeTok(token.LT, "<", line, col)
	case '>':
		if l.match('>') {
			return l.makeTok(token.SHR, ">>", line, col)
		}
		if l.match('=') {
			return l.makeTok(token.GTE, ">=", line, col)
		}
		return l.makeTok(token.GT, ">", line, col)
	case '&':
		if l.match('&') {
			return l.makeTok(token.AND, "&&", line, col)
		}
		return l.makeTok(token.BIT_AND, "&", line, col)
	case '|':
		if l.match('|') {
			return l.makeTok(token.OR, "||", line, col)
		}
		return l.makeTok(token.BIT_OR, "|", line, col)
	case '^':
		return l.makeTok(token.BIT_XOR, "^", line, col)
	case '.':
		if l.cur() == '.' {
			l.advance()
			if l.match('=') {
				return l.makeTok(token.RANGE_INCL, "..=", line, col)
			}
			return l.makeTok(token.RANGE, "..", line, col)
		}
		return l.makeTok(token.DOT, ".", line, col)
	case ':':
		if l.match(':') {
			return l.makeTok(token.DCOLON, "::", line, col)
		}
		return l.makeTok(token.COLON, ":", line, col)
	case '(':
		return l.makeTok(token.LPAREN, "(", line, col)
	case ')':
		return l.makeTok(token.RPAREN, ")", line, col)
	case '{':
		return l.makeTok(token.LBRACE, "{", line, col)
	case '}':
		return l.makeTok(token.RBRACE, "}", line, col)
	case '[':
		return l.makeTok(token.LBRACKET, "[", line, col)
	case ']':
		return l.makeTok(token.RBRACKET, "]", line, col)
	case ',':
		return l.makeTok(token.COMMA, ",", line, col)
	case ';':
		return l.makeTok(token.SEMICOLON, ";", line, col)
	}

	lit := string(ch)
	l.errs.Add(errors.New(errors.PhaseLex, line, col, "unexpected character: %q", ch))
	return l.makeTok(token.ILLEGAL, lit, line, col)
}

func (l *lexer) scanNumber(line, col int) token.Token {
	start := l.pos
	isFloat := false

	// Hex / binary / octal prefixes
	if l.cur() == '0' && l.pos+1 < len(l.src) {
		switch l.src[l.pos+1] {
		case 'x', 'X':
			l.advance(); l.advance()
			for isHexDigit(l.cur()) || l.cur() == '_' {
				l.advance()
			}
			return l.makeTok(token.INT, l.src[start:l.pos], line, col)
		case 'b', 'B':
			l.advance(); l.advance()
			for l.cur() == '0' || l.cur() == '1' || l.cur() == '_' {
				l.advance()
			}
			return l.makeTok(token.INT, l.src[start:l.pos], line, col)
		case 'o', 'O':
			l.advance(); l.advance()
			for (l.cur() >= '0' && l.cur() <= '7') || l.cur() == '_' {
				l.advance()
			}
			return l.makeTok(token.INT, l.src[start:l.pos], line, col)
		}
	}

	for isDigit(l.cur()) || l.cur() == '_' {
		l.advance()
	}
	if l.cur() == '.' && isDigit(l.peek()) {
		isFloat = true
		l.advance()
		for isDigit(l.cur()) || l.cur() == '_' {
			l.advance()
		}
	}
	if l.cur() == 'e' || l.cur() == 'E' {
		isFloat = true
		l.advance()
		if l.cur() == '+' || l.cur() == '-' {
			l.advance()
		}
		for isDigit(l.cur()) {
			l.advance()
		}
	}
	lit := l.src[start:l.pos]
	if isFloat {
		return l.makeTok(token.FLOAT, lit, line, col)
	}
	return l.makeTok(token.INT, lit, line, col)
}

func (l *lexer) scanIdent(line, col int) token.Token {
	start := l.pos
	for isLetter(l.cur()) || isDigit(l.cur()) {
		l.advance()
	}
	lit := l.src[start:l.pos]
	t := token.LookupIdent(lit)
	return l.makeTok(t, lit, line, col)
}

func (l *lexer) scanString(line, col int) token.Token {
	l.advance() // consume opening "
	var sb strings.Builder
	for l.pos < len(l.src) && l.cur() != '"' {
		ch := l.cur()
		if ch == '\\' {
			l.advance()
			switch l.cur() {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case '\\':
				sb.WriteByte('\\')
			case '"':
				sb.WriteByte('"')
			case '0':
				sb.WriteByte(0)
			default:
				sb.WriteByte('\\')
				sb.WriteByte(l.cur())
			}
			l.advance()
		} else {
			sb.WriteByte(ch)
			l.advance()
		}
	}
	if l.pos >= len(l.src) {
		l.errs.Add(errors.New(errors.PhaseLex, line, col, "unterminated string literal"))
	} else {
		l.advance() // consume closing "
	}
	return l.makeTok(token.STRING, sb.String(), line, col)
}

// scanFString scans an f"..." token. The raw literal (including { } parts)
// is stored in the token; the parser will split it into parts.
func (l *lexer) scanFString(line, col int) token.Token {
	l.advance() // consume 'f'
	l.advance() // consume '"'
	var sb strings.Builder
	depth := 0
	for l.pos < len(l.src) {
		ch := l.cur()
		if ch == '"' && depth == 0 {
			l.advance()
			break
		}
		if ch == '{' {
			depth++
		}
		if ch == '}' {
			depth--
		}
		sb.WriteByte(ch)
		l.advance()
	}
	return l.makeTok(token.FSTRING, sb.String(), line, col)
}

// ---- character classifiers ----

func isDigit(ch byte) bool  { return ch >= '0' && ch <= '9' }
func isHexDigit(ch byte) bool {
	return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}
func isLetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

// ---- utility ----
var _ = fmt.Sprintf // silence unused import
