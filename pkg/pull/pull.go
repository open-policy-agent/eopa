package pull

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/liamg/memoryfs"

	"github.com/open-policy-agent/opa/logging"

	"github.com/styrainc/enterprise-opa-private/pkg/dasapi"
)

const cookieName = "gosessid"
const dirPermission = 0o700
const filePermission = 0o600

// This limit is used with the /v1/data endpoint to retrieve datsource
// contents. This value is the same as the DAS UI uses.
const dataSizeLimit = "1048576"

const warningStaleFiles = "Found these files that don't currently exist in the remote location: %s."
const warningStaleFilesExtra = "If you were not expecting this message, please delete those files or run `eopa pull --force`"

type opts struct {
	url         *url.URL
	logger      logging.Logger
	sessionFile string
	token       string
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

func APIToken(t string) Opt {
	return func(o *opts) {
		o.token = t
	}
}

func TargetDir(s string) Opt {
	return func(o *opts) {
		o.target = s
	}
}

type cl struct {
	// cookie and token are two potential authentication methods.
	// We'll read sessionFile and populate either of the two fields
	// accordingly.
	cookie *http.Cookie
	token  string

	client *http.Client
	logger logging.Logger
	cl     *dasapi.Client
	fs     *memoryfs.FS
	target string
	force  bool
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

	libsResp, err := cl.LibrariesList(ctx)
	if err != nil {
		return fmt.Errorf("list libraries: %w", err)
	}
	libs, err := dasapi.ParseLibrariesListResponse(libsResp)
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

	if err := hc.checkStaleFiles(); err != nil {
		return fmt.Errorf("check stale files: %w", err)
	}
	return hc.writeToFS()
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
	return nil
}

func (o *opts) newClient() (*cl, error) {
	c := cl{
		client: http.DefaultClient,
		logger: o.logger,
		target: o.target,
		force:  o.force,
		token:  o.token,
		fs:     newMemFS(o.target),
	}

	if o.token != "" {
		return &c, nil
	}

	// no token provided, read session file
	secret, err := os.ReadFile(o.sessionFile)
	if err != nil {
		return nil, fmt.Errorf("read secret %s: %w", o.sessionFile, err)
	}
	if len(secret) == 0 {
		return nil, fmt.Errorf("read secret %s: empty file", o.sessionFile)
	}

	c.cookie = &http.Cookie{
		Name:     cookieName,
		Value:    strings.TrimSpace(string(secret)),
		HttpOnly: true,
		Secure:   true,
	}
	o.logger.Debug("read session secret from %s", o.sessionFile)

	return &c, nil
}

func newMemFS(tgt string) *memoryfs.FS {
	m := memoryfs.New()
	if err := m.MkdirAll(tgt, dirPermission); err != nil {
		panic(err)
	}
	if err := m.WriteFile(filepath.Join(tgt, ".gitignore"),
		[]byte("*\n"),
		filePermission); err != nil {
		panic(err)
	}
	return m
}

func (c *cl) Do(req *http.Request) (*http.Response, error) {
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	} else if c.token != "" {
		req.Header.Set("Authorization", "bearer "+c.token)
	}
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
		if err := c.fs.MkdirAll(c.path(id), dirPermission); err != nil {
			return err
		}
	}
	for file, mod := range modules {
		// NOTE(sr): if this already exists, it's overwritten
		if err := c.fs.WriteFile(c.path(id, file), []byte(mod.(string)), filePermission); err != nil {
			return err
		}
	}
	return nil
}

func (c *cl) getData(ctx context.Context, id string) error {
	lim := dataSizeLimit
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

	blob, err := json.Marshal(dat.JSON200.Result)
	if err != nil {
		return fmt.Errorf("could not marshal datasource contents: %w", err)
	}

	// NOTE: in order for EOPA to be able to load the datasource locally,
	// its parent directory should be the final path element in the
	// `data.` path, and the data itself should live in a `data.json` file
	// inside of that folder. This is similar but different from the
	// policy files, where we want the output of c.path() to directly
	// determine the full path to place the file at.
	//
	// -- CAD 2024-01-10

	if len(blob) > 0 {
		if err := c.fs.MkdirAll(c.path(id), dirPermission); err != nil {
			return err
		}
	}

	// Make sure there is always a trailing newline.
	if blob[len(blob)-1] != '\n' {
		blob = append(blob, '\n')
	}

	if err := c.fs.WriteFile(filepath.Join(c.path(id), "data.json"), blob, filePermission); err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	return nil
}

func (c *cl) checkStaleFiles() error {
	// We walk the OS directory, and check if there is an incoming (memfs)
	// file for each of the files/dirs in the target directory.
	var warnFiles []string
	err := fs.WalkDir(os.DirFS(c.target), ".", func(shortPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		path := filepath.Join(c.target, shortPath)
		_, err = fs.Stat(c.fs, path)
		if os.IsNotExist(err) { // exists in fs, not in memfs
			if c.force {
				if err := os.RemoveAll(path); err != nil {
					return fmt.Errorf("remove %s: %w", shortPath, err)
				}
			} else {
				warnFiles = append(warnFiles, filepath.FromSlash(shortPath))
			}
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	if len(warnFiles) > 0 {
		c.logger.Warn(warningStaleFiles, strings.Join(warnFiles, ", "))
		c.logger.Warn(warningStaleFilesExtra)
	}
	return nil
}

func (c *cl) writeToFS() error {
	// We walk the memfs and create whatever we find in the OS target
	// directory.
	return fs.WalkDir(c.fs, c.target, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		src, err := c.fs.Open(path)
		if err != nil {
			panic(err) // Impossible: we're walking c.fs
		}

		fi, _ := src.Stat()
		if fi.IsDir() { // deal with directories
			if err := os.MkdirAll(path, dirPermission); err != nil {
				return fmt.Errorf("create target dir %s: %w", path, err)
			}
			return nil
		}

		dst, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, filePermission)
		if err != nil {
			return fmt.Errorf("create target file %s: %w", path, err)
		}
		if _, err := io.Copy(dst, src); err != nil {
			return fmt.Errorf("copy to target file %s: %w", path, err)
		}
		return dst.Close()
	})
}

func (c *cl) path(policyID string, extra ...string) string {
	// NOTE: we expect policyID to include the `libraries/` prefix. It is
	// important that we keep this not only to match with DAS conventions,
	// but also because while OPA uses `package` statements to place Rego
	// policies into the `data.` hierarchy, it uses paths on disk to place
	// data.json files. It is thus important for both datasources, and for
	// JSON files included in repositories to include the `libraries`
	// prefix so the data will be loaded into the right place. Note that at
	// present, the latter situation is only applicable when the DATA_FILES
	// feature flag is enabled for DAS.
	//
	// -- CAD 2024-01-10
	return filepath.Join(append([]string{c.target, policyID}, extra...)...)
}

func checkStatus(x interface{ StatusCode() int }) error {
	if x.StatusCode() != http.StatusOK {
		return fmt.Errorf("unexpected response status %d", x.StatusCode())
	}
	return nil
}
