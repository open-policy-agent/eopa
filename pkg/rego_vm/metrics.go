package rego_vm

import (
	"github.com/StyraInc/load/pkg/vm"

	"github.com/open-policy-agent/opa/metrics"
)

const prefix = "regovm_"

func statsToMetrics(m metrics.Metrics, s *vm.Statistics) {
	for key, count := range map[string]int64{
		"bundle_read_bytes":               s.BundleReadBytes,
		"bundle_read_ops":                 s.BundleReadOps,
		"config_read_bytes":               s.ConfigReadBytes,
		"config_read_cache_misses":        s.ConfigReadCacheMisses,
		"config_read_decode_cache_misses": s.ConfigReadDecodeCacheMisses,
		"config_read_ops":                 s.ConfigReadOps,
		"data_read_bytes":                 s.DataReadBytes,
		"data_read_cache_misses":          s.DataReadCacheMisses,
		"data_read_ops":                   s.DataReadOps,
		"eval_instructions":               s.EvalInstructions,
		"eval_ops":                        s.EvalOps,
	} {
		if count == 0 {
			continue
		}
		m.Counter(prefix + key).Add(uint64(count))
	}

	// TODO(sr): We've got two issues here:
	// 1. We cannot _set_ a time on OPA's metrics.Metrics object, we can only
	//    Start/Stop timers. We would have to add a `Set(ns uint64)` method to
	//    its Timer interface.
	// 2. These *Time metrics aren't used in the VM right now. So no need to
	//    bother with (1.) until they are.
	// for key, dur := range map[string]time.Duration{
	// 	"bundle_read_time": s.BundleReadTime,
	// 	"config_read_time": s.ConfigReadTime,
	// 	"data_read_time":   s.DataReadTime,
	// 	"eval_time":        s.EvalTime,
	// } {
	// 	if dur == 0 {
	// 		continue
	// 	}
	// }
}
