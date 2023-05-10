package decisionlogs

import (
	"bytes"
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

	outputs outputs
}

type output interface {
	Benthos() map[string]any
}

type extraProcessing interface {
	Extra() []map[string]any
}

type outputs []output

func (x outputs) Benthos() map[string]any {
	outputs := make([]map[string]any, len(x))
	for i, y := range x {
		outputs[i] = y.Benthos()
		if ep, ok := y.(extraProcessing); ok {
			outputs[i]["processors"] = ep.Extra()
		}
	}
	return map[string]any{
		"broker": map[string]any{
			"outputs": outputs,
		},
	}
}

// NOTE(sr): Maybe batching at the sink is good enough and batching here only complicates
// things. Let's reconsider later.
type memBufferOpts struct {
	MaxBytes      int    `json:"max_bytes"`       // Maximum buffer size (in bytes) to allow before applying backpressure upstream
	FlushAtCount  int    `json:"flush_at_count"`  // Number of messages at which the batch should be flushed. If 0 disables count based batching.
	FlushAtBytes  int    `json:"flush_at_bytes"`  // Amount of bytes at which the batch should be flushed. If 0 disables size based batching.
	FlushAtPeriod string `json:"flush_at_period"` // period in which an incomplete batch should be flushed regardless of its size (e.g. 1s)
}

const defaultMemoryMaxBytes = 524288000 // 500M

func (m *memBufferOpts) String() string {
	if m.FlushAtBytes > 0 || m.FlushAtCount > 0 || m.FlushAtPeriod != "" {
		return fmt.Sprintf(`memory:
  limit: %d
  batch_policy:
    enabled: true
    count: %d
    byte_size: %d
    period: %s`, m.MaxBytes, m.FlushAtCount, m.FlushAtBytes, m.FlushAtPeriod)
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
	URL     string            `json:"url"`
	Timeout string            `json:"timeout,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	OAuth2   *httpAuthOAuth2 `json:"oauth2,omitempty"`
	TLS      *sinkAuthTLS    `json:"tls,omitempty"` // TODO(sr): figure out if we want to expose this as-is, or wrap (or reference files instead of raw certs)
	Batching *batchOpts      `json:"batching,omitempty"`

	// TODO(sr): add retry
}

type httpAuthOAuth2 struct {
	Enabled      bool     `json:"enabled"`
	ClientKey    string   `json:"client_key"`
	ClientSecret string   `json:"client_secret"`
	TokenURL     string   `json:"token_url"`
	Scopes       []string `json:"scopes,omitempty"`
}

type sinkAuthTLS struct {
	Enabled      bool    `json:"enabled"`
	SkipVerify   bool    `json:"skip_cert_verify"`
	Certificates []certs `json:"client_certs"`
	RootCAs      string  `json:"root_cas,omitempty"`
}

func (t *sinkAuthTLS) Benthos() map[string]any {
	certs := make([]map[string]any, len(t.Certificates))
	for i := range certs {
		certs[i] = map[string]any{
			"key":  t.Certificates[i].Key,
			"cert": t.Certificates[i].Cert,
		}
	}
	return map[string]any{
		"enabled":          true,
		"skip_cert_verify": t.SkipVerify,
		"client_certs":     certs,
		"root_cas":         t.RootCAs,
	}
}

type certs struct {
	Key  string `json:"key"`
	Cert string `json:"cert"`
}

func (s *outputHTTPOpts) Benthos() map[string]any {
	m := map[string]any{
		"url":     s.URL,
		"timeout": s.Timeout,
	}
	if s.OAuth2 != nil {
		m["oauth2"] = s.OAuth2
	}
	if s.TLS != nil {
		m["tls"] = s.TLS
	}
	if b := s.Batching; b != nil {
		m["batching"] = b.Benthos()
	}
	return map[string]any{"http_client": m}
}

type outputConsoleOpts struct{}

func (*outputConsoleOpts) Benthos() map[string]any {
	return map[string]any{"stdout": struct{}{}}
}

type outputKafkaOpts struct {
	URLs    []string `json:"urls"`
	Topic   string   `json:"topic"`
	Timeout string   `json:"timeout,omitempty"`
	TLS     *tlsOpts `json:"tls,omitempty"`

	Batching *batchOpts `json:"batching,omitempty"`

	// NOTE(sr): There are just too many configurables if we care about all of them
	// at once. Let's introduce batching when someone needs it.

	tls *sinkAuthTLS
	// TODO(sr): SASL
}

type batchOpts struct {
	Count    int    `json:"at_count,omitempty"`
	Bytes    int    `json:"at_bytes,omitempty"`
	Period   string `json:"at_period,omitempty"`
	Array    bool   `json:"array"`    // send batches as arrays of json blobs (default: lines of json blobs)
	Compress bool   `json:"compress"` // TODO(sr): decide if want to expose the algorithm (gzip vs lz4, snappy, ..)
}

func (b *batchOpts) Benthos() map[string]any {
	processors := make([]map[string]any, 0, 2)
	if b.Array {
		processors = append(processors, map[string]any{"archive": map[string]any{"format": "json_array"}})
	} else {
		processors = append(processors, map[string]any{"archive": map[string]any{"format": "lines"}})
	}
	if b.Compress {
		processors = append(processors, map[string]any{"compress": map[string]any{"algorithm": "gzip"}})
	}
	return map[string]any{
		"count":      b.Count,
		"byte_size":  b.Bytes,
		"period":     b.Period,
		"processors": processors,
	}
}

type tlsOpts struct {
	Cert       string `json:"cert"`
	PrivateKey string `json:"private_key"`
	CACert     string `json:"ca_cert"`
	SkipVerify bool   `json:"skip_cert_verify"`
}

func (s *outputKafkaOpts) Benthos() map[string]any {
	m := map[string]any{
		"seed_brokers": s.URLs,
		"topic":        s.Topic,
	}
	if s.Timeout != "" {
		m["timeout"] = s.Timeout
	}
	if s.tls != nil {
		m["tls"] = s.tls.Benthos()
	}
	if b := s.Batching; b != nil {
		m["batching"] = b.Benthos()
	}
	return map[string]any{"kafka_franz": m}
}

// outputSplunkOpts is transformed into http_client benthos output,
// but not via outputHTTPOpts -- we need hardcoded transformers and headers
type outputSplunkOpts struct {
	URL   string `json:"url"`
	Token string `json:"token"` // "event collector token"

	TLS      *tlsOpts   `json:"tls,omitempty"`
	Batching *batchOpts `json:"batching,omitempty"`

	// TODO(sr): rate limit, retries, max_in_flight
	tls *sinkAuthTLS
}

func (s *outputSplunkOpts) Benthos() map[string]any {
	hdrs := map[string]any{
		"Content-Type":  "application/json",
		"Authorization": "Splunk " + s.Token,
	}
	m := map[string]any{
		"url":  s.URL,
		"verb": "POST",
	}
	if b := s.Batching; b != nil {
		m["batching"] = b.Benthos()
		if b.Compress {
			hdrs["Content-Encoding"] = "gzip"
		}
	}
	if s.tls != nil {
		m["tls"] = s.tls.Benthos()
	}
	m["headers"] = hdrs
	return map[string]any{"http_client": m}
}

var _ extraProcessing = (*outputSplunkOpts)(nil)

// Splunk expects payloads that at least have "time" (epoch) and "event" set
func (s *outputSplunkOpts) Extra() []map[string]any {
	return []map[string]any{
		{"mapping": `{ "event": this, "time": timestamp_unix() }`},
	}
}

// This output is for experimentation and not part of the public feature set.
// NOTE(sr): Because OPA's config-env-var replacement mechanism messes with
// Benthos' interpolation functions, we'll replace any "%" by "$". So to use
// and interpolation function like
//
//	path: '${json("field")}'
//
// you'll have to put this into your config:
//
//	path: '%{json("field")}'
type outputExpOpts struct {
	Config json.RawMessage `json:"config"`
}

func (s *outputExpOpts) Benthos() map[string]any {
	m := map[string]any{}
	if err := json.NewDecoder(bytes.NewReader(bytes.ReplaceAll(s.Config, []byte("%"), []byte("$")))).Decode(&m); err != nil {
		panic(err)
	}
	return m
}
