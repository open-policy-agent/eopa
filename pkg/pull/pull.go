package pull

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-policy-agent/opa/logging"

	"github.com/styrainc/enterprise-opa-private/pkg/dasapi"
)

const cookieName = "gosessid"
const dirPermission = 0o700
const filePermission = 0o600

type opts struct {
	url         *url.URL
	logger      logging.Logger
	sessionFile string
	target      string
	force       bool
}

type Opt func(*opts)

func Force(b bool) Opt {
	return func(o *opts) {
		o.force = b
	}
}

func URL(u *url.URL) Opt {
	return func(o *opts) {
		o.url = u
	}
}

func Logger(l logging.Logger) Opt {
	return func(o *opts) {
		o.logger = l
	}
}

func SessionFile(s string) Opt {
	return func(o *opts) {
		o.sessionFile = s
	}
}

func TargetDir(s string) Opt {
	return func(o *opts) {
		o.target = s
	}
}

type cl struct {
	cookie *http.Cookie
	client *http.Client
	logger logging.Logger
	cl     *dasapi.Client
	target string
}

func Start(ctx context.Context, opt ...Opt) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	o := &opts{
		logger: logging.NewNoOpLogger(),
	}
	for _, opt := range opt {
		opt(o)
	}

	if err := o.preflight(); err != nil {
		return err
	}

	hc, err := o.newClient()
	if err != nil {
		return err
	}

	cl, err := dasapi.NewClient(o.url.String(),
		dasapi.WithHTTPClient(hc),
	)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}
	hc.cl = cl

	resp, err := cl.LibrariesList(ctx)
	if err != nil {
		return fmt.Errorf("list libraries: %w", err)
	}
	libs, err := dasapi.ParseLibrariesListResponse(resp)
	if err != nil {
		return fmt.Errorf("list libraries response: %w", err)
	}
	if err := checkStatus(libs); err != nil {
		return fmt.Errorf("list libraries status: %w", err)
	}

	libIDs := make([]string, 0, len(libs.JSON200.Result))
	for _, r := range libs.JSON200.Result {
		libIDs = append(libIDs, r.Id)
	}
	hc.logger.Info("Retrieving %d libraries: %s", len(libIDs), strings.Join(libIDs, ", "))

	for _, id := range libIDs {
		if err := hc.getLibrary(ctx, id); err != nil {
			return fmt.Errorf("get library %s: %w", id, err)
		}
	}
	return nil
}

func (o *opts) preflight() error {
	if o.target == "" {
		return fmt.Errorf("target directory not set")
	}
	d, err := os.Stat(o.target)
	if os.IsNotExist(err) {
		return os.MkdirAll(o.target, dirPermission)
	}
	if err != nil {
		return err
	}
	if !d.IsDir() {
		return fmt.Errorf("target %s already exists, not a directory", o.target)

	}
	if d.IsDir() { // is it empty?
		files, err := os.ReadDir(o.target)
		if err != nil {
			return err
		}
		if len(files) > 0 && !o.force {
			return fmt.Errorf("target directory %s already exists (non-empty)", o.target)
		}
	}
	return nil
}

func (o *opts) newClient() (*cl, error) {
	secret, err := os.ReadFile(o.sessionFile)
	if err != nil {
		return nil, fmt.Errorf("read secret %s: %w", o.sessionFile, err)
	}

	o.logger.Debug("read secret from %s", o.sessionFile)
	return &cl{
		cookie: &http.Cookie{
			Name:     cookieName,
			Value:    strings.TrimSpace(string(secret)),
			HttpOnly: true,
			Secure:   true,
		},
		client: http.DefaultClient,
		logger: o.logger,
		target: o.target,
	}, nil
}

func (c *cl) Do(req *http.Request) (*http.Response, error) {
	req.AddCookie(c.cookie)
	ds, _ := httputil.DumpRequestOut(req, true)
	c.logger.Debug(string(ds))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	rs, _ := httputil.DumpResponse(resp, true)
	c.logger.Debug(string(rs))
	return resp, nil
}

func (c *cl) getLibrary(ctx context.Context, id string) error {
	resp, err := c.cl.LibrariesGet(ctx, id)
	if err != nil {
		return err
	}

	lib, err := dasapi.ParseLibrariesGetResponse(resp)
	if err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if err := checkStatus(lib); err != nil {
		return err
	}
	for _, p := range lib.JSON200.Result.Policies {
		if p.Modules == nil {
			continue
		}
		if err := c.getPolicy(ctx, p.Id); err != nil {
			return fmt.Errorf("get policy %s: %w", p.Id, err)
		}
	}
	for _, d := range lib.JSON200.Result.Datasources {
		if err := c.getData(ctx, d.Id); err != nil {
			return fmt.Errorf("get data source %s: %w", d.Id, err)
		}
	}
	return nil
}

func (c *cl) getPolicy(ctx context.Context, id string) error {
	resp, err := c.cl.GetPolicy(ctx, id, &dasapi.GetPolicyParams{}) // include deps?
	if err != nil {
		return err
	}
	pol, err := dasapi.ParseGetPolicyResponse(resp)
	if err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if err := checkStatus(pol); err != nil {
		return err
	}

	m, ok := pol.JSON200.Result.(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected result (%T)", pol.JSON200.Result)
	}
	modules, ok := m["modules"].(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected result (%T)", m["modules"])
	}
	if len(modules) > 0 {
		if err := os.MkdirAll(c.path(id), dirPermission); err != nil {
			return err
		}
	}
	for file, mod := range modules {
		if err := os.WriteFile(c.path(id, file), []byte(mod.(string)), filePermission); err != nil {
			return err
		}
	}
	return nil
}

func (c *cl) getData(ctx context.Context, id string) error {
	lim := "1048576" // same as UI uses
	resp, err := c.cl.GetData(ctx, id, &dasapi.GetDataParams{Limit: &lim})
	if err != nil {
		return err
	}
	dat, err := dasapi.ParseGetDataResponse(resp)
	if err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if err := checkStatus(dat); err != nil {
		return err
	}

	blob, ok := dat.JSON200.Result.(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected result (%T)", dat.JSON200.Result)
	}

	if len(blob) > 0 {
		if err := os.MkdirAll(c.path(filepath.Dir(id)), dirPermission); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(c.path(id), os.O_RDWR|os.O_CREATE|os.O_TRUNC, filePermission)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	if err := json.NewEncoder(f).Encode(blob); err != nil {
		return fmt.Errorf("encode data: %w", err)
	}
	return f.Close()
}

func (c *cl) path(policyID string, extra ...string) string {
	policyID = policyID[len("libraries/"):]
	return filepath.Join(append([]string{c.target, policyID}, extra...)...)
}

func checkStatus(x interface{ StatusCode() int }) error {
	if x.StatusCode() != http.StatusOK {
		return fmt.Errorf("unexpected response status %d", x.StatusCode())
	}
	return nil
}
