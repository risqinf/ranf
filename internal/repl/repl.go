// Package repl implements the ranf interactive Read-Eval-Print Loop.
//
// The REPL service:
//   - Reads input line-by-line from stdin
//   - Detects incomplete input (open braces) and prompts for continuation
//   - Parses and evaluates each complete expression or statement
//   - Pretty-prints results, errors, and timing information
//   - Maintains a persistent environment so definitions accumulate across lines
//
// Usage:
//
//	svc := repl.NewService()
//	svc.Start(os.Stdin, os.Stdout)
package repl

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/risqinf/ranf/internal/analyzer"
	"github.com/risqinf/ranf/internal/builtin"
	"github.com/risqinf/ranf/internal/lexer"
	"github.com/risqinf/ranf/internal/object"
	"github.com/risqinf/ranf/internal/parser"
	"github.com/risqinf/ranf/internal/vm"
)

const (
	promptPrimary      = "ranf> "
	promptContinuation = " ...> "
	version            = "0.1.0"
)

// Service is the REPL microservice.
type Service struct {
	lexer    *lexer.Service
	parser   *parser.Service
	analyzer *analyzer.Service
	vm       *vm.Service
	env      *object.Environment
}

// NewService creates a REPL service with a fresh environment.
func NewService() *Service {
	b := builtin.NewRegistry()
	return &Service{
		lexer:    lexer.NewService(),
		parser:   parser.NewService(),
		analyzer: analyzer.NewService(),
		vm:       vm.NewService(b),
		env:      object.NewEnvironment(),
	}
}

// Start runs the REPL loop until EOF or :quit.
func (s *Service) Start(in io.Reader, out io.Writer) {
	fmt.Fprintf(out, "ranf %s — interactive mode\n", version)
	fmt.Fprintf(out, "Type :help for commands, :quit to exit\n\n")

	scanner := bufio.NewScanner(in)
	var buffer strings.Builder
	depth := 0

	for {
		if depth == 0 {
			fmt.Fprint(out, promptPrimary)
		} else {
			fmt.Fprint(out, promptContinuation)
		}

		if !scanner.Scan() {
			fmt.Fprintln(out, "\nBye!")
			return
		}

		line := scanner.Text()

		// REPL commands
		switch strings.TrimSpace(line) {
		case ":quit", ":q", ":exit":
			fmt.Fprintln(out, "Bye!")
			return
		case ":help", ":h":
			printHelp(out)
			continue
		case ":env":
			// Not implemented: would print current bindings
			fmt.Fprintln(out, "(env listing not yet implemented)")
			continue
		case ":clear":
			s.env = object.NewEnvironment()
			fmt.Fprintln(out, "Environment cleared.")
			continue
		}

		// Accumulate multi-line input
		buffer.WriteString(line)
		buffer.WriteString("\n")

		for _, ch := range line {
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
			}
		}

		if depth > 0 {
			// Wait for closing braces
			continue
		}
		if depth < 0 {
			depth = 0
		}

		src := buffer.String()
		buffer.Reset()

		result := s.eval(src)
		if result != "" {
			fmt.Fprintln(out, result)
		}
	}
}

// eval runs a single source snippet and returns the output string.
func (s *Service) eval(src string) string {
	start := time.Now()

	// Lex
	tokens, lexErrs := s.lexer.Tokenize("<repl>", src)
	if lexErrs.HasErrors() {
		return formatErrors("Lex error", lexErrs.Error())
	}

	// Parse
	prog, parseErrs := s.parser.Parse(tokens)
	if parseErrs.HasErrors() {
		return formatErrors("Parse error", parseErrs.Error())
	}

	if len(prog.Statements) == 0 {
		return ""
	}

	// Analyze (non-fatal: print warnings but continue)
	analyzeErrs := s.analyzer.Analyze(prog)
	if analyzeErrs.HasErrors() {
		fmt.Print(formatErrors("Warning", analyzeErrs.Error()))
	}

	// Evaluate
	result, runErr := s.vm.Run(prog, s.env)
	elapsed := time.Since(start)

	if runErr != nil {
		return formatErrors("Runtime error", runErr.Error())
	}

	// Only print non-null results
	if result != nil && result.TypeID() != object.TypeNull {
		return fmt.Sprintf("= %s  \033[90m(%s)\033[0m", result.Inspect(), elapsed.Round(time.Microsecond))
	}
	return ""
}

func printHelp(out io.Writer) {
	fmt.Fprintln(out, `
Commands:
  :help  :h      Show this message
  :quit  :q      Exit the REPL
  :clear         Reset the environment (clear all variables)
  :env           List current bindings

Examples:
  let x = 42
  let name = "Alice"
  println(f"Hello {name}!")
  fn add(a: int, b: int) -> int { return a + b }
  add(3, 4)
`)
}

func formatErrors(label, msg string) string {
	return fmt.Sprintf("\033[31m%s:\033[0m %s", label, msg)
}
