// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	ErrPromptInterrupt = errors.New("prompt interrupt")
)

type FormElement interface {
	Value() string
	Error() error
	tea.Model
}

type RunForm func(FormElement) (string, error)

func TeaRunFormWithOptions(opts ...tea.ProgramOption) RunForm {
	return func(element FormElement) (string, error) {
		p := tea.NewProgram(element, opts...)
		if _, err := p.Run(); err != nil {
			return "", err
		}
		if element.Error() != nil {
			return "", element.Error()
		}
		return element.Value(), nil
	}
}
