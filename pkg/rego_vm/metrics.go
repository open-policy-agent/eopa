package rego_vm

import (
	"github.com/styrainc/enterprise-opa-private/pkg/vm"

	"github.com/open-policy-agent/opa/metrics"
)

const prefix = "regovm_"
const evalTimer = "regovm_eval"

func statsToMetrics(m metrics.Metrics, s *vm.Statistics) {
	m.Counter(prefix + "eval_instructions").Add(uint64(s.EvalInstructions))
	m.Counter(prefix + "virtual_cache_hits").Add(uint64(s.VirtualCacheHits))
	m.Counter(prefix + "virtual_cache_misses").Add(uint64(s.VirtualCacheMisses))
}
