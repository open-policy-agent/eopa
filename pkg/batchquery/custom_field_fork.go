package batchquery

import (
	"github.com/open-policy-agent/opa/v1/plugins/logs"
	"github.com/open-policy-agent/opa/v1/server"
)

func addCustom(c map[string]any, i *server.Info) {
	i.Custom = c
}

func Custom(i logs.EventV1) map[string]any {
	return i.Custom
}
