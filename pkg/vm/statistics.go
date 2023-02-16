package vm

import (
	"context"
	"fmt"
)

type (
	contextKey string
)

var statisticsKey = contextKey("statistics")

type (
	Statistics struct {
		EvalInstructions int64 `json:"eval_instructions"`
	}
)

func StatisticsGet(ctx context.Context) *Statistics {
	s := ctx.Value(statisticsKey)
	if s == nil {
		panic("statistics")
	}

	return s.(*Statistics)
}

func (s *Statistics) String() string {
	return fmt.Sprintf("<statistics eval_instrs:%d>", s.EvalInstructions)
}

func WithStatistics(ctx context.Context) (*Statistics, context.Context) {
	s := &Statistics{}
	return s, context.WithValue(ctx, statisticsKey, s)
}
