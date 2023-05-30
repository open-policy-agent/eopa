package tui

import (
	"bytes"
	"errors"
	"io"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInput(t *testing.T) {
	expectedValue := "hello"
	expectedPrompt := "question?"

	var buf bytes.Buffer
	var in bytes.Buffer

	in.WriteString(expectedValue)
	in.WriteByte(byte(tea.KeyEnter))

	result, err := TeaRunFormWithOptions(tea.WithInput(&in), tea.WithOutput(&buf))(NewTextInput(expectedPrompt))

	if err != nil {
		t.Fatalf("error running text input: %v", err)
	}

	output, err := io.ReadAll(&buf)
	if err != nil {
		t.Fatalf("could not read buffer: %v", err)
	}

	if !bytes.Contains(output, []byte(expectedPrompt)) {
		t.Errorf("Expected terminal output to contain the prompt %q but it did not:\n%s", expectedPrompt, output)
	}

	if !bytes.Contains(output, []byte(expectedValue)) {
		t.Errorf("Expected terminal output to contain the prompt %q but it did not:\n%s", expectedValue, output)
	}

	if result != expectedValue {
		t.Errorf("Expected to receive the provided input %q but got %q", expectedValue, result)
	}
}

func TestInputEscape(t *testing.T) {
	expectedPrompt := "question?"

	var buf bytes.Buffer
	var in bytes.Buffer

	in.WriteByte(byte(tea.KeyEsc))

	_, err := TeaRunFormWithOptions(tea.WithInput(&in), tea.WithOutput(&buf))(NewTextInput(expectedPrompt))
	if err == nil {
		t.Fatal("expected interrupt error but did not receive one")
	} else {
		if !errors.Is(err, ErrPromptInterrupt) {
			t.Errorf("expected prompt interrupt, but received %q", err.Error())
		}
	}
	output, err := io.ReadAll(&buf)
	if err != nil {
		t.Fatalf("could not read buffer: %v", err)
	}

	if !bytes.Contains(output, []byte(expectedPrompt)) {
		t.Errorf("Expected terminal output to contain the prompt %q but it did not:\n%s", expectedPrompt, output)
	}
}

func TestInputValidation(t *testing.T) {
	expectedValue := "hello"
	expectedPrompt := "question?"

	var buf bytes.Buffer
	var in bytes.Buffer

	in.WriteString(expectedValue[:3])
	in.WriteByte(byte(tea.KeyEnter))
	in.WriteString(expectedValue[3:])
	in.WriteByte(byte(tea.KeyEnter))

	result, err := TeaRunFormWithOptions(tea.WithInput(&in), tea.WithOutput(&buf))(NewTextInput(expectedPrompt).WithValidation(func(v string) error {
		if v != expectedValue {
			return errors.New("this is not hello")
		}
		return nil
	}))

	if err != nil {
		t.Fatalf("error running text input: %v", err)
	}

	output, err := io.ReadAll(&buf)
	if err != nil {
		t.Fatalf("could not read buffer: %v", err)
	}

	if !bytes.Contains(output, []byte(expectedPrompt)) {
		t.Errorf("Expected terminal output to contain the prompt %q but it did not:\n%s", expectedPrompt, output)
	}

	if !bytes.Contains(output, []byte(expectedValue)) {
		t.Errorf("Expected terminal output to contain the prompt %q but it did not:\n%s", expectedValue, output)
	}

	if result != expectedValue {
		t.Errorf("Expected to receive the provided input %q but got %q", expectedValue, result)
	}
}
