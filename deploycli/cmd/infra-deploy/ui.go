package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

func interactiveOutput() bool {
	info, e := os.Stdout.Stat()
	return e == nil && info.Mode()&os.ModeCharDevice != 0
}
func color(text, code string) string {
	if !interactiveOutput() || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return text
	}
	if runtime.GOOS == "windows" && os.Getenv("WT_SESSION") == "" && os.Getenv("ANSICON") == "" && os.Getenv("TERM") == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}
func heading(title string)       { fmt.Printf("\n  %s\n", color(title, "1;36")) }
func detail(label, value string) { fmt.Printf("  %-14s %s\n", label, color(value, "1")) }
func note(text string)           { fmt.Printf("  %s\n", color(text, "2")) }
func banner(spec manifest, action, environment string) {
	fmt.Printf("\n  %s\n", color(spec.Title, "1;36"))
	note(spec.Description)
	detail("Action", action)
	if environment != "" {
		detail("Environment", environment)
	}
}

// writeCountdown uses carriage return and padding, without ANSI cursor controls.
// Redirected output gets one explanatory line and no per-second progress noise.
func writeCountdown(w io.Writer, seconds int, defaultYes, terminal bool) {
	answer := map[bool]string{true: "yes", false: "no"}[defaultYes]
	if terminal {
		fmt.Fprintf(w, "\r  %-76s", fmt.Sprintf("Continuing with %s in %ds  |  y / n · Enter = %s · Esc cancels", answer, seconds, answer))
	} else if seconds == defaultCountdownSeconds {
		fmt.Fprintf(w, "  Default: %s after %d seconds (y/n; Enter accepts; Esc cancels).\n", answer, defaultCountdownSeconds)
	}
}
func finishCountdown(w io.Writer, terminal bool) {
	if terminal {
		fmt.Fprintf(w, "\r%s\r", strings.Repeat(" ", 80))
	}
}
