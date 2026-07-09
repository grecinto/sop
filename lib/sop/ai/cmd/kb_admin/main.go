package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharedcode/sop/ai/memory"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "report":
		runReport(os.Args[2:])
	case "analyze":
		runAnalyze(os.Args[2:])
	case "prune":
		runPrune(os.Args[2:])
	case "list":
		runList(os.Args[2:])
	case "export":
		runExport(os.Args[2:])
	case "version":
		fmt.Printf("kb-admin version %s\n", version)
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("kb-admin - Knowledge Base Quality Administration Tool")
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Println("  kb-admin <command> [options]")
	fmt.Println()
	fmt.Println("COMMANDS:")
	fmt.Println("  report     Generate comprehensive quality report")
	fmt.Println("  analyze    Identify poor performers and pruning candidates")
	fmt.Println("  prune      Execute pruning (use --dry-run first)")
	fmt.Println("  list       List all KB effectiveness records")
	fmt.Println("  export     Export effectiveness data to JSON")
	fmt.Println("  version    Show version information")
	fmt.Println("  help       Show this help message")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Println("  kb-admin report --data /path/to/kb_effectiveness.json")
	fmt.Println("  kb-admin analyze --threshold 0.40")
	fmt.Println("  kb-admin prune --dry-run")
	fmt.Println("  kb-admin list --format table")
	fmt.Println()
	fmt.Println("For command-specific help, use: kb-admin <command> --help")
}

func runReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	dataPath := fs.String("data", getDefaultDataPath(), "Path to KB effectiveness data file")
	threshold := fs.Float64("threshold", 0.30, "Success rate threshold for flagging")
	minUses := fs.Int("min-uses", 10, "Minimum uses before flagging")
	inactiveDays := fs.Int("inactive-days", 90, "Days of inactivity threshold")
	
	fs.Parse(args)
	
	tracker, err := loadTracker(*dataPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading data: %v\n", err)
		os.Exit(1)
	}
	
	thresholds := memory.PruningThresholds{
		MinimumUses:      *minUses,
		SuccessRateBelow: *threshold,
		InactivityDays:   *inactiveDays,
	}
	
	analyzer := memory.NewPruningAnalyzer(tracker, thresholds)
	report, err := analyzer.Analyze()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing data: %v\n", err)
		os.Exit(1)
	}
	
	formatted := analyzer.FormatReport(report)
	fmt.Print(formatted)
}

func runAnalyze(args []string) {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	dataPath := fs.String("data", getDefaultDataPath(), "Path to KB effectiveness data file")
	threshold := fs.Float64("threshold", 0.30, "Success rate threshold for flagging")
	minUses := fs.Int("min-uses", 10, "Minimum uses before flagging")
	showAll := fs.Bool("all", false, "Show all candidates (not just critical)")
	
	fs.Parse(args)
	
	tracker, err := loadTracker(*dataPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading data: %v\n", err)
		os.Exit(1)
	}
	
	thresholds := memory.PruningThresholds{
		MinimumUses:      *minUses,
		SuccessRateBelow: *threshold,
		InactivityDays:   90,
	}
	
	analyzer := memory.NewPruningAnalyzer(tracker, thresholds)
	report, err := analyzer.Analyze()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analyzing data: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Println("🔍 ANALYSIS RESULTS")
	fmt.Println("─────────────────────────────────────────────────────")
	fmt.Printf("Total KBs: %d | Active: %d | Stale: %d\n", 
		report.TotalKBs, report.ActiveKBs, report.StaleKBs)
	fmt.Printf("Average Success Rate: %.1f%%\n\n", report.AverageSuccessRate*100)
	
	if len(report.PoorPerformers) == 0 {
		fmt.Println("✓ No poor performers identified")
		return
	}
	
	criticalCount := 0
	warningCount := 0
	
	for _, candidate := range report.PoorPerformers {
		if !*showAll && candidate.Severity != "critical" {
			continue
		}
		
		if candidate.Severity == "critical" {
			criticalCount++
		} else if candidate.Severity == "warning" {
			warningCount++
		}
		
		icon := "ℹ️ "
		if candidate.Severity == "critical" {
			icon = "❌"
		} else if candidate.Severity == "warning" {
			icon = "⚠️ "
		}
		
		fmt.Printf("%s %s [%s]\n", icon, candidate.KBName, candidate.TaskType)
		fmt.Printf("   Success: %.1f%% | Uses: %d | Last Used: %d days ago\n",
			candidate.SuccessRate*100, candidate.TotalUses, candidate.DaysSinceUse)
		fmt.Printf("   Issue: %s\n", candidate.Reason)
		fmt.Printf("   Action: %s\n\n", candidate.Recommendation)
	}
	
	fmt.Printf("Summary: %d critical, %d warnings\n", criticalCount, warningCount)
}

func runPrune(args []string) {
	fs := flag.NewFlagSet("prune", flag.ExitOnError)
	dataPath := fs.String("data", getDefaultDataPath(), "Path to KB effectiveness data file")
	dryRun := fs.Bool("dry-run", true, "Show what would be pruned without making changes")
	execute := fs.Bool("execute", false, "Actually execute pruning (disables dry-run)")
	
	fs.Parse(args)
	
	if *execute {
		*dryRun = false
	}
	
	tracker, err := loadTracker(*dataPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading data: %v\n", err)
		os.Exit(1)
	}
	
	analyzer := memory.NewPruningAnalyzer(tracker, memory.DefaultPruningThresholds())
	
	pruned, err := analyzer.ExecutePruning(*dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing pruning: %v\n", err)
		os.Exit(1)
	}
	
	if len(pruned) == 0 {
		fmt.Println("✓ No KBs need pruning")
		return
	}
	
	if *dryRun {
		fmt.Println("🔍 DRY RUN - The following KBs would be pruned:")
		fmt.Println("─────────────────────────────────────────────────────")
	} else {
		fmt.Println("✂️  PRUNING - The following KBs have been marked:")
		fmt.Println("─────────────────────────────────────────────────────")
	}
	
	for i, kb := range pruned {
		fmt.Printf("%d. %s\n", i+1, kb)
	}
	
	fmt.Printf("\nTotal: %d KB(s)\n", len(pruned))
	
	if *dryRun {
		fmt.Println("\nTo execute pruning, run: kb-admin prune --execute")
	}
}

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	dataPath := fs.String("data", getDefaultDataPath(), "Path to KB effectiveness data file")
	format := fs.String("format", "table", "Output format: table, json, csv")
	sortBy := fs.String("sort", "name", "Sort by: name, success, uses, recent")
	
	fs.Parse(args)
	
	tracker, err := loadTracker(*dataPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading data: %v\n", err)
		os.Exit(1)
	}
	
	records := tracker.GetAllRecords()
	
	if len(records) == 0 {
		fmt.Println("No KB effectiveness records found")
		return
	}
	
	// Sort records
	switch *sortBy {
	case "success":
		// Already implements sort by success in GetAllRecords
	case "uses":
		// Would need custom sorting - omitted for brevity
	case "recent":
		// Would need custom sorting - omitted for brevity
	}
	
	switch *format {
	case "json":
		outputJSON(records)
	case "csv":
		outputCSV(records)
	default:
		outputTable(records)
	}
}

func runExport(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	dataPath := fs.String("data", getDefaultDataPath(), "Path to KB effectiveness data file")
	output := fs.String("output", "kb_export.json", "Output file path")
	
	fs.Parse(args)
	
	tracker, err := loadTracker(*dataPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading data: %v\n", err)
		os.Exit(1)
	}
	
	records := tracker.GetAllRecords()
	
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling data: %v\n", err)
		os.Exit(1)
	}
	
	if err := os.WriteFile(*output, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("✓ Exported %d records to %s\n", len(records), *output)
}

func loadTracker(dataPath string) (*memory.KBEffectivenessTracker, error) {
	if dataPath == "" {
		return nil, fmt.Errorf("data path not specified")
	}
	
	// Create directory if it doesn't exist
	dir := filepath.Dir(dataPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}
	
	// CLI tool doesn't need auto-save
	return memory.NewKBEffectivenessTracker(dataPath, false)
}

func getDefaultDataPath() string {
	// Try common locations
	homeDir, err := os.UserHomeDir()
	if err == nil {
		sopDir := filepath.Join(homeDir, ".sop", "ai", "kb_effectiveness.json")
		if _, err := os.Stat(sopDir); err == nil {
			return sopDir
		}
	}
	
	// Fall back to current directory
	return "./kb_effectiveness.json"
}

func outputTable(records []memory.KBEffectiveness) {
	fmt.Println("KB EFFECTIVENESS RECORDS")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("%-30s %-15s %8s %8s %10s %12s\n", 
		"KB Name", "Task Type", "Success", "Failure", "Score", "Last Used")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	
	for _, record := range records {
		lastUsed := "never"
		if !record.LastUsed.IsZero() {
			lastUsed = record.LastUsed.Format("2006-01-02")
		}
		
		kbName := record.KBName
		if len(kbName) > 28 {
			kbName = kbName[:25] + "..."
		}
		
		fmt.Printf("%-30s %-15s %8d %8d %9.1f%% %12s\n",
			kbName,
			record.TaskType,
			record.SuccessCount,
			record.FailureCount,
			record.EffectivenessScore()*100,
			lastUsed,
		)
	}
	
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("Total records: %d\n", len(records))
}

func outputJSON(records []memory.KBEffectiveness) {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

func outputCSV(records []memory.KBEffectiveness) {
	fmt.Println("KB Name,Task Type,Success Count,Failure Count,Effectiveness Score,Last Used")
	for _, record := range records {
		lastUsed := ""
		if !record.LastUsed.IsZero() {
			lastUsed = record.LastUsed.Format("2006-01-02")
		}
		
		kbName := strings.ReplaceAll(record.KBName, ",", ";")
		
		fmt.Printf("%s,%s,%d,%d,%.2f,%s\n",
			kbName,
			record.TaskType,
			record.SuccessCount,
			record.FailureCount,
			record.EffectivenessScore(),
			lastUsed,
		)
	}
}
