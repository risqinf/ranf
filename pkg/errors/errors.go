// Package errors defines structured error types used across all ranf components.
// Each service (lexer, parser, analyzer, compiler, vm) produces errors that
// implement this interface, enabling consistent error formatting and reporting.
package errors

import "fmt"

// Phase identifies which compilation or runtime stage produced the error.
type Phase string

const (
	PhaseLex      Phase = "lexer"
	PhaseParse    Phase = "parser"
	PhaseAnalysis Phase = "analyzer"
	PhaseCompile  Phase = "compiler"
	PhaseRuntime  Phase = "runtime"
)

// RanfError is the structured error type for all ranf errors.
type RanfError struct {
	Phase   Phase  // which stage detected the error
	Message string // human-readable description
	Line    int    // source line (1-based, 0 if unknown)
	Col     int    // source column (1-based, 0 if unknown)
	Source  string // snippet of source that triggered the error
}

func (e *RanfError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("[%s] line %d:%d: %s", e.Phase, e.Line, e.Col, e.Message)
	}
	return fmt.Sprintf("[%s] %s", e.Phase, e.Message)
}

// New creates a RanfError for the given phase.
func New(phase Phase, line, col int, format string, args ...interface{}) *RanfError {
	return &RanfError{
		Phase:   phase,
		Message: fmt.Sprintf(format, args...),
		Line:    line,
		Col:     col,
	}
}

// NewRuntime creates a runtime phase error without position info.
func NewRuntime(format string, args ...interface{}) *RanfError {
	return &RanfError{
		Phase:   PhaseRuntime,
		Message: fmt.Sprintf(format, args...),
	}
}

// ErrorList aggregates multiple errors from a single phase.
type ErrorList []*RanfError

func (el ErrorList) Error() string {
	if len(el) == 0 {
		return "no errors"
	}
	if len(el) == 1 {
		return el[0].Error()
	}
	msg := fmt.Sprintf("%d errors:\n", len(el))
	for _, e := range el {
		msg += "  " + e.Error() + "\n"
	}
	return msg
}

// HasErrors returns true if the list is non-empty.
func (el ErrorList) HasErrors() bool { return len(el) > 0 }

// Add appends a new error.
func (el *ErrorList) Add(e *RanfError) { *el = append(*el, e) }
