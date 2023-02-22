package okta

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/okta/okta-sdk-golang/v3/okta"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/plugins"
	"github.com/open-policy-agent/opa/storage"
)

const (
	Name = "okta"
)

// Data plugin
type Data struct {
	manager        *plugins.Manager
	log            logging.Logger
	Config         Config
	exit, doneExit chan struct{}
}

func (c *Data) Start(ctx context.Context) error {
	c.exit = make(chan struct{})
	if err := storage.Txn(ctx, c.manager.Store, storage.WriteParams, func(txn storage.Transaction) error {
		return storage.MakeDir(ctx, c.manager.Store, txn, c.Config.path)
	}); err != nil {
		return err
	}

	c.doneExit = make(chan struct{})
	go c.loop(ctx)
	return nil
}

func (c *Data) Stop(ctx context.Context) {
	if c.doneExit == nil {
		return
	}
	close(c.exit) // stops our polling loop
	select {
	case <-c.doneExit: // waits for polling loop to be stopped
	case <-ctx.Done(): // or exit if context canceled or timed out
	}
}

func (c *Data) Reconfigure(ctx context.Context, next any) {
	if c.Config.Equal(next.(Config)) {
		return // nothing to do
	}
	if c.doneExit != nil { // started before
		c.Stop(ctx)
	}
	c.Config = next.(Config)
	c.Start(ctx)
}

func (c *Data) loop(ctx context.Context) {
	hc := &http.Client{} // client is needed to close all idle connections
	conf := okta.NewConfiguration(c.Config.config...)
	conf.HTTPClient = hc
	timer := time.NewTimer(0) // zero timer is needed to execute immediately for first time

LOOP:
	for {
		select {
		case <-ctx.Done():
			break LOOP
		case <-c.exit:
			break LOOP
		case <-timer.C:
		}
		c.poll(ctx, conf)
		timer.Reset(c.Config.interval)
	}
	// stop and drain the timer
	if !timer.Stop() && len(timer.C) > 0 {
		<-timer.C
	}
	hc.CloseIdleConnections()
	close(c.doneExit)
}

func (c *Data) poll(ctx context.Context, conf *okta.Configuration) {
	client := okta.NewAPIClient(conf)
	if c.Config.ClientSecret != "" {
		var token string
		token, err := exchangeClientSecret(ctx, conf.HTTPClient, c.Config)
		if err != nil {
			c.log.Error("okta client misconfiguration: %v", err)
			return
		}
		// the client must be recreated with the token
		conf.Okta.Client.Token = token
		client = okta.NewAPIClient(conf)
	}

	results := make(map[string]any)
	var merr []error

	if c.Config.Users {
		users, err := getUsers(ctx, client)
		if err != nil {
			merr = append(merr, err)
		} else {
			results["users"] = users
		}
	}

	if c.Config.Groups {
		groups, err := getGroups(ctx, client)
		if err != nil {
			merr = append(merr, err)
		} else {
			results["groups"] = groups

			members, err := getGroupMembers(ctx, client, groups)
			if err != nil {
				merr = append(merr, err)
			} else {
				results["group-members"] = members
			}
		}
	}

	if c.Config.Roles {
		roles, err := getRoles(ctx, client)
		if err != nil {
			merr = append(merr, err)
		} else {
			results["roles"] = roles
		}
	}

	if c.Config.Apps {
		apps, err := getApps(ctx, client)
		if err != nil {
			merr = append(merr, err)
		} else {
			results["apps"] = apps
		}
	}

	if len(merr) > 0 {
		c.log.Warn("not all resources were fetched: %v", errors.Join(merr...))
		return
	}

	if err := storage.WriteOne(ctx, c.manager.Store, storage.ReplaceOp, c.Config.path, results); err != nil {
		c.log.Error("writing data to %+v failed: %v", c.Config.path, err)
	}
}

func getUsers(ctx context.Context, client *okta.APIClient) ([]okta.User, error) {
	users, resp, err := client.UserApi.ListUsers(ctx).Execute()
	if err != nil {
		return nil, err
	}
	for resp.HasNextPage() {
		var nextUsers []okta.User
		resp, err = resp.Next(&nextUsers)
		if err != nil {
			return nil, err
		}
		users = append(users, nextUsers...)
	}
	return users, err
}

func getGroups(ctx context.Context, client *okta.APIClient) ([]okta.Group, error) {
	groups, resp, err := client.GroupApi.ListGroups(ctx).Execute()
	if err != nil {
		return nil, err
	}
	for resp.HasNextPage() {
		var nextGroups []okta.Group
		resp, err = resp.Next(&nextGroups)
		if err != nil {
			return nil, err
		}
		groups = append(groups, nextGroups...)
	}
	return groups, err
}

func getGroupMembers(ctx context.Context, client *okta.APIClient, groups []okta.Group) (map[string][]okta.User, error) {
	members := make(map[string][]okta.User, len(groups))
	for _, g := range groups {
		gid := g.GetId()
		users, resp, err := client.GroupApi.ListGroupUsers(ctx, gid).Execute()
		if err != nil {
			return nil, err
		}
		for resp.HasNextPage() {
			var nextUsers []okta.User
			resp, err = resp.Next(&nextUsers)
			if err != nil {
				return nil, err
			}
			users = append(users, nextUsers...)
		}
		members[gid] = users
	}
	return members, nil
}

func getRoles(ctx context.Context, client *okta.APIClient) ([]okta.IamRole, error) {
	res, resp, err := client.RoleApi.ListRoles(ctx).Execute()
	if err != nil {
		return nil, err
	}
	roles := res.GetRoles()
	for resp.HasNextPage() {
		var nextRoles okta.IamRoles
		resp, err = resp.Next(&nextRoles)
		if err != nil {
			return nil, err
		}
		roles = append(roles, nextRoles.Roles...)
	}
	return roles, err
}

func getApps(ctx context.Context, client *okta.APIClient) ([]any, error) {
	apps, resp, err := client.ApplicationApi.ListApplications(ctx).Execute()
	applications := make([]any, len(apps))
	for i := range apps {
		applications[i] = apps[i].GetActualInstance()
	}
	if err != nil {
		return nil, err
	}
	for resp.HasNextPage() {
		var nextApps []okta.ListApplications200ResponseInner
		resp, err = resp.Next(&nextApps)
		if err != nil {
			return nil, err
		}
		for i := range nextApps {
			applications = append(applications, nextApps[i].GetActualInstance())
		}
	}
	return applications, err
}

func exchangeClientSecret(ctx context.Context, client *http.Client, conf Config) (string, error) {
	body := url.Values{}
	body.Add("grant_type", "client_credentials")
	body.Add("scope", strings.Join(conf.scopes, " "))

	u, err := url.JoinPath(conf.URL, "/oauth2/v1/token")
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	basic := base64.StdEncoding.EncodeToString([]byte(conf.ClientID + ":" + conf.ClientSecret))
	req.Header.Set("Authorization", "Basic "+basic)
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		if resp.Body != nil {
			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return "", err
			}
			return "", fmt.Errorf("%s with response: %s", resp.Status, string(data))
		}
		return "", errors.New(resp.Status)
	}

	var token okta.RequestAccessToken
	err = json.NewDecoder(resp.Body).Decode(&token)
	resp.Body.Close()
	return token.AccessToken, err
}
