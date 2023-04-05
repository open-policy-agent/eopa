package decisionlogs

import (
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

func (factory) Validate(m *plugins.Manager, config []byte) (any, error) {
	c := Config{}
	err := util.Unmarshal(config, &c)
	if err != nil {
		return nil, err
	}
	type typ struct {
		Type string `json:"type"`
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

	// Outputs
	output := new(typ)
	if err := util.Unmarshal(c.Output, output); err != nil {
		return nil, err
	}
	switch output.Type { // TODO(sr): benefit from generics?
	case "http":
		c.outputHTTP = new(outputHTTPOpts)
		if err := util.Unmarshal(c.Output, c.outputHTTP); err != nil {
			return nil, err
		}
	case "service":
		service := new(outputServiceOpts)
		if err := util.Unmarshal(c.Output, service); err != nil {
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
		c.outputHTTP = new(outputHTTPOpts)
		c.outputHTTP.URL = fmt.Sprintf("%s/%s", cfg.URL, service.Resource)
		if sec := cfg.ResponseHeaderTimeoutSeconds; sec != nil {
			c.outputHTTP.Timeout = fmt.Sprintf("%ds", *sec)
		} else {
			c.outputHTTP.Timeout = "10s"
		}
		c.outputHTTP.Headers = cfg.Headers
		c.outputHTTP.Array = true
		c.outputHTTP.Compress = true

		if oauth2 := cfg.Credentials.OAuth2; oauth2 != nil {
			c.outputHTTP.OAuth2 = &httpAuthOAuth2{
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
			c.outputHTTP.TLS = &httpAuthTLS{
				Enabled:      true,
				Certificates: []certs{{Cert: string(cert), Key: string(key)}},
			}
			if cfg.TLS != nil {
				caCert, err := os.ReadFile(cfg.TLS.CACert)
				if err != nil {
					return nil, err
				}
				c.outputHTTP.TLS.RootCAs = string(caCert)
			}
		}
	case "console":
		c.outputConsole = new(outputConsoleOpts)
		if err := util.Unmarshal(c.Output, c.outputConsole); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown output type: %q", output.Type)
	}
	return c, nil
}
