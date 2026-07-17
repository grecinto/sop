package desktop

import (
	"strings"
	"testing"
)

func TestFormatCommandOutputForDisplay_LsSimpleList(t *testing.T) {
	command := "ls"
	output := "file1.txt\nfile2.txt\nfile3.txt\nfile4.txt\nfile5.txt\nfile6.txt"

	result := formatCommandOutputForDisplay(command, output)

	// Should be formatted into two columns
	if !strings.Contains(result, "  ") {
		t.Errorf("Expected two-column format with spacing, got:\n%s", result)
	}

	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 3 {
		t.Errorf("Expected 3 lines (6 items in 2 columns), got %d lines:\n%s", len(lines), result)
	}

	// First line should have file1.txt and file2.txt
	if !strings.Contains(lines[0], "file1.txt") || !strings.Contains(lines[0], "file2.txt") {
		t.Errorf("First line should contain file1.txt and file2.txt, got: %s", lines[0])
	}
}

func TestFormatCommandOutputForDisplay_LsOddNumber(t *testing.T) {
	command := "ls"
	output := "file1.txt\nfile2.txt\nfile3.txt\nfile4.txt\nfile5.txt"

	result := formatCommandOutputForDisplay(command, output)

	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 3 {
		t.Errorf("Expected 3 lines (5 items: 2+2+1), got %d lines:\n%s", len(lines), result)
	}

	// Last line should only have file5.txt
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if lastLine != "file5.txt" {
		t.Errorf("Last line should be 'file5.txt', got: %s", lastLine)
	}
}

func TestFormatCommandOutputForDisplay_TooFewItems(t *testing.T) {
	command := "ls"
	output := "file1.txt\nfile2.txt\nfile3.txt"

	result := formatCommandOutputForDisplay(command, output)

	// Should not be formatted (less than 4 items)
	if result != output {
		t.Errorf("Expected unformatted output for 3 items, got:\n%s", result)
	}
}

func TestFormatCommandOutputForDisplay_AlreadyFormatted(t *testing.T) {
	command := "ls -la"
	output := "drwxr-xr-x  5 user  staff   160 Jan  1 12:00 dir1\n-rw-r--r--  1 user  staff  1024 Jan  1 12:00 file1.txt"

	result := formatCommandOutputForDisplay(command, output)

	// Should not reformat already formatted output
	if result != output {
		t.Errorf("Expected unchanged output for already formatted ls -la, got:\n%s", result)
	}
}

func TestFormatCommandOutputForDisplay_NonLsCommand(t *testing.T) {
	command := "pwd"
	output := "/home/user/project"

	result := formatCommandOutputForDisplay(command, output)

	// Should not format non-ls commands
	if result != output {
		t.Errorf("Expected unchanged output for pwd command, got: %s", result)
	}
}

func TestFormatCommandOutputForDisplay_LongFilenames(t *testing.T) {
	command := "ls"
	output := "very_long_filename_that_is_more_than_fifty_characters_long.txt\nshort.txt\nanother.txt\nfile.txt"

	result := formatCommandOutputForDisplay(command, output)

	// Now with truncation, long filenames should be truncated and formatted
	if !strings.Contains(result, "..") {
		t.Errorf("Expected long filename to be truncated with '..',got:\n%s", result)
	}

	// Should still format into two columns
	if !strings.Contains(result, "  ") {
		t.Errorf("Expected two-column format, got:\n%s", result)
	}
}

func TestFormatCommandOutputForDisplay_DirCommand(t *testing.T) {
	command := "dir"
	output := "file1.txt\nfile2.txt\nfile3.txt\nfile4.txt"

	result := formatCommandOutputForDisplay(command, output)

	// Should work for 'dir' command too
	if !strings.Contains(result, "  ") {
		t.Errorf("Expected two-column format for 'dir' command, got:\n%s", result)
	}
}

func TestFormatCommandOutputForDisplay_CaseInsensitive(t *testing.T) {
	command := "LS"
	output := "file1.txt\nfile2.txt\nfile3.txt\nfile4.txt"

	result := formatCommandOutputForDisplay(command, output)

	// Should work case-insensitively
	if !strings.Contains(result, "  ") {
		t.Errorf("Expected two-column format for uppercase 'LS' command, got:\n%s", result)
	}
}

func TestFormatCommandOutputForDisplay_LongFilenamesTruncated(t *testing.T) {
	command := "ls"
	// Create filenames longer than 45 characters (max column width)
	longName1 := "very_long_filename_that_exceeds_the_maximum_column_width_limit.txt"
	longName2 := "another_extremely_long_filename_for_testing_truncation.go"
	shortName1 := "short.txt"
	shortName2 := "file.go"

	output := longName1 + "\n" + shortName1 + "\n" + longName2 + "\n" + shortName2

	result := formatCommandOutputForDisplay(command, output)

	// Long filenames should be truncated with ".." suffix
	if !strings.Contains(result, "..") {
		t.Errorf("Expected long filenames to be truncated with '..', got:\n%s", result)
	}

	// Should contain 43 chars + ".." = 45 chars total from the long filename
	truncatedName1 := longName1[:43] + ".."
	if !strings.Contains(result, truncatedName1) {
		t.Errorf("Expected truncated name '%s' in output, got:\n%s", truncatedName1, result)
	}

	// Short filenames should not be truncated
	if !strings.Contains(result, shortName1) {
		t.Errorf("Expected short filename '%s' to remain intact, got:\n%s", shortName1, result)
	}
}

func TestTruncateFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		maxLen   int
		want     string
	}{
		{
			name:     "short filename no truncation",
			filename: "file.txt",
			maxLen:   45,
			want:     "file.txt",
		},
		{
			name:     "exact max length no truncation",
			filename: "this_is_exactly_forty_five_characters_x.txt",
			maxLen:   45,
			want:     "this_is_exactly_forty_five_characters_x.txt",
		},
		{
			name:     "one char over max truncated",
			filename: "this_filename_is_one_character_over_the_limit_x",
			maxLen:   45,
			want:     "this_filename_is_one_character_over_the_lim..",
		},
		{
			name:     "very long filename truncated",
			filename: "extremely_long_filename_that_goes_way_beyond_any_reasonable_limit_for_display.txt",
			maxLen:   45,
			want:     "extremely_long_filename_that_goes_way_beyon..",
		},
		{
			name:     "maxLen of 2 returns ..",
			filename: "file.txt",
			maxLen:   2,
			want:     "..",
		},
		{
			name:     "maxLen of 5 shows some chars",
			filename: "verylongfilename.txt",
			maxLen:   10,
			want:     "verylong..",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateFilename(tt.filename, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateFilename() = %q, want %q", got, tt.want)
			}
			if len(got) > tt.maxLen {
				t.Errorf("truncateFilename() length = %d, should be <= %d", len(got), tt.maxLen)
			}
		})
	}
}

func TestNormalizeLsCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "bare ls command",
			input:    "ls",
			expected: "ls -C",
		},
		{
			name:     "ls with path",
			input:    "ls /tmp",
			expected: "ls -C /tmp",
		},
		{
			name:     "ls with multiple args",
			input:    "ls -a /tmp",
			expected: "ls -C -a /tmp",
		},
		{
			name:     "ls already has -C",
			input:    "ls -C /tmp",
			expected: "ls -C /tmp",
		},
		{
			name:     "ls already has -1",
			input:    "ls -1 /tmp",
			expected: "ls -1 /tmp",
		},
		{
			name:     "ls -l should not add -C",
			input:    "ls -l /tmp",
			expected: "ls -l /tmp",
		},
		{
			name:     "ls -x should not add -C",
			input:    "ls -x /tmp",
			expected: "ls -x /tmp",
		},
		{
			name:     "dir command",
			input:    "dir /tmp",
			expected: "dir -C /tmp",
		},
		{
			name:     "non-ls command unchanged",
			input:    "pwd",
			expected: "pwd",
		},
		{
			name:     "uppercase LS",
			input:    "LS /tmp",
			expected: "LS -C /tmp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeLsCommand(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeLsCommand(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
