package vm

import (
	"context"
	"time"
)

type (
	contextKey string
)

var statisticsKey = contextKey("statistics")

type (
	Statistics struct {
		BundleReadBytes             int64         `json:"bundle_read_bytes"`
		BundleReadOps               int64         `json:"bundle_read_ops"`
		BundleReadTime              time.Duration `json:"bundle_read_time"`
		ConfigReadBytes             int64         `json:"config_read_bytes"`
		ConfigReadCacheMisses       int64         `json:"config_read_cache_misses"`
		ConfigReadDecodeCacheMisses int64         `json:"config_read_decode_cache_misses"`
		ConfigReadOps               int64         `json:"config_read_ops"`
		ConfigReadTime              time.Duration `json:"config_read_time"`
		DataReadBytes               int64         `json:"data_read_bytes"`
		DataReadCacheMisses         int64         `json:"data_read_cache_misses"`
		DataReadOps                 int64         `json:"data_read_ops"`
		DataReadTime                time.Duration `json:"data_read_time"`
		EvalInstructions            int64         `json:"eval_instructions"`
		EvalOps                     int64         `json:"eval_ops"`
		EvalTime                    time.Duration `json:"eval_time"`
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
