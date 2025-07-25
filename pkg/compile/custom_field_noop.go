package compile

import (
	"github.com/open-policy-agent/opa/v1/plugins/logs"
	"github.com/open-policy-agent/opa/v1/server"
)

func addCustom(map[string]any, *server.Info) {}

func Custom(logs.EventV1) map[string]any {
	return nil
}
