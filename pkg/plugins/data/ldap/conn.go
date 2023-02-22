package ldap

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

type clientKey struct{}

// WithClient puts the ldap client into context, used by unit tests to mock the client
func WithClient(parent context.Context, client ldap.Client) context.Context {
	return context.WithValue(parent, clientKey{}, client)
}

func getNetConn(ctx context.Context, u *url.URL, tc *tls.Config) (net.Conn, bool, error) {
	dialer := &net.Dialer{
		Timeout: ldap.DefaultTimeout,
	}

	if u.Scheme == "ldapi" {
		if u.Path == "" || u.Path == "/" {
			u.Path = "/var/run/slapd/ldapi"
		}
		conn, err := dialer.DialContext(ctx, "unix", u.Path)
		return conn, false, err
	}

	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		// use default ldap por if the port is missing
		if !strings.Contains(err.Error(), "missing port in address") {
			return nil, false, err
		}
		host = u.Host
		port = ""
	}

	switch u.Scheme {
	case "ldap":
		if port == "" {
			port = ldap.DefaultLdapPort
		}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
		return conn, false, err
	case "ldaps":
		if port == "" {
			port = ldap.DefaultLdapsPort
		}
		tlsDialer := tls.Dialer{
			NetDialer: dialer,
			Config:    tc,
		}
		conn, err := tlsDialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
		return conn, true, err
	}

	return nil, false, fmt.Errorf("unknown scheme %q", u.Scheme)
}

func getLDAPConn(ctx context.Context, u *url.URL, tc *tls.Config) (ldap.Client, error) {
	if cl, ok := ctx.Value(clientKey{}).(ldap.Client); ok {
		cl.Start()
		return cl, nil
	}
	c, isTLS, err := getNetConn(ctx, u, tc)
	if err != nil {
		return nil, err
	}

	conn := ldap.NewConn(c, isTLS)
	conn.Start()
	return conn, nil
}
