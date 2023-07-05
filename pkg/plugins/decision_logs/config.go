package decisionlogs

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type Config struct {
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

type additionalResources interface {
	Resources() *resources
}

type resources struct {
	RateLimit []map[string]any `json:"rate_limit_resources,omitempty"`
}

func (rs *resources) Merge(r *resources) *resources {
	if r == nil {
		return rs
	}

	rs.RateLimit = append(rs.RateLimit, r.RateLimit...)

	return rs
}

func ResourceKey(input []byte) string {
	hash := md5.Sum(input)
	return hex.EncodeToString(hash[:])
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

func (x outputs) Resources() *resources {
	var resourceList *resources
	for _, y := range x {
		if out, ok := y.(additionalResources); ok {
			ar := out.Resources()
			if ar != nil {
				resourceList = ar.Merge(resourceList)
			}
		}
	}

	return resourceList
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

	OAuth2    *httpAuthOAuth2 `json:"oauth2,omitempty"`
	TLS       *sinkAuthTLS    `json:"tls,omitempty"` // TODO(sr): figure out if we want to expose this as-is, or wrap (or reference files instead of raw certs)
	Batching  *batchOpts      `json:"batching,omitempty"`
	Retry     *retryOpts      `json:"retry,omitempty"`
	RateLimit *rateLimitOpts  `json:"rate_limit,omitempty"`
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
type rateLimitOpts struct {
	Count    int    `json:"count"`
	Interval string `json:"interval"`
}

func (r *rateLimitOpts) Resources(label string) *resources {
	if label == "" {
		return nil
	}
	resource := map[string]any{}
	if r.Count != 0 {
		resource["count"] = r.Count
	}
	if r.Interval != "" {
		resource["interval"] = r.Interval
	}
	return &resources{
		RateLimit: []map[string]any{
			{
				"label": label,
				"local": resource,
			},
		},
	}
}

func (s *outputHTTPOpts) Benthos() map[string]any {
	m := map[string]any{
		"url": s.URL,
	}
	if s.Timeout != "" {
		m["timeout"] = s.Timeout
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
	if s.RateLimit != nil {
		m["rate_limit"] = ResourceKey([]byte(s.URL))
	}
	if r := s.Retry; r != nil {
		for key, value := range r.Benthos() {
			m[key] = value
		}
	}
	return map[string]any{
		"drop_on": map[string]any{
			"error":  true,
			"output": map[string]any{"http_client": m},
		},
	}
}

func (s *outputHTTPOpts) Resources() *resources {
	if s.RateLimit != nil {
		return s.RateLimit.Resources(ResourceKey([]byte(s.URL)))
	}
	return nil
}

type outputConsoleOpts struct{}

func (*outputConsoleOpts) Benthos() map[string]any {
	return map[string]any{"stdout": struct{}{}}
}

type outputKafkaOpts struct {
	URLs    []string   `json:"urls"`
	Topic   string     `json:"topic"`
	Timeout string     `json:"timeout,omitempty"`
	TLS     *tlsOpts   `json:"tls,omitempty"`
	SASL    []saslOpts `json:"sasl,omitempty"`

	Batching *batchOpts `json:"batching,omitempty"`

	// NOTE(sr): There are just too many configurables if we care about all of them
	// at once. Let's introduce batching when someone needs it.

	tls *sinkAuthTLS
}

type saslOpts struct {
	Mechanism string `json:"mechanism"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
}

func (s *saslOpts) Benthos() map[string]any {
	c := map[string]any{
		"mechanism": strings.ToUpper(s.Mechanism),
		"username":  s.Username,
		"password":  s.Password,
	}

	return c
}

type batchOpts struct {
	Count    int    `json:"at_count,omitempty"`
	Bytes    int    `json:"at_bytes,omitempty"`
	Period   string `json:"at_period,omitempty"`
	Array    bool   `json:"array"`    // send batches as arrays of json blobs (default: lines of json blobs)
	Compress bool   `json:"compress"` // TODO(sr): decide if want to expose the algorithm (gzip vs lz4, snappy, ..)

	unprocessed bool // don't do any processing
}

func (b *batchOpts) Benthos() map[string]any {
	m := map[string]any{
		"count":     b.Count,
		"byte_size": b.Bytes,
		"period":    b.Period,
	}
	if b.unprocessed {
		return m
	}
	m["processors"] = b.Processors()
	return m
}

func (b *batchOpts) Processors() []map[string]any {
	processors := make([]map[string]any, 0, 2)
	if b.Array {
		processors = append(processors, map[string]any{"archive": map[string]any{"format": "json_array"}})
	} else {
		processors = append(processors, map[string]any{"archive": map[string]any{"format": "lines"}})
	}
	if b.Compress {
		processors = append(processors, map[string]any{"compress": map[string]any{"algorithm": "gzip"}})
	}

	return processors
}

type retryOpts struct {
	Period       string `json:"period,omitempty"`
	MaxAttempts  int    `json:"max_attempts,omitempty"`
	MaxBackoff   string `json:"max_backoff,omitempty"`
	BackoffOn    []int  `json:"backoff_on,omitempty"`
	DropOn       []int  `json:"drop_on,omitempty"`
	SuccessfulOn []int  `json:"successful_on,omitempty"`
}

func (r *retryOpts) Benthos() map[string]any {
	m := map[string]any{}
	if r.Period != "" {
		m["retry_period"] = r.Period
	}
	if r.MaxAttempts > 0 {
		m["retries"] = r.MaxAttempts
	}
	if r.MaxBackoff != "" {
		m["max_retry_backoff"] = r.MaxBackoff
	}
	if r.BackoffOn != nil {
		m["backoff_on"] = r.BackoffOn
	}
	if r.DropOn != nil {
		m["drop_on"] = r.DropOn
	}
	if r.SuccessfulOn != nil {
		m["successful_on"] = r.SuccessfulOn
	}
	return m
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
	if s.SASL != nil {
		saslConf := make([]map[string]any, 0, len(s.SASL))
		for _, sasl := range s.SASL {
			saslConf = append(saslConf, sasl.Benthos())
		}
		m["sasl"] = saslConf
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

type outputS3Opts struct {
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region"`
	ForcePath    bool   `json:"force_path"`
	Bucket       string `json:"bucket"`
	AccessKeyID  string `json:"access_key_id"`
	AccessSecret string `json:"access_secret"`
	// TODO(sr): support more automatic methods, ec2 role, kms etc

	TLS      *tlsOpts   `json:"tls,omitempty"`
	Batching *batchOpts `json:"batching,omitempty"`

	tls *sinkAuthTLS
}

var s3MetaMapping = `meta eopa_id = json("labels.id").from(0)
meta first_ts = json("timestamp").from(0).ts_parse("2006-01-02T15:04:05Z07:00").ts_unix()
meta last_ts = json("timestamp").from(-1).ts_parse("2006-01-02T15:04:05Z07:00").ts_unix()
`

func (s *outputS3Opts) Benthos() map[string]any {
	m := map[string]any{
		"bucket":       s.Bucket,
		"path":         `eopa=${!json("labels.id")}/ts=${!json("timestamp").ts_parse("2006-01-02T15:04:05Z07:00").ts_unix()}/decision_id=${!json("decision_id")}.json`,
		"content_type": "application/json",
	}
	if e := s.Endpoint; e != "" {
		m["endpoint"] = e
	}
	if r := s.Region; r != "" {
		m["region"] = r
	}
	if s.ForcePath {
		m["force_path_style_urls"] = true
	}
	if b := s.Batching; b != nil {
		bMap := b.Benthos()
		bProc := b.Processors()

		// Add mapping processor to pull out data from the first batch item for use in the naming scheme
		processors := make([]map[string]any, 1, len(bProc)+1)
		processors[0] = map[string]any{
			"mutation": s3MetaMapping,
		}
		processors = append(processors, bProc...)

		bMap["processors"] = processors
		m["batching"] = bMap
		objectPath := `eopa=${!@eopa_id}/first_ts=${!@first_ts}/last_ts=${!@last_ts}/batch_id=${!uuid_v4()}.json`
		if !b.Array {
			// Make the extension `.jsonl` in non-array mode indicating json lines format
			objectPath += "l"
		}
		if b.Compress {
			objectPath += ".gz"
		}
		m["path"] = objectPath
	}
	if s.tls != nil {
		m["tls"] = s.tls.Benthos()
	}
	m["credentials"] = map[string]any{
		"id":     s.AccessKeyID,
		"secret": s.AccessSecret,
	}
	return map[string]any{"aws_s3": m}
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
