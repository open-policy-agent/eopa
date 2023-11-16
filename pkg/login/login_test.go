package login_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/styrainc/enterprise-opa-private/pkg/login"
)

const dummyURL = "https://my.das.server"

type testopen struct{ url chan string }

func (to *testopen) Run(url string) error {
	to.url <- url
	close(to.url)
	return nil
}

type ret struct {
	s   string
	err error
}

func TestLoginFlowSuccess(t *testing.T) {
	ctx := context.Background()
	to := testopen{url: make(chan string)}
	login.SetOpener(&to)
	t.Cleanup(login.Reset)

	resultc := make(chan ret)

	go func() {
		s := ret{}
		s.s, s.err = login.Start(ctx, dummyURL)
		resultc <- s
		close(resultc)
	}()

	var port string
	{ // expect url to be opened, extract port
		u := <-to.url
		u0, err := url.Parse(u)
		if err != nil {
			t.Fatal(err)
		}
		port = u0.Query().Get("callback")
	}

	{ // send callback request with cookie
		req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%s", port), nil)
		if err != nil {
			t.Fatalf("callback request: %v", err)
		}
		c := http.Cookie{
			Name:     "gossid",
			Value:    "pssstsecret",
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		req.AddCookie(&c)
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			t.Fatalf("send callback: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("unexpected status: %d", resp.StatusCode)
		}
	}

	{ // check return value
		r := <-resultc
		if r.err != nil {
			t.Fatalf("expected no error: %v", r.err)
		}
		if exp, act := "pssstsecret", r.s; exp != act {
			t.Errorf("unexpected secret, expected %s, got %s", exp, act)
		}
	}
}
