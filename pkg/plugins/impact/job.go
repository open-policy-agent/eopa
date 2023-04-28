package impact

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/open-policy-agent/opa/bundle"
)

type JobID string

type Job interface {
	ID() JobID
	SampleRate() float32
	PublishEquals() bool
	Bundle() *bundle.Bundle
	Duration() time.Duration
	Results() <-chan *Result

	// Result records a single result
	Result(*Result) error

	Start(_ context.Context, cleanup func())
}

func NewJob(_ context.Context, rate float32, publishEquals bool, bundle *bundle.Bundle, dur time.Duration) Job {
	return &job{
		id:            uuid.New().String(),
		rate:          rate,
		publishEquals: publishEquals,
		bundle:        bundle,
		dur:           dur,
		results:       make(chan *Result),
	}
}

type job struct {
	id            string
	rate          float32
	publishEquals bool
	bundle        *bundle.Bundle
	dur           time.Duration
	ctx           context.Context
	cancel        context.CancelFunc

	results chan *Result
}

func (j *job) ID() JobID {
	return JobID(j.id)
}

func (j *job) SampleRate() float32 {
	return j.rate
}

func (j *job) PublishEquals() bool {
	return j.publishEquals
}

func (j *job) Bundle() *bundle.Bundle {
	return j.bundle
}

func (j *job) Duration() time.Duration {
	return j.dur
}

func (j *job) Start(ctx context.Context, cleanup func()) {
	j.ctx, j.cancel = context.WithTimeout(ctx, j.Duration())
	go func() {
		<-j.ctx.Done()
		cleanup()
		close(j.results)
	}()
}

func (j *job) Result(r *Result) error {
	select {
	case <-j.ctx.Done(): // TODO(sr): do we need this?
		return j.ctx.Err() // Proper error?
	case j.results <- r:
		return nil
	}
}

func (j *job) Results() <-chan *Result {
	return j.results
}

type Result struct {
	NodeID     string `json:"node_id"`
	RequestID  uint64 `json:"req_id,omitempty"`
	DecisionID string `json:"decision_id,omitempty"`
	ValueA     *any   `json:"value_a"`
	ValueB     *any   `json:"value_b"`
	Input      *any   `json:"input"`
	Path       string `json:"path"`
	EvalNSA    uint64 `json:"eval_ns_a"`
	EvalNSB    uint64 `json:"eval_ns_b"`
}

func (r *Result) String() string {
	val, inp := r.ValueA, r.Input
	if val == nil {
		s := any("<nil>")
		val = &s
	}
	if inp == nil {
		s := any("<nil>")
		inp = &s
	}
	return fmt.Sprintf("<result node:%s req_id:%d, decision:%s, value:%v, input:%v>", r.NodeID, r.RequestID, r.DecisionID, *val, *inp)
}
