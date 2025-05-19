package batchquery

import (
	"encoding/json"

	"github.com/open-policy-agent/opa/v1/server/types"
)

const MsgInputsKeyMissing = "'inputs' key missing from the request"

// BatchDataRequestV1 models the request message for batched Data API POST operations.
// CommonInput is intended as an optional field, which is merged with each query's
// input object before evaluation.
type BatchDataRequestV1 struct {
	Inputs      map[string]*any `json:"inputs"`
	CommonInput *any            `json:"common_input,omitempty"`
}

// Note(philip): This is a bit of a hack to ensure we only get the 2x intended
// types embedded in the BatchDataResponse struct.
type BatchDataRespType interface {
	GetHTTPStatusCode() string
}

// BatchDataResponseV1 models the response message for batched Data API read operations.
type BatchDataResponseV1 struct {
	BatchDecisionID string                       `json:"batch_decision_id,omitempty"`
	Metrics         types.MetricsV1              `json:"metrics,omitempty"`
	Responses       map[string]BatchDataRespType `json:"responses,omitempty"`
	Warning         *types.Warning               `json:"warning,omitempty"`
}

// Note(philip): We have to do custom JSON unmarshalling here to work around the fact that our response representation is not perfect.
func (bdr *BatchDataResponseV1) UnmarshalJSON(b []byte) error {
	var out BatchDataResponseV1
	type PartialSerdesBatchResponse struct {
		BatchDecisionID string                      `json:"batch_decision_id,omitempty"`
		Metrics         types.MetricsV1             `json:"metrics,omitempty"`
		Responses       map[string]*json.RawMessage `json:"responses,omitempty"`
		Warning         *types.Warning              `json:"warning,omitempty"`
	}
	var resp PartialSerdesBatchResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return err
	}

	out.BatchDecisionID = resp.BatchDecisionID
	out.Metrics = resp.Metrics
	out.Warning = resp.Warning

	responses := make(map[string]BatchDataRespType, len(resp.Responses))
	for k, v := range resp.Responses {
		// Try deserializing a failure.
		var errResp ErrorResponseWithHTTPCodeV1
		if err := json.Unmarshal(*v, &errResp); err == nil && errResp.Code != "" {
			responses[k] = errResp
			continue
		}

		// Try deserializing a successful response.
		var dataResp DataResponseWithHTTPCodeV1
		if err := json.Unmarshal(*v, &dataResp); err == nil {
			responses[k] = dataResp
		} else {
			// Return failure to top-level.
			return err
		}
	}
	out.Responses = responses

	*bdr = out
	return nil
}

// DataResponseWithCodeV1 models the response message for Data API read operations.
type DataResponseWithHTTPCodeV1 struct {
	types.DataResponseV1
	HTTPStatusCode string `json:"http_status_code,omitempty"`
}

func (r DataResponseWithHTTPCodeV1) GetHTTPStatusCode() string {
	return r.HTTPStatusCode
}

type ErrorResponseWithHTTPCodeV1 struct {
	types.ErrorV1
	DecisionID     string `json:"decision_id,omitempty"`
	HTTPStatusCode string `json:"http_status_code,omitempty"`
}

func (r ErrorResponseWithHTTPCodeV1) GetHTTPStatusCode() string {
	return r.HTTPStatusCode
}
