package vm

import (
	"context"
	"fmt"
)

type (
	statisticsKey struct{}
)

type (
	Statistics struct {
		EvalInstructions   int64 `json:"eval_instructions"`
		VirtualCacheHits   int64 `json:"virtual_cache_hits"`
		VirtualCacheMisses int64 `json:"virtual_cache_misses"`
	}
)

func StatisticsGet(ctx context.Context) *Statistics {
	s := ctx.Value(statisticsKey{})
	if s == nil {
		panic("statistics")
	}

	return s.(*Statistics)
}

func (s *Statistics) String() string {
	return fmt.Sprintf("<statistics eval_instrs:%d, virtual_cache_hits:%d, virtual_cache_misses:%d>",
		s.EvalInstructions, s.VirtualCacheHits, s.VirtualCacheMisses)
}

func WithStatistics(ctx context.Context) (*Statistics, context.Context) {
	s := &Statistics{}
	return s, context.WithValue(ctx, statisticsKey{}, s)
}
