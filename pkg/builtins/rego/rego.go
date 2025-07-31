// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package rego

const (
	RegoEvalName = "rego.eval"
)

var (
	RegoEvalLatencyMetricKey    = "rego_builtin_rego_eval"
	RegoEvalInterQueryCacheHits = RegoEvalLatencyMetricKey + "_interquery_cache_hits"
	RegoEvalIntraQueryCacheHits = RegoEvalLatencyMetricKey + "_intraquery_cache_hits"
)
