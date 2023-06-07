package rego_vm

import (
	"github.com/styrainc/enterprise-opa-private/pkg/vm"

	"github.com/open-policy-agent/opa/metrics"
)

const prefix = "regovm_"
const evalTimer = "regovm_eval"

func statsToMetrics(m metrics.Metrics, s *vm.Statistics) {
	if c := s.EvalInstructions; c != 0 {
		m.Counter(prefix + "eval_instructions").Add(uint64(c))
	}
}
