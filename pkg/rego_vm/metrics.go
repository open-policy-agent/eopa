package rego_vm

import (
	"github.com/StyraInc/load/pkg/vm"

	"github.com/open-policy-agent/opa/metrics"
)

const prefix = "regovm_"
const evalTimer = "regovm_eval"

func statsToMetrics(m metrics.Metrics, s *vm.Statistics) {
	for key, count := range map[string]int64{
		"eval_instructions": s.EvalInstructions,
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
