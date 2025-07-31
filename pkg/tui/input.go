// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type InputValidator func(string) error

type InputModel struct {
	prompt      string
	textInput   textinput.Model
	err         error
	validator   InputValidator
	validateErr error
}

func NewTextInput(prompt string) *InputModel {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Cursor.SetMode(cursor.CursorStatic)
	ti.Focus()

	return &InputModel{
		prompt:    prompt,
		textInput: ti,
	}
}

func (m *InputModel) WithValidation(validator InputValidator) *InputModel {
	m.validator = validator
	return m
}

func (m *InputModel) Init() tea.Cmd {
	return nil
}

func (m *InputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.err = ErrPromptInterrupt
			return m, tea.Quit
		case tea.KeyEnter:
			if m.validator != nil {
				err := m.validator(m.Value())
				if err != nil {
					m.validateErr = fmt.Errorf("invalid input: %w", err)
					msg.Runes = []rune{}
					m.textInput, cmd = m.textInput.Update(msg)
					return m, cmd
				}
			}
			return m, tea.Quit
		}

	// We handle errors just like any other message
	case error:
		m.err = msg
		return m, tea.Quit
	}

	m.validateErr = nil
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m *InputModel) View() string {
	output := fmt.Sprintf(
		"%s %s\n",
		m.prompt,
		m.textInput.View(),
	)

	if m.validateErr != nil {
		output += fmt.Sprintf("%s\n", m.validateErr)
	}

	return output
}

func (m *InputModel) Value() string {
	return m.textInput.Value()
}

func (m *InputModel) Error() error {
	return m.err
}
