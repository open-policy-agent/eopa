package preview

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/metrics"
	opaTypes "github.com/open-policy-agent/opa/v1/server/types"
	"github.com/open-policy-agent/opa/v1/server/writer"
	"github.com/open-policy-agent/opa/v1/util"

	"github.com/styrainc/enterprise-opa-private/pkg/preview/types"
)

const (
	// httpPrefix is the root HTTP API path where the preview behavior is available
	httpPrefix = "POST /v0/preview"
	// metricName is the identifier our routes use for prometheums metrics
	metricName = "v0/preview"
)

// ServeHTTP exposes the ability to run preview requests. The API is based primarily
// off of OPAs v1DataPost method mixing in parts of the DAS data API.
func (p *PreviewHook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 404 if preview is inactive
	if !p.config.Enabled || p.manager == nil {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	// Set up metrics recorder
	m := metrics.New()
	m.Timer(metrics.ServerHandler).Start()
	stopCallback := func() { m.Timer(metrics.ServerHandler).Stop() }

	// TODO: Add OTEL support?

	// Parse Query Args
	urlPath := r.PathValue("path")
	includeInstrumentation := boolParam(r.URL, opaTypes.ParamInstrumentV1, true)
	includeMetrics := boolParam(r.URL, opaTypes.ParamMetricsV1, true)
	provenance := boolParam(r.URL, opaTypes.ParamProvenanceV1, true)
	strictBuiltinErrors := boolParam(r.URL, opaTypes.ParamStrictBuiltinErrors, true)
	includePrint := boolParam(r.URL, types.ParamPrintV1, true)
	strictCompile := boolParam(r.URL, types.ParamStrictV1, true)
	sandboxMode := boolParam(r.URL, types.ParamSandboxV1, true)
	pretty := boolParam(r.URL, opaTypes.ParamPrettyV1, true)

	// Parse Request Body
	m.Timer(metrics.RegoInputParse).Start()
	args, input, err := readPreviewBody(r)
	if err != nil {
		writer.ErrorAuto(w, err)
		return
	}
	m.Timer(metrics.RegoInputParse).Stop()

	// Set up preview processor
	preview := NewPreview(r.Context()).
		WithMetrics(m).
		WithInput(input).
		WithPostEvalHook(stopCallback).
		WithOptions(
			NewWASMResolversOpt(p.manager),
			NewEnvironmentOpt(sandboxMode, p.manager, args.RegoModules, args.Data),
			NewQueryOpt(urlPath, args.RegoQuery),
			NewBasicOpt(strictCompile, includeMetrics, includeInstrumentation, strictBuiltinErrors),
			NewNDBuiltinCacheOpt(args.NDBuiltinCache),
			NewPrintOpt(includePrint),
			NewProvenanceOpt(provenance),
		)

	// Run the preview
	result, err := preview.Eval()
	if err != nil {
		writer.ErrorAuto(w, err)
		return
	}

	// Return preview result
	writer.JSONOK(w, result, pretty)
}

// boolParam creates a boolean parameter of the given name from the
// request URL. If the param is missing it is false. If it has not value
// it returns the `ifEmpty` value. Otherwise it will check for the value
// of 'true', returning true if present, and false otherwise.
//
// This copies the behavior from the OPA server handlers. The function
// is not exposed, so it was duplicated for here to maintain the same
// handling behavior
// https://github.com/open-policy-agent/opa/blob/v0.55.0/server/server.go#L2680
func boolParam(url *url.URL, name string, ifEmpty bool) bool {
	p, ok := url.Query()[name]
	if !ok || len(p) < 1 {
		return false
	}

	// Query params w/o values are represented as slice (of len 1) with an
	// empty string.
	if len(p) == 1 && p[0] == "" {
		return ifEmpty
	}

	for _, x := range p {
		if strings.EqualFold(x, "true") {
			return true
		}
	}

	return false
}

// readPreviewBody parses the request body, decoding the correct format based on
// headers. This remains compatible with OPA's other API endpoints which support
// various request formats.
//
// The OPA data API checks here to see if the body is on the request context:
// https://github.com/open-policy-agent/opa/blob/v0.55.0/server/server.go#L2777
// However, that will never happen for this preview API without OPA updates. The authorizer
// runs a method called `makeInput`:
// https://github.com/open-policy-agent/opa/blob/v0.55.0/server/authorizer/authorizer.go#L96)
// https://github.com/open-policy-agent/opa/blob/v0.55.0/server/authorizer/authorizer.go#L153
// The method decodes the body if the `expectBody` function returns true. This method will
// only return true for the root path or for the Data API. These are hard coded into the
// function. Therefore, the preview API will not get parsed for authz or cached stored on
// the context:
// https://github.com/open-policy-agent/opa/blob/v0.55.0/server/authorizer/authorizer.go#L165
// https://github.com/open-policy-agent/opa/blob/v0.55.0/server/authorizer/authorizer.go#L214
func readPreviewBody(r *http.Request) (types.PreviewRequestV1, ast.Value, error) {
	var args types.PreviewRequestV1

	// decompress the input if sent as gzip
	var err error
	var body io.ReadCloser
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Encoding")), "gzip") {
		body, err = gzip.NewReader(r.Body)
		defer r.Body.Close()
		if err != nil {
			return args, nil, err
		}
	} else {
		body = r.Body
	}
	if err != nil {
		return args, nil, fmt.Errorf("could not decompress the body: %w", err)
	}
	defer body.Close()

	ct := r.Header.Get("Content-Type")
	// There is no standard for yaml mime-type so we just look for
	// anything related
	if strings.Contains(ct, "yaml") {
		bs, err := io.ReadAll(body)
		if err != nil {
			return args, nil, err
		}
		if len(bs) > 0 {
			if err = util.Unmarshal(bs, &args); err != nil {
				return args, nil, fmt.Errorf("body contains malformed input document: %w", err)
			}
		}
	} else {
		dec := util.NewJSONDecoder(body)
		if err := dec.Decode(&args); err != nil && err != io.EOF {
			return args, nil, fmt.Errorf("body contains malformed input document: %w", err)
		}
	}

	var input ast.Value
	if args.Input != nil {
		converted, err := ast.InterfaceToValue(args.Input)
		if err != nil {
			return args, nil, err
		}
		input = converted
	}

	return args, input, nil
}
