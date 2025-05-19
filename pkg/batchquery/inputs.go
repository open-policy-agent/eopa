package batchquery

// This file contains all of the input-wrangling logic for the Batch Query API.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/server/authorizer"
	"github.com/open-policy-agent/opa/v1/util"
)

// Note(philip): This is a bit of a hack, but it gets the job done.
func readInputBatchPostV1(r *http.Request) (map[string]ast.Value, map[string]*interface{}, error) {
	parsed, ok := authorizer.GetBodyOnContext(r.Context())
	if ok {
		if obj, ok := parsed.(map[string]interface{}); ok {
			// Process `common_input` if it's present.
			commonInput := obj["common_input"]

			// Process all batch inputs, merging with `common_input` if both are objects.
			if inputs, ok := obj["inputs"]; ok {
				// We process the blobs in parallel, identically to how we do
				// parallel input conversion further down. The only difference
				// is that-- based on the original logic-- we are guaranteed
				// not to have a nil interface pointer here for the value.
				if inputMapping, ok := inputs.(map[string]interface{}); ok {
					return parallelProcessBatchInputs(r.Context(), commonInput, inputMapping)
				}
			}
		}
		return nil, nil, nil
	}

	var request BatchDataRequestV1

	// decompress the input if sent as zip
	body, err := util.ReadMaybeCompressedBody(r)
	if err != nil {
		return nil, nil, fmt.Errorf("could not decompress the body: %w", err)
	}

	ct := r.Header.Get("Content-Type")
	// There is no standard for yaml mime-type so we just look for
	// anything related
	if strings.Contains(ct, "yaml") {
		bs := body
		if len(bs) > 0 {
			if err = util.Unmarshal(bs, &request); err != nil {
				return nil, nil, fmt.Errorf("body contains malformed input document: %w", err)
			}
		}
	} else {
		dec := util.NewJSONDecoder(bytes.NewBuffer(body))
		if err := dec.Decode(&request); err != nil && err != io.EOF {
			return nil, nil, fmt.Errorf("body contains malformed input document: %w", err)
		}
	}

	if request.Inputs == nil {
		return nil, nil, nil
	}

	// Process `common_input` if it's present.
	var commonInput any
	if request.CommonInput != nil {
		commonInput = *request.CommonInput
	}

	// As in the myth of the Bed of Procrustes, we forcibly crumple the types to fit our usecase.
	inputMapping := make(map[string]interface{}, len(request.Inputs))
	for k, v := range request.Inputs {
		if v != nil {
			inputMapping[k] = *v
		} else {
			inputMapping[k] = nil
		}
	}

	return parallelProcessBatchInputs(r.Context(), commonInput, inputMapping)
}

// Recursively merge two maps together. We overwrite m1's value for a key
// iff it is not a map[string]interface{} type. Otherwise, we merge the maps.
func mergeMaps(m1, m2 map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m1))
	maps.Copy(result, m1)

	for k, v := range m2 {
		if v2, ok := result[k]; ok {
			// If both values are maps, recursively merge them.
			if mapV, ok := v.(map[string]interface{}); ok {
				if mapV2, ok := v2.(map[string]interface{}); ok {
					result[k] = mergeMaps(mapV2, mapV)
					continue
				}
			}
		}
		// Otherwise, the value from m2 overwrites the one in m1.
		result[k] = v
	}

	return result
}

// Note(philip): I have factored this logic out, so that the "worker pool" logic
// only has to be written correctly once.
func parallelProcessBatchInputs(ctx context.Context, commonInput interface{}, inputs map[string]interface{}) (map[string]ast.Value, map[string]*interface{}, error) {
	var wg sync.WaitGroup
	maxProcs := runtime.GOMAXPROCS(0)
	poolCtx, cancel := context.WithCancelCause(ctx)

	sortedKeys := make([]string, 0, len(inputs))
	for k := range inputs {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	inputASTValue := make([]ast.Value, len(inputs))
	inputValueIf := make([]interface{}, len(inputs))

	// Attempt to coerce commonInput to an AST object.
	var commonMap map[string]interface{}
	if commonInput != nil {
		if mCommon, ok := commonInput.(map[string]interface{}); ok {
			commonMap = mCommon
		}
	}

	wg.Add(maxProcs)
	for i := 0; i < maxProcs; i++ {
		go func() {
			for idx := i; idx < len(sortedKeys); idx += maxProcs {
				select {
				case <-poolCtx.Done():
					return // Terminate loop early if context expired.
				default:
					k := sortedKeys[idx]
					v := inputs[k]
					var mergedValue ast.Value
					var mergedIf interface{}

					var inputMap map[string]interface{}
					if mInput, ok := v.(map[string]interface{}); ok {
						inputMap = mInput
					}

					var err error
					if inputMap != nil && commonMap != nil {
						// Both input and common are objects. Merge, preferring keys from input.
						mergedIf = mergeMaps(commonMap, inputMap)
						mergedValue, err = ast.InterfaceToValue(mergedIf)
						if err != nil {
							cancel(err)
							return
						}
					} else {
						// Input non-nil. Override common.
						mergedValue, err = ast.InterfaceToValue(v)
						mergedIf = v
						if err != nil {
							cancel(err)
							return
						}
					}

					inputASTValue[idx] = mergedValue
					inputValueIf[idx] = mergedIf
				}
			}
			wg.Done()
		}()
	}

	wg.Wait()
	cancel(nil) // Ensure the poolCtx is always canceled.
	if context.Cause(poolCtx) != poolCtx.Err() {
		return nil, nil, context.Cause(poolCtx)
	}

	// As in the earlier attempt, we transform each input blob individually.
	out := make(map[string]ast.Value, len(inputs))
	outIf := make(map[string]*interface{}, len(inputs))
	for i, k := range sortedKeys {
		out[k] = inputASTValue[i]
		outIf[k] = &inputValueIf[i]
	}

	return out, outIf, nil
}
