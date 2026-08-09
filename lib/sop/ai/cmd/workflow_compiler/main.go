package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop/ai/agent/codegen"
)

const version = "1.0.0"

func main() {
	// Define flags
	var (
		inputFile   = flag.String("input", "", "Path to workflow JSON file")
		outputFile  = flag.String("output", "", "Output path for compiled binary (default: workflow name)")
		outputGo    = flag.String("output-go", "", "Output path for generated Go source (optional)")
		outputCs    = flag.String("output-cs", "", "Output path for generated C# source (optional)")
		outputPy    = flag.String("output-py", "", "Output path for generated Python source (optional)")
		targetLang  = flag.String("target", "go", "Target language for generated source: go, csharp, python")
		scriptMode  = flag.Bool("script", false, "Compile a script instead of a workflow")
		showVersion = flag.Bool("version", false, "Show version information")
		verbose     = flag.Bool("v", false, "Verbose output")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "sop-compile-workflow - Compile workflows and scripts to native binaries\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Compile a workflow to binary:\n")
		fmt.Fprintf(os.Stderr, "  %s -input workflow.json -output my-workflow\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Generate Go source only:\n")
		fmt.Fprintf(os.Stderr, "  %s -input workflow.json -output-go workflow.go\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Generate C# source:\n")
		fmt.Fprintf(os.Stderr, "  %s -input workflow.json -target csharp -output-cs workflow.cs\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Compile a script:\n")
		fmt.Fprintf(os.Stderr, "  %s -script -input script.json -target python -output-py script.py\n\n", os.Args[0])
	}

	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Printf("sop-compile-workflow version %s\n", version)
		os.Exit(0)
	}

	// Validate input
	if *inputFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -input is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Check if input file exists
	if _, err := os.Stat(*inputFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: Input file not found: %s\n", *inputFile)
		os.Exit(1)
	}

	if *verbose {
		fmt.Printf("Compiling: %s\n", *inputFile)
	}

	// Compile based on mode
	if *scriptMode {
		if err := compileScript(*inputFile, *targetLang, *outputGo, *outputCs, *outputPy, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := compileWorkflow(*inputFile, *outputFile, *targetLang, *outputGo, *outputCs, *outputPy, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

// compileScript compiles a JSON script to source in the requested target language
func compileScript(inputFile, targetLang, outputGo, outputCs, outputPy string, verbose bool) error {
	target := codegen.TargetLanguage(strings.ToLower(strings.TrimSpace(targetLang)))
	var (
		code string
		err  error
	)

	switch target {
	case codegen.TargetLanguageGo:
		code, err = codegen.CompileScriptToGo(inputFile)
	case codegen.TargetLanguageCSharp:
		code, err = codegen.CompileScriptToCSharp(inputFile)
	case codegen.TargetLanguagePython:
		code, err = codegen.CompileScriptToPython(inputFile)
	case codegen.TargetLanguageTypeScript, codegen.TargetLanguageJavaScript, codegen.TargetLanguageReact, codegen.TargetLanguageAngular, codegen.TargetLanguageHTMLCSS:
		code, err = codegen.CompileScript(inputFile, target)
	default:
		return fmt.Errorf("unsupported target language: %s", targetLang)
	}
	if err != nil {
		return fmt.Errorf("compile script: %w", err)
	}

	outputPath := outputGo
	switch target {
	case codegen.TargetLanguageCSharp:
		outputPath = outputCs
	case codegen.TargetLanguagePython:
		outputPath = outputPy
	}

	if outputPath == "" {
		base := strings.TrimSuffix(filepath.Base(inputFile), filepath.Ext(inputFile))
		switch target {
		case codegen.TargetLanguageCSharp:
			outputPath = base + "_compiled.cs"
		case codegen.TargetLanguagePython:
			outputPath = base + "_compiled.py"
		case codegen.TargetLanguageTypeScript, codegen.TargetLanguageReact:
			outputPath = base + "_compiled.tsx"
		case codegen.TargetLanguageJavaScript:
			outputPath = base + "_compiled.js"
		case codegen.TargetLanguageAngular:
			outputPath = base + "_compiled.ts"
		case codegen.TargetLanguageHTMLCSS:
			outputPath = base + "_compiled.html"
		default:
			outputPath = base + "_compiled.go"
		}
	}

	if err := os.WriteFile(outputPath, []byte(code), 0644); err != nil {
		return fmt.Errorf("write source: %w", err)
	}

	fmt.Printf("✓ Generated %s source: %s\n", target, outputPath)
	fmt.Printf("  Size: %d bytes\n", len(code))

	if verbose {
		fmt.Printf("\nTo use in your code:\n")
		if target == codegen.TargetLanguageGo {
			fmt.Printf("  import \"./compiled\"\n")
			fmt.Printf("  result, err := compiled.ScriptName(ctx, input)\n")
		}
	}

	return nil
}

// compileWorkflow compiles a JSON workflow to source and optionally to binary
func compileWorkflow(inputFile, outputBinary, targetLang, outputGo, outputCs, outputPy string, verbose bool) error {
	target := codegen.TargetLanguage(strings.ToLower(strings.TrimSpace(targetLang)))
	var (
		code string
		err  error
	)

	switch target {
	case codegen.TargetLanguageGo:
		code, err = codegen.CompileWorkflowToGo(inputFile)
	case codegen.TargetLanguageCSharp:
		code, err = codegen.CompileWorkflowToCSharp(inputFile)
	case codegen.TargetLanguagePython:
		code, err = codegen.CompileWorkflowToPython(inputFile)
	case codegen.TargetLanguageTypeScript, codegen.TargetLanguageJavaScript, codegen.TargetLanguageReact, codegen.TargetLanguageAngular, codegen.TargetLanguageHTMLCSS:
		code, err = codegen.CompileWorkflow(inputFile, target)
	default:
		return fmt.Errorf("unsupported target language: %s", targetLang)
	}
	if err != nil {
		return fmt.Errorf("compile workflow: %w", err)
	}

	outputPath := outputGo
	switch target {
	case codegen.TargetLanguageCSharp:
		outputPath = outputCs
	case codegen.TargetLanguagePython:
		outputPath = outputPy
	}

	if outputPath == "" {
		if outputBinary != "" {
			outputPath = filepath.Join(os.TempDir(), "workflow_main.go")
		} else {
			base := strings.TrimSuffix(filepath.Base(inputFile), filepath.Ext(inputFile))
			switch target {
			case codegen.TargetLanguageCSharp:
				outputPath = base + "_compiled.cs"
			case codegen.TargetLanguagePython:
				outputPath = base + "_compiled.py"
			case codegen.TargetLanguageTypeScript, codegen.TargetLanguageReact:
				outputPath = base + "_compiled.tsx"
			case codegen.TargetLanguageJavaScript:
				outputPath = base + "_compiled.js"
			case codegen.TargetLanguageAngular:
				outputPath = base + "_compiled.ts"
			case codegen.TargetLanguageHTMLCSS:
				outputPath = base + "_compiled.html"
			default:
				outputPath = base + "_compiled.go"
			}
		}
	}

	if err := os.WriteFile(outputPath, []byte(code), 0644); err != nil {
		return fmt.Errorf("write source: %w", err)
	}

	if outputGo != "" || outputCs != "" || outputPy != "" || outputBinary == "" {
		fmt.Printf("✓ Generated %s source: %s\n", target, outputPath)
		fmt.Printf("  Size: %d bytes\n", len(code))
	}

	// Compile to binary if requested
	if outputBinary != "" {
		if verbose {
			fmt.Printf("\nCompiling binary...\n")
		}

		// Determine binary path
		binaryPath := outputBinary
		if !strings.Contains(binaryPath, ".") && !strings.Contains(binaryPath, "/") {
			// Add platform-specific extension if needed
			// (go build handles this automatically)
		}

		// Run go build only for Go output
		if target != codegen.TargetLanguageGo {
			fmt.Printf("Skipping binary build for target %s; source emitted at %s\n", target, outputPath)
			return nil
		}

		cmd := exec.Command("go", "build", "-o", binaryPath, outputPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go build failed: %w", err)
		}

		// Get binary size
		stat, err := os.Stat(binaryPath)
		if err != nil {
			return fmt.Errorf("stat binary: %w", err)
		}

		fmt.Printf("✓ Compiled binary: %s\n", binaryPath)
		fmt.Printf("  Size: %.2f MB\n", float64(stat.Size())/(1024*1024))

		if verbose {
			fmt.Printf("\nTo run:\n")
			fmt.Printf("  ./%s \"your query here\"\n", filepath.Base(binaryPath))
		}

		// Clean up temp source if it was temporary
		if outputGo == "" && outputCs == "" && outputPy == "" {
			os.Remove(outputPath)
		}
	} else {
		if verbose {
			fmt.Printf("\nTo compile to binary:\n")
			fmt.Printf("  go build -o my-workflow %s\n", outputPath)
		}
	}

	return nil
}
