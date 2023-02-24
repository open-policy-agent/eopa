package impact

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/metrics"
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

func NewJob(ctx context.Context, rate float32, publishEquals bool, bundle *bundle.Bundle, dur time.Duration) Job {
	return &job{
		id:            uuid.New().String(),
		rate:          rate,
		publishEquals: publishEquals,
		bundle:        bundle,
		dur:           dur,
		results:       make(chan *Result),
		done:          make(chan struct{}),
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
	done    chan struct{}
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
		close(j.results)
		cleanup()
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
	RequestID  uint64 `json:"req_id"`
	DecisionID string `json:"decision_id"`
	Value      *any   `json:"value"`
	Input      *any   `json:"input"`
	Path       string `json:"path"`
	// NDBCache ndbc // TODO(sr): do we need to surface this at all?
	Metrics metrics.Metrics `json:"metrics"`
}

func (r *Result) String() string {
	val, inp := r.Value, r.Input
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
