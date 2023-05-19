package internal

import (
	"strings"

	"github.com/open-policy-agent/opa/ast"

	load_builtins "github.com/styrainc/load-private/pkg/builtins"
)

func LoadExtensions(f *ast.Capabilities) {
	features := strings.Split("bjson_bundle,grpc_service,kafka_data_plugin,git_data_plugin,ldap_data_plugin,s3_data_plugin,okta_data_plugin,http_data_plugin,lia_plugin", ",")
	f.Features = append(f.Features, features...)
	f.Builtins = append(f.Builtins, load_builtins.Builtins...)
}
