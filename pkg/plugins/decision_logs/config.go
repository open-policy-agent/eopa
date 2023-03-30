package decisionlogs

import (
	"encoding/json"
	"fmt"
)

type Config struct {
	// Compatibility options
	DropDecision string `json:"drop_decision"`
	MaskDecision string `json:"mask_decision"`

	Output json.RawMessage `json:"output"`
	Buffer json.RawMessage `json:"buffer"`

	memoryBuffer *memBufferOpts
	diskBuffer   *diskBufferOpts

	outputHTTP    *outputHTTPOpts
	outputConsole *outputConsoleOpts
}

type memBufferOpts struct {
	MaxBytes    int    `json:"max_bytes"`    // Maximum buffer size (in bytes) to allow before applying backpressure upstream
	FlushCount  int    `json:"flush_count"`  // Number of messages at which the batch should be flushed. If 0 disables count based batching.
	FlushBytes  int    `json:"flush_bytes"`  // Amount of bytes at which the batch should be flushed. If 0 disables size based batching.
	FlushPeriod string `json:"flush_period"` // period in which an incomplete batch should be flushed regardless of its size (e.g. 1s)
}

const defaultMemoryMaxBytes = 524288000 // 500M

func (m *memBufferOpts) String() string {
	if m.FlushBytes > 0 || m.FlushCount > 0 || m.FlushPeriod != "" {
		return fmt.Sprintf(`memory:
  limit: %d
  batch_policy:
    enabled: true
    count: %d
    byte_size: %d
    period: %s`, m.MaxBytes, m.FlushCount, m.FlushBytes, m.FlushPeriod)
	}
	return fmt.Sprintf(`memory:
  limit: %d
`, m.MaxBytes)
}

type diskBufferOpts struct {
	Path string `json:"path"` // where to put on-disk sqlite buffer
}

func (d *diskBufferOpts) String() string {
	return fmt.Sprintf(`sqlite:
  path: "%s"`, d.Path)
}

// outputServiceOpts is transformed into an outputHTTPOpts instance
type outputServiceOpts struct {
	Service  string `json:"service"`
	Resource string `json:"resource"`
	// TODO(sr): add retry
}

type outputHTTPOpts struct {
	URL      string            `json:"url"`
	Timeout  string            `json:"timeout,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Array    bool              `json:"array"` // send batches as arrays of json blobs (default: lines of json blobs)
	Compress bool              `json:"compress"`
	// TODO(sr): add retry, batching
}

func (s *outputHTTPOpts) String() string {
	processors := make([]map[string]any, 0, 2)
	if s.Array {
		processors = append(processors, map[string]any{"archive": map[string]any{"format": "json_array"}})
	} else {
		processors = append(processors, map[string]any{"archive": map[string]any{"format": "lines"}})
	}
	if s.Compress {
		processors = append(processors, map[string]any{"compress": map[string]any{"algorithm": "gzip"}})
	}

	j, err := json.Marshal(map[string]any{"http_client": map[string]any{
		"url":     s.URL,
		"timeout": s.Timeout,
		"batching": map[string]any{
			"period":     "10ms",
			"processors": processors,
		},
	}})
	if err != nil {
		panic(err)
	}
	return string(j)
}

type outputConsoleOpts struct{}

func (*outputConsoleOpts) String() string {
	return `stdout: {}`
}
