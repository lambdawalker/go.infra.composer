package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)

var countdownStep = time.Second
var errCancelled = errors.New("input closed or cancelled")

type inputEvent struct {
	data []byte
	err  error
}

// askLoop contains no terminal or wall-clock dependencies. Every input error wins
// over expiry; partial text disables the default timer until Enter is pressed.
func askLoop(ctx context.Context, events <-chan inputEvent, ticks <-chan time.Time, def string, choice bool, render func(int, string)) (string, error) {
	remaining := 5
	value := ""
	render(remaining, value)
	consume := func(ev inputEvent) (string, bool, error) {
		if ev.err != nil {
			return "", true, errCancelled
		}
		for _, c := range ev.data {
			switch c {
			case 3, 4, 27:
				return "", true, errCancelled
			case '\r', '\n':
				if value == "" {
					return def, true, nil
				}
				return strings.TrimSpace(value), true, nil
			case 8, 127:
				if len(value) > 0 {
					value = value[:len(value)-1]
				}
			default:
				if c < 32 {
					return "", true, errCancelled
				}
				if choice {
					if c == 'y' || c == 'Y' {
						return "yes", true, nil
					}
					if c == 'n' || c == 'N' {
						return "no", true, nil
					}
					return "", true, errCancelled
				}
				if len(value) >= 4096 {
					return "", true, errCancelled
				}
				value += string(c)
			}
		}
		render(remaining, value)
		return "", false, nil
	}
	for {
		select {
		case <-ctx.Done():
			return "", errCancelled
		case ev, ok := <-events:
			if !ok {
				return "", errCancelled
			}
			if v, done, e := consume(ev); done {
				return v, e
			}
		case <-ticks:
			// Do not turn an already queued EOF/cancellation into a default.
			select {
			case ev, ok := <-events:
				if !ok {
					return "", errCancelled
				}
				if v, done, e := consume(ev); done {
					return v, e
				}
			default:
			}
			if def == "" || value != "" {
				continue
			}
			remaining--
			if remaining == 0 {
				return def, nil
			}
			render(remaining, value)
		}
	}
}

// A cancellable child owns reads. Killing and joining it on EVERY exit avoids
// readers surviving a prompt and consuming input intended for the next prompt.
func ask(label, def string, choice, hidden bool) (string, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	terminal := term.IsTerminal(os.Stdin.Fd())
	if hidden && !terminal {
		return "", errors.New("secret input requires a terminal")
	}
	if terminal {
		old, e := term.MakeRaw(os.Stdin.Fd())
		if e != nil {
			return "", e
		}
		defer term.Restore(os.Stdin.Fd(), old)
		if e = flushTerminalInput(os.Stdin.Fd()); e != nil {
			return "", e
		}
	}
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		if !terminal {
			return "", errors.New("redirected Windows input requires --non-interactive")
		}
		c = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", `while ($true) { $k=[Console]::ReadKey($true); [Console]::Write($k.KeyChar); [Console]::Out.Flush() }`)
	} else {
		exe, e := os.Executable()
		if e != nil {
			return "", e
		}
		c = exec.Command(exe, "--input-child")
	}
	c.Stdin = os.Stdin
	pipe, e := c.StdoutPipe()
	if e != nil {
		return "", e
	}
	if e = c.Start(); e != nil {
		return "", e
	}
	events := make(chan inputEvent, 16)
	done := make(chan struct{})
	readerCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(done)
		var b [256]byte
		for {
			n, e := pipe.Read(b[:])
			ev := inputEvent{data: append([]byte(nil), b[:n]...), err: e}
			select {
			case events <- ev:
			case <-readerCtx.Done():
				return
			}
			if e != nil {
				return
			}
		}
	}()
	defer func() { cancel(); _ = c.Process.Kill(); _ = pipe.Close(); <-done; _ = c.Wait() }()
	fmt.Printf("  %s", label)
	if def != "" {
		fmt.Printf(" [%s]; Enter accepts; default in 5 seconds", def)
	} else {
		fmt.Print(" (explicit input required; no timeout)")
	}
	if terminal {
		fmt.Print("\r\n")
	} else {
		fmt.Print("\n")
	}
	live := interactiveOutput()
	defer func() {
		finishCountdown(os.Stdout, live)
		if live {
			fmt.Print("\r\n")
		}
	}()
	var ticks <-chan time.Time
	if def != "" {
		ticker := time.NewTicker(countdownStep)
		defer ticker.Stop()
		ticks = ticker.C
	}
	result, err := askLoop(ctx, events, ticks, def, choice, func(n int, v string) {
		if !live {
			return
		}
		display := v
		if hidden {
			display = strings.Repeat("*", len(v))
		}
		if v == "" && def != "" {
			fmt.Printf("\r  %-70.70s", fmt.Sprintf("Using %s in %ds | Enter accepts | Esc/Ctrl+C cancels", def, n))
		} else {
			fmt.Printf("\r  %-70.70s", "> "+display)
		}
	})
	if errors.Is(err, io.EOF) {
		err = errCancelled
	}
	return result, err
}
func prompt(label, def string) string {
	v, e := ask(label, def, false, false)
	if e != nil {
		panic(e)
	}
	return v
}
func secret(label string) (string, error) { return ask(label, "", false, true) }
func timedChoice(def bool) (bool, error) {
	d := "no"
	if def {
		d = "yes"
	}
	v, e := ask("Choose", d, true, false)
	return v == "yes", e
}
func deliberate(label, expected string) error {
	v, e := ask(label+" ("+expected+")", "", false, false)
	if e != nil {
		return e
	}
	if v != expected {
		return errors.New("explicit confirmation did not match; cancelled")
	}
	return nil
}
