package ekm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	vault "github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/api/auth/approle"
	"github.com/hashicorp/vault/api/auth/kubernetes"

	"github.com/open-policy-agent/opa/config"
	"github.com/open-policy-agent/opa/logging"

	"github.com/styrainc/enterprise-opa-private/internal/license"
)

const Name = "ekm"

type (
	Config struct {
		Vault *VaultConfig `json:"vault,omitempty"`
	}

	RoleAuth struct {
		RoleID   string `json:"role_id,omitempty"`
		SecretID string `json:"secret_id,omitempty"`
		Wrapped  bool   `json:"wrapped,omitempty"`
	}

	K8sAuth struct {
		ServiceToken string `json:"service_token,omitempty"`
	}

	VaultConfig struct {
		URL        string    `json:"url"`
		Insecure   bool      `json:"insecure"`
		RootCA     string    `json:"rootca"`
		AccessType string    `json:"access_type"`
		Token      string    `json:"token,omitempty"`
		TokenFile  string    `json:"token_file,omitempty"`
		AppRole    *RoleAuth `json:"approle,omitempty"`
		K8sService *K8sAuth  `json:"kubernetes,omitempty"`

		// override mappings
		License  map[string]string         `json:"license,omitempty"`
		Keys     map[string]string         `json:"keys,omitempty"`
		Services map[string]string         `json:"services,omitempty"`
		HTTPSend map[string]map[string]any `json:"httpsend,omitempty"`
	}

	EKM struct {
		checker license.Checker
		lparams *license.LicenseParams
		logger  logging.Logger
	}
)

func NewEKM(c license.Checker, lparams *license.LicenseParams) *EKM {
	return &EKM{checker: c, lparams: lparams}
}

func (e *EKM) SetLogger(l logging.Logger) {
	e.logger = l
}

func (e *EKM) OnConfigDiscovery(ctx context.Context, conf *config.Config) (*config.Config, error) {
	return e.onConfig(ctx, conf, true)
}

func (e *EKM) OnConfig(ctx context.Context, conf *config.Config) (*config.Config, error) {
	return e.onConfig(ctx, conf, conf.Discovery == nil)
}

func (e *EKM) onConfig(ctx context.Context, conf *config.Config, validateLicense bool) (*config.Config, error) {
	if validateLicense {
		defer func() {
			// TODO(sr): e2e test EKM license interactions
			if e.checker != nil && e.checker.Strict() {
				e.checker.ValidateLicenseOrDie(e.lparams) // calls os.Exit if invalid
			}
		}()
	}

	e.logger.Debug("Process EKM")

	if conf.Extra["ekm"] == nil {
		return conf, nil
	}

	var vc Config
	err := unmarshalAndValidate(conf.Extra["ekm"], &vc)
	if err != nil {
		return conf, err
	}

	if vc.Vault == nil {
		return conf, nil
	}

	vaultCfg := vault.DefaultConfig()
	vaultCfg.Address = vc.Vault.URL

	parsedURL, err := url.Parse(vc.Vault.URL)
	if err != nil {
		return conf, fmt.Errorf("invalid URL %v, %w", vc.Vault.URL, err)
	}

	if parsedURL.Scheme == "https" {
		tlsconfig := &vault.TLSConfig{}
		if len(vc.Vault.RootCA) != 0 {
			tlsconfig.CACertBytes = []byte(vc.Vault.RootCA)
		}
		if vc.Vault.Insecure {
			tlsconfig.Insecure = vc.Vault.Insecure
			os.Unsetenv("VAULT_SKIP_VERIFY")
		}
		vaultCfg.ConfigureTLS(tlsconfig)
	}

	vaultClient, err := vault.NewClient(vaultCfg)
	if err != nil {
		err := fmt.Errorf("ProcessEKM vault failure: %w", err)
		e.logger.Error("%v", err)
		return nil, err
	}

	// use kubernetes
	switch vc.Vault.AccessType {
	case "kubernetes":
		if vc.Vault.K8sService == nil {
			return nil, fmt.Errorf("unable to initialize kubernetes auth method")
		}

		k8sAuth, err := kubernetes.NewKubernetesAuth(
			"dev-role-k8s",
			kubernetes.WithServiceAccountTokenPath(vc.Vault.K8sService.ServiceToken),
		)
		if err != nil {
			return nil, fmt.Errorf("unable to initialize Kubernetes auth method: %w", err)
		}

		authInfo, err := vaultClient.Auth().Login(ctx, k8sAuth)
		if err != nil {
			return nil, fmt.Errorf("unable to log in with Kubernetes auth: %w", err)
		}
		if authInfo == nil {
			return nil, fmt.Errorf("no auth info was returned after login")
		}

	case "approle":
		if vc.Vault.AppRole == nil {
			return nil, fmt.Errorf("unable to initialize AppRole auth method")
		}

		s := &approle.SecretID{FromString: vc.Vault.AppRole.SecretID}

		// wrapped secrets: https://developer.hashicorp.com/vault/tutorials/auth-methods/approle-best-practices#approle-response-wrapping
		var opts []approle.LoginOption
		if vc.Vault.AppRole.Wrapped {
			opts = append(opts, approle.WithWrappingToken())
		}

		appRoleAuth, err := approle.NewAppRoleAuth(
			vc.Vault.AppRole.RoleID,
			s,
			opts...,
		)
		if err != nil {
			return nil, fmt.Errorf("unable to initialize AppRole auth method: %w", err)
		}

		authInfo, err := vaultClient.Auth().Login(ctx, appRoleAuth)
		if err != nil {
			return nil, fmt.Errorf("unable to login to AppRole auth method: %w", err)
		}
		if authInfo == nil {
			return nil, fmt.Errorf("no auth info was returned after login")
		}

	case "token":
		// set token or use environment variable VAULT_TOKEN
		if vc.Vault.Token != "" {
			vaultClient.SetToken(vc.Vault.Token)
		} else if vc.Vault.TokenFile != "" {
			ltoken, err := readFile(vc.Vault.TokenFile)
			if err != nil {
				return nil, fmt.Errorf("invalid token file: %w", err)
			}
			vaultClient.SetToken(ltoken)
		} else {
			if os.Getenv("VAULT_TOKEN") == "" {
				return nil, fmt.Errorf("no token specified")
			}
		}

	default:
		return nil, fmt.Errorf("invalid accesstype: %s", vc.Vault.AccessType)
	}

	vlogical := vaultClient.Logical()

	if e.checker != nil && e.lparams != nil && vc.Vault.License != nil {
		if tokenKey, ok := vc.Vault.License["token"]; ok {
			value, err := lookupKey(vlogical, tokenKey)
			if err == nil {
				e.lparams.Source = license.SourceOverride
				e.lparams.Token = value // override token
				e.lparams.Key = ""
			} else {
				e.logger.Debug("lookupKey failure %v", err)
			}
		} else {
			if keyKey, ok := vc.Vault.License["key"]; ok {
				value, err := lookupKey(vlogical, keyKey)
				if err == nil {
					e.lparams.Source = license.SourceOverride
					e.lparams.Key = value // override key
					e.lparams.Token = ""
				} else {
					e.logger.Debug("lookupKey failure %v", err)
				}
			}
		}
	}

	if len(vc.Vault.Services) > 0 {
		conf.Services, err = e.filter(vlogical, conf.Services, &vc.Vault.Services)
		if err != nil {
			return nil, err
		}
	}

	if len(vc.Vault.Keys) > 0 {
		conf.Keys, err = e.filter(vlogical, conf.Keys, &vc.Vault.Keys)
		if err != nil {
			return nil, err
		}
	}
	if len(vc.Vault.HTTPSend) > 0 {
		send := make(map[url.URL]map[string]any)
		for k1, v1 := range vc.Vault.HTTPSend {
			u, err := url.Parse(k1)
			if err != nil {
				e.logger.Error("url.Parse %v: failed %v", k1, err)
				continue
			}
			if u.Path != "" && u.Path != "/" {
				e.logger.Warn("unexpected url path ignored: %v", u.String())
			}
			url := url.URL{Scheme: u.Scheme, Host: u.Host}
			send[url] = make(map[string]any)
			for k2, v2 := range v1 {
				send[url][k2] = v2
			}
		}
		registerHTTPSend(e.logger, send, vlogical)
	} else {
		resetHTTPSend()
	}
	return conf, nil
}

func readFile(file string) (string, error) {
	dat, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("invalid token file %v: %w", file, err)
	}
	s := strings.TrimSpace(string(dat))
	if len(s) == 0 {
		return "", fmt.Errorf("invalid token file %v", file)
	}
	return s, nil
}

func lookupString(v any, field string) (string, error) {
	switch v := v.(type) {
	case string:
		return v, nil
	case fmt.Stringer:
		return v.String(), nil
	}
	return "", fmt.Errorf("invalid string type %T, %v", v, field)
}

func lookupKey(vlogical *vault.Logical, p string) (string, error) {
	p = strings.TrimSpace(p)
	arr := strings.Split(p, ":")
	if len(arr) != 2 {
		return "", fmt.Errorf("invalid path:field specification: %v", p)
	}

	path := arr[0]
	field := arr[1]

	secret, err := vlogical.Read(path)
	if err != nil {
		return "", fmt.Errorf("key %v not found: %w", p, err)
	}
	if secret == nil {
		return "", fmt.Errorf("key %v not found", p)
	}

	f := strings.Split(field, "/")
	if len(f) > 2 || len(f) < 1 {
		return "", fmt.Errorf("invalid field specification: %v", field)
	}

	res, ok := secret.Data[f[0]]
	if !ok {
		return "", fmt.Errorf("field not found: %v", field)
	}
	if m, ok := res.(map[string]any); ok {
		if len(f) == 1 {
			return "", fmt.Errorf("invalid field, missing section: %v", field)
		}
		return lookupString(m[f[1]], field)
	}
	if len(f) != 1 {
		return "", fmt.Errorf("invalid field, extra section: %v", field)
	}
	return lookupString(res, field)
}

func unmarshalAndValidate(config []byte, c *Config) error {
	err := json.Unmarshal(config, c)
	if err != nil {
		return fmt.Errorf("unmarshalAndValidate failure: %w", err)
	}
	if c.Vault.URL == "" {
		s := os.Getenv("VAULT_ADDR")
		if s == "" {
			return fmt.Errorf("need at least one address to connect to vault")
		}
		c.Vault.URL = s
	}
	if _, err := url.Parse(c.Vault.URL); err != nil {
		return fmt.Errorf("invalid url: %v", c.Vault.URL)
	}
	return nil
}

func (e *EKM) filter(vlogical *vault.Logical, input json.RawMessage, overrides *map[string]string) (json.RawMessage, error) {
	if len(*overrides) > 0 {
		data := make(map[string]any)
		if len(input) > 0 {
			err := json.Unmarshal(input, &data)
			if err != nil {
				return nil, fmt.Errorf("filter unmarshal failure: %w", err)
			}
		}

		srvs := e.convertOverrideToMap(vlogical, overrides)
		c := mergeValues(data, srvs)

		s, err := json.Marshal(c)
		if err != nil {
			return nil, fmt.Errorf("filter marshal failure: %w", err)
		}
		return s, nil
	}
	return input, nil
}

func (e *EKM) convertOverrideToMap(vlogical *vault.Logical, input *map[string]string) map[string]any {
	res := make(map[string]any)
	for k, v1 := range *input {
		arr := strings.Split(k, ".")
		secret, err := lookupKey(vlogical, v1)
		if err != nil {
			e.logger.Error("%v", err)
			continue
		}
		if secret == "" {
			continue
		}
		r := &res
		for i, v2 := range arr {
			if i < len(arr)-1 {
				if tmp, ok := (*r)[v2]; ok {
					if t, ok := tmp.(map[string]any); ok {
						r = &t
					} else {
						e.logger.Error("invalid type %T, skipping", tmp, k)
						break
					}
				} else {
					tmp := make(map[string]any)
					(*r)[v2] = tmp
					r = &tmp
				}
			} else {
				(*r)[v2] = secret
			}
		}
	}
	return res
}

// mergeValues will merge source and destination map, preferring values from the source map
func mergeValues(dest map[string]any, src map[string]any) map[string]any {
	for k, v := range src {
		// If the key doesn't exist already, then just set the key to that value
		if _, exists := dest[k]; !exists {
			dest[k] = v
			continue
		}
		nextMap, ok := v.(map[string]any)
		// If it isn't another map, overwrite the value
		if !ok {
			dest[k] = v
			continue
		}
		// Edge case: If the key exists in the destination, but isn't a map
		destMap, isMap := dest[k].(map[string]any)
		// If the source map has a map for this key, prefer it
		if !isMap {
			dest[k] = v
			continue
		}
		// If we got to this point, it is a map in both, so merge them
		dest[k] = mergeValues(destMap, nextMap)
	}
	return dest
}
