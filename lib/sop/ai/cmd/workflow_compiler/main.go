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
		fmt.Fprintf(os.Stderr, "  # Compile a script:\n")
		fmt.Fprintf(os.Stderr, "  %s -script -input script.json -output-go script.go\n\n", os.Args[0])
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
		if err := compileScript(*inputFile, *outputGo, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := compileWorkflow(*inputFile, *outputFile, *outputGo, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

// compileScript compiles a JSON script to Go source
func compileScript(inputFile, outputGo string, verbose bool) error {
	// Generate Go source
	goCode, err := codegen.CompileScriptToGo(inputFile)
	if err != nil {
		return fmt.Errorf("compile script: %w", err)
	}

	// Determine output path
	if outputGo == "" {
		// Default: same name as input with .go extension
		base := strings.TrimSuffix(filepath.Base(inputFile), filepath.Ext(inputFile))
		outputGo = base + "_compiled.go"
	}

	// Write Go source
	if err := os.WriteFile(outputGo, []byte(goCode), 0644); err != nil {
		return fmt.Errorf("write Go source: %w", err)
	}

	fmt.Printf("✓ Generated Go source: %s\n", outputGo)
	fmt.Printf("  Size: %d bytes\n", len(goCode))

	if verbose {
		fmt.Printf("\nTo use in your code:\n")
		fmt.Printf("  import \"./compiled\"\n")
		fmt.Printf("  result, err := compiled.ScriptName(ctx, input)\n")
	}

	return nil
}

// compileWorkflow compiles a JSON workflow to Go source and optionally to binary
func compileWorkflow(inputFile, outputBinary, outputGo string, verbose bool) error {
	// Generate Go source
	goCode, err := codegen.CompileWorkflowToGo(inputFile)
	if err != nil {
		return fmt.Errorf("compile workflow: %w", err)
	}

	// Determine Go source path
	goSourcePath := outputGo
	if goSourcePath == "" {
		// Use temp file if only building binary
		if outputBinary != "" {
			goSourcePath = filepath.Join(os.TempDir(), "workflow_main.go")
		} else {
			// Default: same name as input with .go extension
			base := strings.TrimSuffix(filepath.Base(inputFile), filepath.Ext(inputFile))
			goSourcePath = base + "_compiled.go"
		}
	}

	// Write Go source
	if err := os.WriteFile(goSourcePath, []byte(goCode), 0644); err != nil {
		return fmt.Errorf("write Go source: %w", err)
	}

	if outputGo != "" {
		fmt.Printf("✓ Generated Go source: %s\n", goSourcePath)
		fmt.Printf("  Size: %d bytes\n", len(goCode))
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

		// Run go build
		cmd := exec.Command("go", "build", "-o", binaryPath, goSourcePath)
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

		// Clean up temp Go source if it was temporary
		if outputGo == "" {
			os.Remove(goSourcePath)
		}
	} else {
		if verbose {
			fmt.Printf("\nTo compile to binary:\n")
			fmt.Printf("  go build -o my-workflow %s\n", goSourcePath)
		}
	}

	return nil
}
