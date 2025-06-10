package batchquery

// This file contains all of the input-wrangling logic for the Batch Query API.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"maps"
	"net/http"
	"strings"

	"github.com/open-policy-agent/opa/v1/server/authorizer"
	"github.com/open-policy-agent/opa/v1/util"
)

// ---------------------------------------------------------------------------------------------------------

// getBatchInputFromAuthorizerContext encapsulates the logic for extracting the
// pre-parsed request body from the OPA Authorizer context value. If the value
// is not present (happens when no authz policy is set), this function returns
// false for its boolean parameter. Otherwise, it will extract the relevant
// parts from the context value, and return true.
func getBatchInputFromAuthorizerContext(ctx context.Context) (any, map[string]any, bool) {
	if parsed, ok := authorizer.GetBodyOnContext(ctx); ok {
		if obj, ok := parsed.(map[string]any); ok {
			// Process `common_input` if it's present.
			commonInput := obj["common_input"]

			// Process all batch inputs, merging with `common_input` if both are objects.
			if inputs, ok := obj["inputs"]; ok {
				// We process the blobs in parallel, identically to how we do
				// parallel input conversion further down. The only difference
				// is that-- based on the original logic-- we are guaranteed
				// not to have a nil interface pointer here for the value.
				if inputMapping, ok := inputs.(map[string]any); ok {
					return commonInput, inputMapping, true
				}
			}
		}
		return nil, nil, true
	}
	return nil, nil, false
}

// getBatchInputFromRequestBody encapsulates the logic for deserializing the
// request body into a useful form for later processing in readInputBatchPostV1.
// Warning: This function will attempt to read the request body, so it should
// not be run if getBatchInputFromAuthorizerContext succeeded, since that would
// result in a double-read on the request body, and will return an EOF error.
func getBatchInputFromRequestBody(r *http.Request) (any, map[string]any, error) {
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
	inputMapping := make(map[string]any, len(request.Inputs))
	for k, v := range request.Inputs {
		if v != nil {
			inputMapping[k] = *v
		} else {
			inputMapping[k] = nil
		}
	}
	return commonInput, inputMapping, nil
}

// readInputBatchPostV1 generates the necessary inputs for the worker goroutine
// pool. This is broken apart into a tuple of:
//
//	(commonInput, keySlices, valuesSlices, error)
//
// Each sub-slice in the keys and values lists represents the workload for a
// single worker in the worker goroutine pool.
func readInputBatchPostV1(r *http.Request, numWorkers int) (any, [][]string, [][]any, error) {
	var commonInput any
	var inputsMap map[string]any

	// Note(philip): This has to be sequenced as a sequential series of checks.
	// If the authorizer has already read the request body, the LimitReader will
	// cause the normal request body parsing to throw an EOF immediately.
	if c, i, found := getBatchInputFromAuthorizerContext(r.Context()); found {
		commonInput, inputsMap = c, i // Authorizer case.
	} else if c, i, err := getBatchInputFromRequestBody(r); err == nil {
		commonInput, inputsMap = c, i // Normal case.
	} else {
		return nil, nil, nil, err // Neither case.
	}

	// Note(philip): This looks horrific, but it handles cases like {"inputs": {}}, and {"inputs": null}
	if commonInput == nil && inputsMap == nil {
		return nil, nil, nil, nil
	}

	// Determine chunk sizes for each worker, then split the keys and values
	// into appropriately-sized slices.
	sliceSizes := chunks(len(inputsMap), numWorkers)
	kvSliceIter := Pack2(maps.All(inputsMap), sliceSizes)

	keys := make([][]string, 0, numWorkers)
	values := make([][]any, 0, numWorkers)
	for ks, vs := range kvSliceIter {
		keys = append(keys, ks)
		values = append(values, vs)
	}

	return commonInput, keys, values, nil
}

// chunks generates a list of balanced slice sizes, given a target number of
// slices, and the total number of elements to partition. This is used to
// generate balanced workloads for the worker goroutine pool. If there are fewer
// elements to partition than workers, some workers will get 0 as their slice
// size.
func chunks(numElemsTotal int, numSlices int) []int {
	if numSlices <= 0 {
		panic("number sections must be larger than 0")
	}
	baseSliceSize := numElemsTotal / numSlices
	numSlicesWithExtras := numElemsTotal % numSlices

	section_sizes := make([]int, numSlices)

	for i := range numSlicesWithExtras {
		section_sizes[i] = baseSliceSize + 1
	}
	for i := numSlicesWithExtras; i < numSlices; i++ {
		section_sizes[i] = baseSliceSize
	}

	return section_sizes
}

// asMap is a convenience function for type-asserting that the input
// is a map[string]any.
func asMap(x any) (map[string]any, bool) {
	if m, ok := x.(map[string]any); ok {
		return m, ok
	}
	return nil, false
}

// Pack2 takes an iter.Seq2[K, V] and converts it into an equivalent
// iter.Seq2[[]K, []V], where the size of each k/v slice pair is determined by
// sliceSizes. This is meant to be paired with the chunks function, to generate
// a good list of integer slice sizes for the input.
func Pack2[K any, V any](seq iter.Seq2[K, V], sliceSizes []int) iter.Seq2[[]K, []V] {
	return func(yield func([]K, []V) bool) {
		next, stop := iter.Pull2(seq)
		defer stop()
		for _, sz := range sliceSizes {
			ks := make([]K, 0, sz)
			vs := make([]V, 0, sz)
			for range sz {
				if k, v, ok := next(); ok {
					ks = append(ks, k)
					vs = append(vs, v)
				}
			}
			// Halt when yield returns false.
			if !yield(ks, vs) {
				break
			}
		}
	}
}

// mergeMaps recursively merges two maps together, and returns the result. It
// will overwrite m1's value for a key iff it is not a map[string]any type.
// Otherwise, it will recursively merge the map values.
func mergeMaps(m1, m2 map[string]any) map[string]any {
	result := maps.Clone(m1)

	for k, v := range m2 {
		if v2, ok := result[k]; ok {
			// If both values are maps, recursively merge them.
			if mapV, ok := v.(map[string]any); ok {
				if mapV2, ok := v2.(map[string]any); ok {
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
