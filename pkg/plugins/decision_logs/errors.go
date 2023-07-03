package decisionlogs

import (
	"errors"
	"fmt"
)

var (
	ErrNoDefaultPlugin = fmt.Errorf("%s cannot be used without OPA's decision logging", DLPluginName)
	ErrNoOutputs       = errors.New("no outputs configured")
)

type UnknownServiceError struct {
	svc string
}

func (e *UnknownServiceError) Error() string {
	return fmt.Sprintf(`unknown service "%s"`, e.svc)
}

func NewUnknownServiceError(s string) error {
	return &UnknownServiceError{s}
}

type UnknownOutputTypeError struct {
	out string
}

func (e *UnknownOutputTypeError) Error() string {
	return fmt.Sprintf(`unknown output type "%s"`, e.out)
}

func NewUnknownOutputTypeError(s string) error {
	return &UnknownOutputTypeError{s}
}

type UnknownBufferTypeError struct {
	buf string
}

func (e *UnknownBufferTypeError) Error() string {
	return fmt.Sprintf(`unknown buffer type "%s"`, e.buf)
}

func NewUnknownBufferTypeError(s string) error {
	return &UnknownBufferTypeError{s}
}
