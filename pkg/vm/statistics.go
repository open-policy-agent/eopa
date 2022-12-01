package vm

import (
	"context"
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

func WithStatistics(ctx context.Context) (*Statistics, context.Context) {
	s := &Statistics{}
	return s, context.WithValue(ctx, statisticsKey, s)
}
