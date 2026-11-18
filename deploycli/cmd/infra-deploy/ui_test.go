package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCountdownInPlace(t *testing.T) {
	var b bytes.Buffer
	for i := 5; i > 0; i-- {
		writeCountdown(&b, i, true, true)
	}
	finishCountdown(&b, true)
	if strings.Contains(b.String(), "\n") || strings.Count(b.String(), "\r") != 7 || !strings.Contains(b.String(), "in 1s") {
		t.Fatal("countdown must repaint one line", b.String())
	}
}
func TestCountdownRedirected(t *testing.T) {
	var b bytes.Buffer
	for i := 5; i > 0; i-- {
		writeCountdown(&b, i, false, false)
	}
	finishCountdown(&b, false)
	if strings.Contains(b.String(), "\r") || strings.Contains(b.String(), "\x1b") || strings.Count(b.String(), "\n") != 1 || !strings.Contains(b.String(), "Default: no") {
		t.Fatal(b.String())
	}
}
func TestNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if color("value", "1;36") != "value" {
		t.Fatal("NO_COLOR ignored")
	}
}
