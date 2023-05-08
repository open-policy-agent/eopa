package decisionlogs

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/util"
)

type factory struct{}

func Factory() plugins.Factory {
	return &factory{}
}

func (factory) New(m *plugins.Manager, config any) plugins.Plugin {
	m.UpdatePluginStatus(Name, &plugins.Status{State: plugins.StateNotReady})

	return &Logger{
		manager: m,
		config:  config.(Config),
	}
}

type typ struct {
	Type string `json:"type"`
}

func (factory) Validate(m *plugins.Manager, config []byte) (any, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}

	// Defaults
	if c.DropDecision == "" {
		c.DropDecision = "/system/log/drop"
	}
	if c.MaskDecision == "" {
		c.MaskDecision = "/system/log/mask"
	}

	// Buffers
	buffer := new(typ)
	if err := util.Unmarshal(c.Buffer, buffer); err != nil {
		return nil, err
	}
	switch buffer.Type {
	case "memory", "":
		c.memoryBuffer = new(memBufferOpts)
		if err := util.Unmarshal(c.Buffer, c.memoryBuffer); err != nil {
			return nil, err
		}
		if c.memoryBuffer.MaxBytes == 0 {
			c.memoryBuffer.MaxBytes = defaultMemoryMaxBytes
		}
	case "unbuffered": // no config
	case "disk":
		c.diskBuffer = new(diskBufferOpts)
		if err := util.Unmarshal(c.Buffer, c.diskBuffer); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown buffer type: %q", buffer.Type)
	}

	// Outputs, one:
	out := new(typ)
	if err := util.Unmarshal(c.Output, out); err == nil && out.Type != "" {
		out, err := outputFromRaw(m, c.Output)
		if err != nil {
			return nil, err
		}
		c.outputs = []output{out}
		return c, nil
	}

	// Outputs, multiple:
	outputs := make([]json.RawMessage, 0)
	if err := util.Unmarshal(c.Output, &outputs); err != nil {
		return nil, err
	}
	for _, outputRaw := range outputs {
		output, err := outputFromRaw(m, outputRaw)
		if err != nil {
			return nil, err
		}
		c.outputs = append(c.outputs, output)
	}
	return c, nil
}

func outputFromRaw(m *plugins.Manager, outputRaw []byte) (output, error) {
	out := new(typ)
	if err := util.Unmarshal(outputRaw, out); err != nil {
		return nil, err
	}
	switch out.Type { // TODO(sr): benefit from generics?
	case "http":
		outputHTTP := new(outputHTTPOpts)
		if err := util.Unmarshal(outputRaw, outputHTTP); err != nil {
			return nil, err
		}
		return outputHTTP, nil
	case "service":
		service := new(outputServiceOpts)
		if err := util.Unmarshal(outputRaw, service); err != nil {
			return nil, err
		}
		if service.Resource == "" {
			service.Resource = "logs"
		}
		// m.Client(svc) returns a struct, not a pointer, and so is its `Config`, so we need
		// to check the Name field...
		cfg := m.Client(service.Service).Config()
		if cfg.Name == "" {
			return nil, fmt.Errorf("unknown service %q", service.Service)
		}
		outputHTTP := new(outputHTTPOpts)
		outputHTTP.URL = fmt.Sprintf("%s/%s", cfg.URL, service.Resource)
		if sec := cfg.ResponseHeaderTimeoutSeconds; sec != nil {
			outputHTTP.Timeout = fmt.Sprintf("%ds", *sec)
		} else {
			outputHTTP.Timeout = "10s"
		}
		outputHTTP.Headers = cfg.Headers
		outputHTTP.Batching = &batchOpts{
			Array:    true,
			Compress: true,
			Period:   "10ms", // TODO(sr): make this configurable for services
		}

		if oauth2 := cfg.Credentials.OAuth2; oauth2 != nil {
			outputHTTP.OAuth2 = &httpAuthOAuth2{
				Enabled:      true,
				ClientKey:    oauth2.ClientID,
				ClientSecret: oauth2.ClientSecret,
				TokenURL:     oauth2.TokenURL,
				Scopes:       oauth2.Scopes,
			}
		}
		if tls := cfg.Credentials.ClientTLS; tls != nil {
			cert, err := os.ReadFile(tls.Cert)
			if err != nil {
				return nil, err
			}
			key, err := os.ReadFile(tls.PrivateKey)
			if err != nil {
				return nil, err
			}
			outputHTTP.TLS = &sinkAuthTLS{
				Enabled:      true,
				Certificates: []certs{{Cert: string(cert), Key: string(key)}},
			}
			if cfg.TLS != nil {
				caCert, err := os.ReadFile(cfg.TLS.CACert)
				if err != nil {
					return nil, err
				}
				outputHTTP.TLS.RootCAs = string(caCert)
			}
		}
		return outputHTTP, nil
	case "console":
		outputConsole := new(outputConsoleOpts)
		if err := util.Unmarshal(outputRaw, outputConsole); err != nil {
			return nil, err
		}
		return outputConsole, nil
	case "kafka":
		outputKafka := new(outputKafkaOpts)
		if err := util.Unmarshal(outputRaw, outputKafka); err != nil {
			return nil, err
		}
		if tls := outputKafka.TLS; tls != nil {
			cert, err := os.ReadFile(tls.Cert)
			if err != nil {
				return nil, err
			}
			key, err := os.ReadFile(tls.PrivateKey)
			if err != nil {
				return nil, err
			}
			outputKafka.tls = &sinkAuthTLS{
				Enabled: true,
				Certificates: []certs{
					{Cert: string(cert), Key: string(key)},
				},
			}
			if ca := tls.CACert; ca != "" {
				caCert, err := os.ReadFile(ca)
				if err != nil {
					return nil, err
				}
				outputKafka.tls.RootCAs = string(caCert)
			}
			outputKafka.TLS = nil
		}
		return outputKafka, nil
	case "experimental":
		outputExp := new(outputExpOpts)
		if err := util.Unmarshal(outputRaw, outputExp); err != nil {
			return nil, err
		}
		return outputExp, nil
	default:
		return nil, fmt.Errorf("unknown output type: %q", out.Type)
	}
}
