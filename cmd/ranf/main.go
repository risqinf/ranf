// Command ranf is the entry point for the ranf programming language toolchain.
//
// Subcommands:
//
//	ranf run  <file.ranf>   Execute a source file
//	ranf check <file.ranf>  Lex + parse + analyze without running
//	ranf repl               Start the interactive REPL
//	ranf version            Print version information
//	ranf help               Print usage
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/risqinf/ranf/internal/analyzer"
	"github.com/risqinf/ranf/internal/builtin"
	"github.com/risqinf/ranf/internal/lexer"
	"github.com/risqinf/ranf/internal/object"
	"github.com/risqinf/ranf/internal/parser"
	"github.com/risqinf/ranf/internal/repl"
	"github.com/risqinf/ranf/internal/vm"
)

const (
	version = "0.1.0"
	banner  = `
   ██████╗  █████╗ ███╗   ██╗███████╗
   ██╔══██╗██╔══██╗████╗  ██║██╔════╝
   ██████╔╝███████║██╔██╗ ██║█████╗
   ██╔══██╗██╔══██║██║╚██╗██║██╔══╝
   ██║  ██║██║  ██║██║ ╚████║██║
   ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═══╝╚═╝  v` + version + `

   A Rust-inspired, safe, expressive language — built on Go.
`
)

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage()
		os.Exit(0)
	}

	switch args[0] {
	case "run", "r":
		if len(args) < 2 {
			fatalf("usage: ranf run <file.ranf>")
		}
		runFile(args[1], args[2:])

	case "check", "c":
		if len(args) < 2 {
			fatalf("usage: ranf check <file.ranf>")
		}
		checkFile(args[1])

	case "repl", "i":
		fmt.Print(banner)
		svc := repl.NewService()
		svc.Start(os.Stdin, os.Stdout)

	case "version", "v", "--version", "-v":
		fmt.Printf("ranf %s\n", version)
		fmt.Printf("Repository: https://github.com/risqinf/ranf\n")
		fmt.Printf("License: MIT\n")

	case "help", "--help", "-h":
		printUsage()

	default:
		// Treat unknown argument as a filename shortcut: ranf file.ranf
		if strings.HasSuffix(args[0], ".ranf") {
			runFile(args[0], args[1:])
		} else {
			fatalf("unknown command: %q\nRun 'ranf help' for usage.", args[0])
		}
	}
}

// ============================================================
// Commands
// ============================================================

func runFile(path string, _ []string) {
	src, err := os.ReadFile(path)
	if err != nil {
		fatalf("cannot read file %q: %v", path, err)
	}

	start := time.Now()

	// Stage 1: Lex
	lexSvc := lexer.NewService()
	tokens, lexErrs := lexSvc.Tokenize(path, string(src))
	if lexErrs.HasErrors() {
		printStageErrors("Lexer", lexErrs.Error())
		os.Exit(1)
	}

	// Stage 2: Parse
	parseSvc := parser.NewService()
	prog, parseErrs := parseSvc.Parse(tokens)
	if parseErrs.HasErrors() {
		printStageErrors("Parser", parseErrs.Error())
		os.Exit(1)
	}

	// Stage 3: Analyze
	analyzeSvc := analyzer.NewService()
	analyzeErrs := analyzeSvc.Analyze(prog)
	if analyzeErrs.HasErrors() {
		printStageErrors("Analyzer", analyzeErrs.Error())
		os.Exit(1)
	}

	// Stage 4: Execute
	b := builtin.NewRegistry()
	vmSvc := vm.NewService(b)
	env := object.NewEnvironment()

	_, runErr := vmSvc.Run(prog, env)
	if runErr != nil {
		printStageErrors("Runtime", runErr.Error())
		os.Exit(1)
	}

	_ = time.Since(start) // timing available for --verbose flag (future)
}

func checkFile(path string) {
	src, err := os.ReadFile(path)
	if err != nil {
		fatalf("cannot read file %q: %v", path, err)
	}

	lexSvc := lexer.NewService()
	tokens, lexErrs := lexSvc.Tokenize(path, string(src))
	if lexErrs.HasErrors() {
		printStageErrors("Lexer", lexErrs.Error())
		os.Exit(1)
	}

	parseSvc := parser.NewService()
	prog, parseErrs := parseSvc.Parse(tokens)
	if parseErrs.HasErrors() {
		printStageErrors("Parser", parseErrs.Error())
		os.Exit(1)
	}

	analyzeSvc := analyzer.NewService()
	analyzeErrs := analyzeSvc.Analyze(prog)
	if analyzeErrs.HasErrors() {
		printStageErrors("Analyzer", analyzeErrs.Error())
		os.Exit(1)
	}

	fmt.Printf("✓ %s — no errors found\n", path)
}

// ============================================================
// Helpers
// ============================================================

func printStageErrors(stage, msg string) {
	fmt.Fprintf(os.Stderr, "\033[31m[%s error]\033[0m %s\n", stage, msg)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ranf: "+format+"\n", args...)
	os.Exit(1)
}

func printUsage() {
	fmt.Print(banner)
	fmt.Print(`Usage:
  ranf run   <file.ranf>   Execute a ranf source file
  ranf check <file.ranf>   Check syntax and semantics without running
  ranf repl                Start the interactive REPL
  ranf version             Print version information
  ranf help                Print this message

Shortcuts:
  ranf <file.ranf>         Same as 'ranf run <file.ranf>'

Examples:
  ranf run examples/hello.ranf
  ranf check myprogram.ranf
  ranf repl

`)
}
