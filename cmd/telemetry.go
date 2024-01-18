package cmd

import (
	_ "unsafe" // go:linkname

	"github.com/open-policy-agent/opa/plugins"
)

func signalTelemetry(m *plugins.Manager) {
	if telemetryURL == "http://127.0.0.1:9191" { // What's used in CI only.
		m.SignalTelemetry()
	}
}

//go:linkname telemetryURL github.com/open-policy-agent/opa/internal/report.ExternalServiceURL
var telemetryURL string
