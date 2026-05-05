package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[0;31m"
	colorGreen  = "\033[0;32m"
	colorYellow = "\033[1;33m"
	colorCyan   = "\033[1;36m"
)

// printStatus prints an info message in green
func printStatus(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s[INFO]%s %s\n", colorGreen, colorReset, msg)
}

// printError prints an error message in red
func printError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "%s[ERROR]%s %s\n", colorRed, colorReset, msg)
}

// printStep prints a step message in yellow
func printStep(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s[STEP]%s %s\n", colorYellow, colorReset, msg)
}

// printProgress prints a progress bar
func printProgress(current, total int) {
	percentage := current * 100 / total
	filled := current * 50 / total
	empty := 50 - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	progressMsg := fmt.Sprintf("[%d/%d] %s %d%%", current, total, bar, percentage)
	fmt.Printf("\n%s%s%s\n", colorYellow, progressMsg, colorReset)
}

// printSkipped prints a skip message in cyan
func printSkipped(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s[SKIP]%s %s\n", colorCyan, colorReset, msg)
}

// StepLogger manages logging for a specific deployment step
type StepLogger struct {
	stepFile   *os.File
	masterFile *os.File
	combined   io.Writer
	stepName   string
}

// NewStepLogger creates a new step logger
func NewStepLogger(stepFile, masterFile *os.File, stepName string) *StepLogger {
	return &StepLogger{
		stepFile:   stepFile,
		masterFile: masterFile,
		combined:   io.MultiWriter(stepFile, masterFile),
		stepName:   stepName,
	}
}

// Write implements io.Writer for the combined output
func (l *StepLogger) Write(p []byte) (n int, err error) {
	return l.combined.Write(p)
}

// Printf writes formatted output to both log files
func (l *StepLogger) Printf(format string, args ...interface{}) {
	fmt.Fprintf(l.combined, format, args...)
}

// Println writes a line to both log files
func (l *StepLogger) Println(msg string) {
	fmt.Fprintln(l.combined, msg)
}

// Close closes the step log file (master log stays open)
func (l *StepLogger) Close() {
	if l.stepFile != nil {
		l.stepFile.Close()
	}
}
