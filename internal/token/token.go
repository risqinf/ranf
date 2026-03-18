// Package token defines all token types recognized by the ranf lexer.
// Each token type is a typed constant so that downstream packages (parser,
// analyzer) can use exhaustive switches without relying on string comparisons.
package token

// Type is the discriminant for a token.
type Type string

// Token carries the type, raw source text, and source position.
type Token struct {
	Type    Type
	Literal string // raw text from source
	Line    int    // 1-based line number
	Col     int    // 1-based column number
}

func (t Token) String() string {
	return string(t.Type) + "(" + t.Literal + ")"
}

// IsKeyword returns true if the token is a language keyword.
func (t Token) IsKeyword() bool {
	_, ok := keywords[t.Literal]
	return ok
}

// ============================================================
// Token type constants
// ============================================================

const (
	// Literals
	INT     Type = "INT"     // 42, -7
	FLOAT   Type = "FLOAT"   // 3.14
	STRING  Type = "STRING"  // "hello"
	FSTRING Type = "FSTRING" // f"hello {name}"
	TRUE    Type = "true"
	FALSE   Type = "false"
	NULL    Type = "null"

	// Identifier
	IDENT Type = "IDENT"

	// ---- Keywords ----
	LET      Type = "let"
	MUT      Type = "mut"
	FN       Type = "fn"
	STRUCT   Type = "struct"
	IMPL     Type = "impl"
	MATCH    Type = "match"
	IF       Type = "if"
	ELSE     Type = "else"
	WHILE    Type = "while"
	FOR      Type = "for"
	IN       Type = "in"
	LOOP     Type = "loop"
	RETURN   Type = "return"
	BREAK    Type = "break"
	CONTINUE Type = "continue"
	PUB      Type = "pub"
	USE      Type = "use"

	// Option / Result constructors (treated as keywords)
	SOME Type = "Some"
	NONE Type = "None"
	OK   Type = "Ok"
	ERR  Type = "Err"

	// ---- Primitive type names (used in annotations) ----
	TINT   Type = "int"
	TFLOAT Type = "float"
	TSTR   Type = "str"
	TBOOL  Type = "bool"

	// ---- Arithmetic operators ----
	PLUS    Type = "+"
	MINUS   Type = "-"
	STAR    Type = "*"
	SLASH   Type = "/"
	PERCENT Type = "%"
	POWER   Type = "**"

	// ---- Comparison operators ----
	EQ  Type = "=="
	NEQ Type = "!="
	LT  Type = "<"
	GT  Type = ">"
	LTE Type = "<="
	GTE Type = ">="

	// ---- Logical operators ----
	AND Type = "&&"
	OR  Type = "||"
	NOT Type = "!"

	// ---- Bitwise operators ----
	BIT_AND Type = "&"
	BIT_OR  Type = "|"
	BIT_XOR Type = "^"
	SHL     Type = "<<"
	SHR     Type = ">>"

	// ---- Assignment ----
	ASSIGN      Type = "="
	PLUS_ASSIGN Type = "+="
	MINUS_ASSIGN Type = "-="
	STAR_ASSIGN  Type = "*="
	SLASH_ASSIGN Type = "/="

	// ---- Punctuation ----
	LPAREN   Type = "("
	RPAREN   Type = ")"
	LBRACE   Type = "{"
	RBRACE   Type = "}"
	LBRACKET Type = "["
	RBRACKET Type = "]"
	COMMA    Type = ","
	SEMICOLON Type = ";"
	COLON    Type = ":"
	DOT      Type = "."

	// ---- Compound punctuation ----
	ARROW      Type = "->"  // fn return type
	FAT_ARROW  Type = "=>"  // match arm
	RANGE      Type = ".."  // exclusive range  0..10
	RANGE_INCL Type = "..=" // inclusive range  0..=10
	DCOLON     Type = "::"  // path separator

	// ---- Special ----
	NEWLINE Type = "NEWLINE"
	EOF     Type = "EOF"
	ILLEGAL Type = "ILLEGAL"
)

// keywords maps identifier text to the appropriate keyword token type.
// LookupIdent checks this map when the lexer reads an alphabetic token.
var keywords = map[string]Type{
	"let":      LET,
	"mut":      MUT,
	"fn":       FN,
	"struct":   STRUCT,
	"impl":     IMPL,
	"match":    MATCH,
	"if":       IF,
	"else":     ELSE,
	"while":    WHILE,
	"for":      FOR,
	"in":       IN,
	"loop":     LOOP,
	"return":   RETURN,
	"break":    BREAK,
	"continue": CONTINUE,
	"pub":      PUB,
	"use":      USE,
	"true":     TRUE,
	"false":    FALSE,
	"null":     NULL,
	"Some":     SOME,
	"None":     NONE,
	"Ok":       OK,
	"Err":      ERR,
	"int":      TINT,
	"float":    TFLOAT,
	"str":      TSTR,
	"bool":     TBOOL,
}

// LookupIdent returns the keyword type for s, or IDENT if s is not a keyword.
func LookupIdent(s string) Type {
	if t, ok := keywords[s]; ok {
		return t
	}
	return IDENT
}

// Precedence levels for Pratt parser (higher = tighter binding).
type Precedence int

const (
	PREC_LOWEST     Precedence = iota
	PREC_ASSIGN                // =
	PREC_OR                    // ||
	PREC_AND                   // &&
	PREC_EQUALITY              // == !=
	PREC_COMPARISON            // < > <= >=
	PREC_BITWISE               // & | ^
	PREC_SHIFT                 // << >>
	PREC_SUM                   // + -
	PREC_PRODUCT               // * / %
	PREC_POWER                 // **
	PREC_UNARY                 // ! -
	PREC_CALL                  // ()
	PREC_INDEX                 // []
	PREC_FIELD                 // .
)

// InfixPrecedence maps infix operator token types to their parsing precedence.
var InfixPrecedence = map[Type]Precedence{
	OR:           PREC_OR,
	AND:          PREC_AND,
	EQ:           PREC_EQUALITY,
	NEQ:          PREC_EQUALITY,
	LT:           PREC_COMPARISON,
	GT:           PREC_COMPARISON,
	LTE:          PREC_COMPARISON,
	GTE:          PREC_COMPARISON,
	BIT_OR:       PREC_BITWISE,
	BIT_XOR:      PREC_BITWISE,
	BIT_AND:      PREC_BITWISE,
	SHL:          PREC_SHIFT,
	SHR:          PREC_SHIFT,
	PLUS:         PREC_SUM,
	MINUS:        PREC_SUM,
	STAR:         PREC_PRODUCT,
	SLASH:        PREC_PRODUCT,
	PERCENT:      PREC_PRODUCT,
	POWER:        PREC_POWER,
	LPAREN:       PREC_CALL,
	LBRACKET:     PREC_INDEX,
	DOT:          PREC_FIELD,
}
