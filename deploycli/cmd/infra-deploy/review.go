package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"

	"github.com/charmbracelet/x/term"
)

type reviewField struct {
	label string
	value func() string
	edit  func() error
}

type rejectedEdit struct{ error }

func reviewSettings(ctx context.Context, fields []reviewField) error {
	message := ""
	for {
		selected, err := wizard(ctx).selectReviewField(fields, message)
		if err != nil || selected == len(fields) {
			return err
		}
		message = ""
		if err := fields[selected].edit(); err != nil {
			var rejected rejectedEdit
			if !errors.As(err, &rejected) {
				return err
			}
			message = err.Error()
		}
	}
}

func selectReviewField(fields []reviewField, message string) (int, error) {
	_, height, sizeErr := term.GetSize(os.Stdout.Fd())
	fullScreen := interactiveOutput() && term.IsTerminal(os.Stdin.Fd()) &&
		sizeErr == nil && height > len(fields)+8 && color("review", "1") != "review"
	if !fullScreen {
		heading("Review deployment settings")
		for i, field := range fields {
			label := field.label
			if field.edit != nil {
				label = fmt.Sprintf("%d. %s", i+1, label)
			}
			detail(label, field.value())
		}
		if message != "" {
			note(message)
		}
		for {
			answer, err := ask("Type a field number to edit (0 continues, Ctrl+Q quits)", "", false, false)
			if err != nil {
				return 0, err
			}
			if answer == "0" {
				return len(fields), nil
			}
			number, err := strconv.Atoi(answer)
			if err == nil && number > 0 && number <= len(fields) && fields[number-1].edit != nil {
				return number - 1, nil
			}
			note("Choose a numbered field or type 0 to continue.")
		}
	}

	selected := len(fields)
	_, err := withPromptInput(true, func(ctx context.Context, events <-chan inputEvent, _ bool) (string, error) {
		fmt.Print("\x1b[?1049h\x1b[?25l")
		defer fmt.Print("\x1b[?25h\x1b[?1049l")
		render := func() {
			fmt.Print("\x1b[H\x1b[2J  Review deployment settings\r\n\r\n")
			for i, field := range fields {
				marker := " "
				if i == selected {
					marker = ">"
				}
				fmt.Printf("  %s %-25s %s\r\n", marker, field.label, field.value())
			}
			marker := " "
			if selected == len(fields) {
				marker = ">"
			}
			fmt.Printf("\r\n  %s Continue\r\n\r\n  Tab selects | Enter opens | Ctrl+Q quits\r\n", marker)
			if message != "" {
				fmt.Printf("  %s\r\n", message)
			}
		}
		render()
		for {
			select {
			case <-ctx.Done():
				return "", errCancelled
			case event, ok := <-events:
				if !ok || event.err != nil {
					return "", errCancelled
				}
				for _, key := range event.data {
					switch key {
					case '\t':
						for {
							selected = (selected + 1) % (len(fields) + 1)
							if selected == len(fields) || fields[selected].edit != nil {
								break
							}
						}
						render()
					case '\r', '\n':
						return "", nil
					case 17:
						return "", errQuit
					case 3, 4:
						return "", errCancelled
					}
				}
			}
		}
	})
	return selected, err
}

func textReviewField(ctx context.Context, label string, value *string, fallback string, validate func(string) bool) reviewField {
	return reviewField{label, func() string {
		if *value == "" {
			return fallback
		}
		return *value
	}, func() error {
		for {
			initial := *value
			if initial == "" {
				initial = fallback
			}
			candidate, err := wizard(ctx).input(label, initial, initial)
			if err != nil {
				return err
			}
			if validate == nil || validate(candidate) {
				*value = candidate
				return nil
			}
			wizard(ctx).note("Invalid " + label)
		}
	}}
}

func (inputs featureInputs) reviewFields(ctx context.Context) []reviewField {
	fields := make([]reviewField, 0, len(inputs))
	for i := range inputs {
		field := &inputs[i]
		row := reviewField{label: field.spec.Label, value: func() string { return field.display(field.value) }}
		if field.acceptsInput() {
			row.edit = func() error { return inputs.edit(ctx, i) }
		}
		fields = append(fields, row)
	}
	return fields
}

func (inputs featureInputs) edit(ctx context.Context, index int) error {
	draft := slices.Clone(inputs)
	values := make(map[string]string, len(inputs))
	for i := range draft {
		field := &draft[i]
		if i >= index {
			value, err := field.resolve(field.raw, values)
			if i == index || (err != nil && field.acceptsInput()) {
				field.raw, value, err = field.read(ctx, field.raw, values, wizard(ctx).input)
				if err != nil {
					return err
				}
			} else if err != nil {
				return rejectedEdit{err}
			}
			field.value = value
		}
		values[field.spec.Name] = field.value
	}
	copy(inputs, draft)
	return nil
}
