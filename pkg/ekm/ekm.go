package ekm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/Jeffail/gabs/v2"
	vault "github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/api/auth/approle"
	"github.com/hashicorp/vault/api/auth/kubernetes"

	"github.com/open-policy-agent/opa/v1/config"
	"github.com/open-policy-agent/opa/v1/logging"

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
		checker *license.Checker
		logger  logging.Logger
	}
)

func NewEKM(c *license.Checker) *EKM {
	return &EKM{checker: c}
}

func (e *EKM) SetLogger(l logging.Logger) {
	e.logger = l
}

func (e *EKM) OnConfigDiscovery(ctx context.Context, conf *config.Config) (*config.Config, error) {
	return e.OnConfig(ctx, conf)
}

func (e *EKM) OnConfig(ctx context.Context, conf *config.Config) (*config.Config, error) {
	c, lparams, err := e.onConfig(ctx, conf)
	if err != nil {
		return nil, err
	}
	if e.checker != nil && lparams != nil {
		e.checker.UpdateLicenseParams(lparams)
	}
	return c, nil
}

func (e *EKM) onConfig(ctx context.Context, conf *config.Config) (*config.Config, *license.LicenseParams, error) {
	e.logger.Debug("Process EKM")

	if conf.Extra["ekm"] == nil {
		return conf, nil, nil
	}

	var vc Config
	err := unmarshalAndValidate(conf.Extra["ekm"], &vc)
	if err != nil {
		return nil, nil, err
	}

	if vc.Vault == nil {
		return conf, nil, nil
	}

	vaultCfg := vault.DefaultConfig()
	vaultCfg.Address = vc.Vault.URL

	parsedURL, err := url.Parse(vc.Vault.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid URL %v, %w", vc.Vault.URL, err)
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
		return nil, nil, fmt.Errorf("EKM vault failure: %w", err)
	}

	// use kubernetes
	switch vc.Vault.AccessType {
	case "kubernetes":
		if vc.Vault.K8sService == nil {
			return nil, nil, fmt.Errorf("kubernetes auth method")
		}

		k8sAuth, err := kubernetes.NewKubernetesAuth(
			"dev-role-k8s",
			kubernetes.WithServiceAccountTokenPath(vc.Vault.K8sService.ServiceToken),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("kubernetes auth method: %w", err)
		}

		authInfo, err := vaultClient.Auth().Login(ctx, k8sAuth)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to log in with Kubernetes auth: %w", err)
		}
		if authInfo == nil {
			return nil, nil, fmt.Errorf("no auth info was returned after login")
		}

	case "approle":
		if vc.Vault.AppRole == nil {
			return nil, nil, fmt.Errorf("AppRole auth method not configured")
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
			return nil, nil, fmt.Errorf("AppRole auth method: %w", err)
		}

		authInfo, err := vaultClient.Auth().Login(ctx, appRoleAuth)
		if err != nil {
			return nil, nil, fmt.Errorf("AppRole auth method, login: %w", err)
		}
		if authInfo == nil {
			return nil, nil, fmt.Errorf("AppRole auth method, login: no auth info returned")
		}

	case "token":
		// set token or use environment variable VAULT_TOKEN
		if vc.Vault.Token != "" {
			vaultClient.SetToken(vc.Vault.Token)
		} else if vc.Vault.TokenFile != "" {
			ltoken, err := readFile(vc.Vault.TokenFile)
			if err != nil {
				return nil, nil, fmt.Errorf("token file: %w", err)
			}
			vaultClient.SetToken(ltoken)
		} else {
			if os.Getenv("VAULT_TOKEN") == "" {
				return nil, nil, fmt.Errorf("no token specified")
			}
		}

	default:
		return nil, nil, fmt.Errorf(`invalid accesstype: %s (must be one of "kubernetes", "approle", "token")`, vc.Vault.AccessType)
	}

	vlogical := vaultClient.Logical()

	var lparams *license.LicenseParams
	if vc.Vault.License != nil { // attempt to take license params from vault
		if tokenKey, ok := vc.Vault.License["token"]; ok {
			value, err := lookupKey(vlogical, tokenKey)
			if err == nil {
				lparams = &license.LicenseParams{
					Source: license.SourceOverride,
					Token:  value, // override token
				}
			} else {
				e.logger.Debug("lookupKey failure %v", err)
			}
		} else {
			if keyKey, ok := vc.Vault.License["key"]; ok {
				value, err := lookupKey(vlogical, keyKey)
				if err == nil {
					lparams = &license.LicenseParams{
						Source: license.SourceOverride,
						Key:    value, // override key
					}
				} else {
					e.logger.Debug("lookupKey failure %v", err)
				}
			}
		}
	}

	if len(vc.Vault.Services) > 0 {
		conf.Services, err = e.filter(vlogical, conf.Services, &vc.Vault.Services)
		if err != nil {
			return nil, nil, err
		}
	}

	if len(vc.Vault.Keys) > 0 {
		conf.Keys, err = e.filter(vlogical, conf.Keys, &vc.Vault.Keys)
		if err != nil {
			return nil, nil, err
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

	// deal with v2 replacements
	conf, err = replaceV2(vlogical, conf)
	if err != nil {
		return nil, nil, err
	}
	return conf, lparams, nil
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
	if len(*overrides) == 0 {
		return input, nil
	}
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

func replaceV2(vc *vault.Logical, conf *config.Config) (*config.Config, error) {
	var err error
	// services
	if len(conf.Services) > 0 {
		conf.Services, err = replaceRaw(conf.Services, vc)
		if err != nil {
			return nil, err
		}
	}

	// keys
	if len(conf.Keys) > 0 {
		conf.Keys, err = replaceRaw(conf.Keys, vc)
		if err != nil {
			return nil, err
		}
	}

	// plugins
	for p, pc := range conf.Plugins {
		conf.Plugins[p], err = replaceRaw(pc, vc)
		if err != nil {
			return nil, err
		}
	}

	return conf, nil
}

func replaceRaw(j json.RawMessage, vc *vault.Logical) (json.RawMessage, error) {
	m := map[string]any{}
	if err := json.Unmarshal(j, &m); err != nil {
		return nil, err
	}
	r := gabs.Wrap(m)
	if err := replaceLeaves(r, vc); err != nil {
		return nil, err
	}
	return r.Bytes(), nil
}

func replaceLeaves(m *gabs.Container, vc *vault.Logical) error {
	// is it a map?
	if cm := m.ChildrenMap(); len(cm) > 0 {
		for k, c := range cm {
			if s, ok := c.Data().(string); ok {
				n, err := replaceString(s, vc)
				if err != nil {
					return err
				}
				m.Set(n, k)
			}
			if err := replaceLeaves(c, vc); err != nil {
				return err
			}
		}
		return nil
	}

	// NOTE(sr): Children() also covers objects, but we'd not be able
	// to retrieve their keys. We need them, so we use ChildrenMap()
	// above. If it's an object, we'll never reach this part of the
	// code.
	for i, c := range m.Children() { // it's an array (or not, in which case it's ignored)
		if s, ok := c.Data().(string); ok {
			n, err := replaceString(s, vc)
			if err != nil {
				return err
			}
			m.SetIndex(n, i)
		}
		if err := replaceLeaves(c, vc); err != nil {
			return err
		}
	}

	return nil
}

var fullReplacement = regexp.MustCompile(`^\${vault\(([^\)]+)\)}$`)
var substringReplacement = regexp.MustCompile(`\${vault\(([^\)]+)\)}`)

func replaceString(v string, vc *vault.Logical) (any, error) {
	// case a: value replacement, like {"auth": "${vault.get(...)}"} => replace entire value with any (for now only strings, might be more)
	// NOTE(sr): This wouldn't have to be different from case (b) if it were only for strings
	// but I think it would be nice to extend this to pull whole objects from vault later.
	if m := fullReplacement.FindStringSubmatch(v); len(m) == 2 {
		return lookupKey(vc, m[1])
	}

	// case b: substring, like "Bearer ${vault.get(...)}" => insert substring
	var err error
	n := substringReplacement.ReplaceAllStringFunc(v, func(m string) string {
		if err != nil {
			return ""
		}
		vaultKey := m[len("${vault(") : len(m)-len(")}")]
		m, err = lookupKey(vc, vaultKey)
		return m
	})
	return n, err
}
