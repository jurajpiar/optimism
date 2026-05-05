package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Common paths where tools might be installed (not always in PATH when running from debugger)
var commonToolPaths = []string{
	"~/.config/.foundry/bin", // Foundry (forge, cast, anvil)
	"~/.cargo/bin",           // Rust/Cargo tools (just)
	"/opt/homebrew/bin",      // Homebrew on Apple Silicon
	"/usr/local/bin",         // Homebrew on Intel Mac / Linux
	"~/go/bin",               // Go binaries
	"/usr/local/go/bin",      // Go installation
}

func init() {
	// Extend PATH with common tool locations
	extendPath()
}

// extendPath adds common tool directories to PATH
func extendPath() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}

	currentPath := os.Getenv("PATH")
	var additionalPaths []string

	for _, p := range commonToolPaths {
		// Expand ~ to home directory
		expanded := p
		if strings.HasPrefix(p, "~/") {
			expanded = filepath.Join(homeDir, p[2:])
		}

		// Only add if directory exists and not already in PATH
		if info, err := os.Stat(expanded); err == nil && info.IsDir() {
			if !strings.Contains(currentPath, expanded) {
				additionalPaths = append(additionalPaths, expanded)
			}
		}
	}

	if len(additionalPaths) > 0 {
		newPath := strings.Join(additionalPaths, string(os.PathListSeparator)) + string(os.PathListSeparator) + currentPath
		os.Setenv("PATH", newPath)
	}
}

// ToolInfo holds information about a required tool
type ToolInfo struct {
	Name        string
	Command     string
	VersionArgs []string
	InstallURL  string
}

var requiredTools = []ToolInfo{
	{
		Name:        "foundry (forge)",
		Command:     "forge",
		VersionArgs: []string{"--version"},
		InstallURL:  "https://book.getfoundry.sh/getting-started/installation",
	},
	{
		Name:        "go",
		Command:     "go",
		VersionArgs: []string{"version"},
		InstallURL:  "https://go.dev/doc/install",
	},
	{
		Name:        "cast",
		Command:     "cast",
		VersionArgs: []string{"--version"},
		InstallURL:  "https://book.getfoundry.sh/getting-started/installation",
	},
	{
		Name:        "just",
		Command:     "just",
		VersionArgs: []string{"--version"},
		InstallURL:  "https://just.systems/",
	},
}

// findTool looks for a tool in PATH and common locations
func findTool(command string) (string, error) {
	// First try standard PATH lookup
	if path, err := exec.LookPath(command); err == nil {
		return path, nil
	}

	// Try common locations explicitly
	homeDir, _ := os.UserHomeDir()
	for _, p := range commonToolPaths {
		expanded := p
		if strings.HasPrefix(p, "~/") && homeDir != "" {
			expanded = filepath.Join(homeDir, p[2:])
		}

		toolPath := filepath.Join(expanded, command)
		if info, err := os.Stat(toolPath); err == nil && !info.IsDir() {
			return toolPath, nil
		}
	}

	return "", fmt.Errorf("command not found: %s", command)
}

// checkRequiredTools verifies all required tools are installed
func checkRequiredTools(verbose bool) error {
	for _, tool := range requiredTools {
		path, err := findTool(tool.Command)
		if err != nil {
			// Show current PATH for debugging
			printError("Current PATH: %s", os.Getenv("PATH"))
			return fmt.Errorf("%s is not installed or not in PATH. Please install: %s", tool.Name, tool.InstallURL)
		}

		// Get version info
		version := "version unknown"
		if len(tool.VersionArgs) > 0 {
			cmd := exec.Command(path, tool.VersionArgs...)
			output, err := cmd.Output()
			if err == nil {
				// Get first line of output
				lines := strings.Split(string(output), "\n")
				if len(lines) > 0 {
					version = strings.TrimSpace(lines[0])
				}
			}
		}

		if verbose {
			printStatus("✓ %s found at %s: %s", tool.Name, path, version)
		} else {
			printStatus("✓ %s found: %s", tool.Name, version)
		}
	}

	return nil
}
