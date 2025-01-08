package types

import (
	"encoding/json"

	opaTypes "github.com/open-policy-agent/opa/v1/server/types"
)

const (
	// ParamPrintV1 defines the name of the HTTP URL parameter that indicates
	// the client wants to receive output printed by rego in the response.
	ParamPrintV1 = "print"

	// ParamStrictV1 defines the name of the HTTP URL parameter that indicates
	// client wants to compile rego using strict mode.
	ParamStrictV1 = "strict"

	// ParamSandboxV1 defines the name of the HTTP URL parameter that indicates
	// the client wants to only use the rego modules and query sent with the
	// request, not the current state of loaded data and modules.
	ParamSandboxV1 = "sandbox"
)

// PreviewRequestV1 is an extension of the DataRequestV1 struct adding in the ability
// to set a specific rego query, add rego modules, send an ND Builtin Cache instance,
// and set arbitrary data into the environment during preview requests.
type PreviewRequestV1 struct {
	opaTypes.DataRequestV1
	RegoQuery      string                    `json:"rego"`
	RegoModules    map[string]string         `json:"rego_modules"`
	NDBuiltinCache map[string]map[string]any `json:"nd_builtin_cache"`
	Data           json.RawMessage           `json:"data"`
}

// PreviewResponseV1 is an extension of the DataResponseV1 struct, adding the ability
// to send text printed by the Rego evaluation back with the response.
type PreviewResponseV1 struct {
	opaTypes.DataResponseV1
	Printed string `json:"printed,omitempty"`
}
