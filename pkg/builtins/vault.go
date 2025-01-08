package builtins

import (
	"context"
	"sync"
	"time"

	vault "github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/api/auth/approle"
	"go.opentelemetry.io/otel"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
	"github.com/open-policy-agent/opa/v1/types"
)

const (
	vaultSendName = "vault.send"
	// vaultSendBuiltinCacheKey is the key in the builtin context cache that
	// points to the vault.send() specific intra-query cache resides at.
	vaultSendBuiltinCacheKey vaultSendKey = "VAULT_SEND_CACHE_KEY"
)

var (
	vaults           = vaultPool{clients: make(map[vaultKey]*vaultClient)}
	vaultAllowedKeys = ast.NewSet(
		ast.StringTerm("address"),
		ast.StringTerm("app_role"), // Enables AppRole auth method. Valid subfields: "id" (required), "from_file", "from_env", "from_string", "wrapping_token" (required if the SecretID is response-wrapped)
		ast.StringTerm("cache"),
		ast.StringTerm("cache_duration"),
		ast.StringTerm("kv2_get"), // Valid subfields: "mount_path", "path"
		ast.StringTerm("raise_error"),
		ast.StringTerm("token"), // Enables Token auth method.
	)

	vaultRequiredKeys = ast.NewSet(ast.StringTerm("address"), ast.StringTerm("kv2_get"))

	// Marked non-deterministic because Vault query results can be non-deterministic.
	vaultSend = &ast.Builtin{
		Name:        vaultSendName,
		Description: "Returns result to the given Vault operation.",
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))).Description("request object"),
			),
			types.Named("response", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))).Description("response object"),
		),
		Nondeterministic: true,
		Categories:       docs("https://docs.styra.com/enterprise-opa/reference/built-in-functions/vault"),
	}

	vaultSendLatencyMetricKey    = "rego_builtin_vault_send"
	vaultSendInterQueryCacheHits = vaultSendLatencyMetricKey + "_interquery_cache_hits"
)

type (
	vaultPool struct {
		clients map[vaultKey]*vaultClient
		mu      sync.Mutex
	}

	vaultClient struct {
		client *vault.Client
		auth   *approle.AppRoleAuth
	}

	vaultKey struct {
		address           string
		appRoleFromFile   string
		appRoleFromEnv    string
		appRoleFromString string
		token             string
	}

	vaultSendKey string
)

func builtinVaultSend(bctx topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	_, span := otel.Tracer(vaultSendName).Start(bctx.Context, "execute")
	defer span.End()

	pos := 1
	obj, err := builtins.ObjectOperand(operands[0].Value, pos)
	if err != nil {
		return err
	}

	requestKeys := ast.NewSet(obj.Keys()...)
	invalidKeys := requestKeys.Diff(vaultAllowedKeys)
	if invalidKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "invalid request parameters(s): %v", invalidKeys)
	}

	missingKeys := vaultRequiredKeys.Diff(requestKeys)
	if missingKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "missing required request parameters(s): %v", missingKeys)
	}

	address, err := getRequestString(obj, "address")
	if err != nil {
		return err
	}

	raiseError, err := getRequestBoolWithDefault(obj, "raise_error", true)
	if err != nil {
		return err
	}

	interQueryCacheEnabled, err := getRequestBoolWithDefault(obj, "cache", false)
	if err != nil {
		return err
	}

	ttl, err := getRequestTimeoutWithDefault(obj, "cache_duration", interQueryCacheDurationDefault)
	if err != nil {
		return err
	}

	appRole, err := getRequestObjectWithDefault(obj, "app_role", nil)
	if err != nil {
		return err
	}

	token, err := getRequestStringWithDefault(obj, "token", "")
	if err != nil {
		return err
	}

	var (
		appRoleID            string
		appRoleFromFile      string
		appRoleFromEnv       string
		appRoleFromString    string
		appRoleWrappingToken bool
	)

	if appRole != nil {
		appRoleID, err = getRequestString(appRole, "id")
		if err != nil {
			return err
		}

		appRoleFromFile, err = getRequestStringWithDefault(appRole, "from_file", "")
		if err != nil {
			return err
		}

		appRoleFromEnv, err = getRequestStringWithDefault(appRole, "from_env", "")
		if err != nil {
			return err
		}

		appRoleFromString, err = getRequestStringWithDefault(appRole, "from_string", "")
		if err != nil {
			return err
		}

		appRoleWrappingToken, err = getRequestBoolWithDefault(appRole, "wrapping_token", false)
		if err != nil {
			return err
		}

		if appRoleFromFile == "" && appRoleFromEnv == "" && appRoleFromString == "" {
			return builtins.NewOperandErr(pos, "missing app role source: either from_file, from_env, or from_string required.")
		}
	} else if token == "" {
		return builtins.NewOperandErr(pos, "missing auth method: either app_role or token required.")
	}

	get, err := getRequestObject(obj, "kv2_get")
	if err != nil {
		return err
	}

	mountPath, err := getRequestString(get, "mount_path")
	if err != nil {
		return err
	}

	path, err := getRequestString(get, "path")
	if err != nil {
		return err
	}

	bctx.Metrics.Timer(vaultSendLatencyMetricKey).Start()

	if responseObj, ok, err := checkCaches(bctx, get, interQueryCacheEnabled, vaultSendBuiltinCacheKey, vaultSendInterQueryCacheHits); ok {
		if err != nil {
			return err
		}

		return iter(ast.NewTerm(responseObj))
	}

	result, getErr := func() (map[string]interface{}, error) {
		client, err := vaults.Get(bctx.Context, address, appRoleID, appRoleFromFile, appRoleFromEnv, appRoleFromString, appRoleWrappingToken, token)
		if err != nil {
			return nil, err
		}

		secret, err := client.KVv2(mountPath).Get(bctx.Context, path)
		if err != nil {
			return nil, err
		}

		return secret.Data, nil
	}()

	m := map[string]interface{}{}
	if getErr == nil {
		m["data"] = result
	} else {
		if raiseError {
			return getErr
		}

		// TODO: Unpack the specific error type to
		// get more details, if possible.

		m["error"] = map[string]interface{}{
			"message": string(getErr.Error()),
		}

		getErr = nil
	}

	responseObj, err := ast.InterfaceToValue(m)
	if err != nil {
		return err
	}

	if err := insertCaches(bctx, get, responseObj.(ast.Object), getErr, interQueryCacheEnabled, ttl, vaultSendBuiltinCacheKey); err != nil {
		return err
	}

	bctx.Metrics.Timer(vaultSendLatencyMetricKey).Stop()

	return iter(ast.NewTerm(responseObj))
}

func (p *vaultPool) Get(ctx context.Context, address string, appRoleID string, appRoleFromFile string, appRoleFromEnv string, appRoleFromString string, appRoleWrappingToken bool, token string) (*vault.Client, error) {
	p.mu.Lock()

	key := vaultKey{
		address,
		appRoleFromFile,
		appRoleFromEnv,
		appRoleFromString,
		token,
	}
	c, ok := p.clients[key]
	if ok {
		p.mu.Unlock()
		return c.client, nil
	}

	p.mu.Unlock()

	config := vault.DefaultConfig()
	config.Address = address

	client, err := vault.NewClient(config)
	if err != nil {
		return nil, err
	}

	vc := &vaultClient{
		client: client,
	}

	// Use either AppRole or Token auth method.

	appRoleAuthMethod := appRoleID != ""
	var authToken *vault.Secret
	if appRoleAuthMethod {
		secretID := &approle.SecretID{
			FromFile:   appRoleFromFile,
			FromEnv:    appRoleFromEnv,
			FromString: appRoleFromString,
		}

		var opts []approle.LoginOption
		if appRoleWrappingToken {
			opts = append(opts, approle.WithWrappingToken())
		}

		vc.auth, err = approle.NewAppRoleAuth(
			appRoleID,
			secretID,
			opts...,
		)
		if err != nil {
			return nil, err
		}

		authToken, err = vc.login(ctx)
		if err != nil {
			return nil, err
		}

	} else {
		client.SetToken(token)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, ok := p.clients[key]; ok {
		return existing.client, nil
	}

	if appRoleAuthMethod {
		go vc.start(authToken)
	}

	p.clients[key] = vc

	return client, nil
}

func (v *vaultClient) login(ctx context.Context) (*vault.Secret, error) {
	token, err := v.client.Auth().Login(ctx, v.auth)
	if err != nil {
		return nil, err
	}

	return token, nil
}

func (v *vaultClient) start(token *vault.Secret) {
	for {
		_ = v.renew(token)
		// TODO: Should log error?

		for {
			renewed, err := v.login(context.Background())
			if err != nil {
				// TODO: Should log error?
				time.Sleep(time.Second)
				continue
			}

			token = renewed
			break
		}
	}
}

func (v *vaultClient) renew(token *vault.Secret) error {
	watcher, err := v.client.NewLifetimeWatcher(&vault.LifetimeWatcherInput{Secret: token})
	if err != nil {
		return err
	}

	go watcher.Start()
	defer watcher.Stop()

	for {
		select {
		case <-watcher.RenewCh(): // renewed OK
		case err := <-watcher.DoneCh():
			return err
		}
	}
}

func init() {
	RegisterBuiltinFunc(vaultSendName, builtinVaultSend)
}
