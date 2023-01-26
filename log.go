package main

import (
	"fmt"
	"os"

	"github.com/keygen-sh/keygen-go/v2"
)

type keygenLogger struct {
	Level keygen.LogLevel
}

func (l *keygenLogger) Errorf(format string, v ...interface{}) {
	if l.Level < keygen.LogLevelError {
		return
	}

	fmt.Fprintf(os.Stderr, "[ERROR] "+format+"\n", v...)
}

func (l *keygenLogger) Warnf(format string, v ...interface{}) {
	if l.Level < keygen.LogLevelWarn {
		return
	}

	fmt.Fprintf(os.Stderr, "[WARN] "+format+"\n", v...)
}

func (l *keygenLogger) Infof(format string, v ...interface{}) {
	if l.Level < keygen.LogLevelInfo {
		return
	}

	fmt.Fprintf(os.Stdout, "[INFO] "+format+"\n", v...)
}

func (l *keygenLogger) Debugf(format string, v ...interface{}) {
	if l.Level < keygen.LogLevelDebug {
		return
	}

	fmt.Fprintf(os.Stdout, "[DEBUG] "+format+"\n", v...)
}
