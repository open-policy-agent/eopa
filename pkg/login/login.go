package login

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/open-policy-agent/opa/logging"
)

// timeout determines how long the CLI command will wait for
// an incoming request
const timeout = time.Minute

// cookie is the name of the secret session cookie we're retrieving
const cookie = "gossid"

// navMsg is what we display if we're not instructed to open a browser
// or if a problem occurred trying to open the browser. Whitespace matters!
const navMsg = `Please navigate to the following URL in your browser:

  %s`

type opts struct {
	browser bool
	url     *url.URL
	open    Opener
	timeout time.Duration
	logger  logging.Logger
}

type Opt func(*opts)

func Browser(y bool) Opt {
	return func(o *opts) {
		o.browser = y
	}
}

func URL(u *url.URL) Opt {
	return func(o *opts) {
		o.url = u
	}
}

func Open(op Opener) Opt {
	return func(o *opts) {
		o.open = op
	}
}

func Timeout(t time.Duration) Opt {
	return func(o *opts) {
		o.timeout = t
	}
}

func Logger(l logging.Logger) Opt {
	return func(o *opts) {
		o.logger = l
	}
}

func Start(ctx context.Context, opt ...Opt) (string, error) {
	o := &opts{
		open:    defaultOpener{},
		timeout: timeout,
		logger:  logging.NewNoOpLogger(),
	}
	for _, opt := range opt {
		opt(o)
	}
	url := o.url

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	// start listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	srv := newServer(listener, url)
	srv.Start()
	defer srv.Stop()

	toOpen := fmt.Sprintf("%s/?callback=%d", url, port)

	if !o.browser {
		o.logger.Info(navMsg, toOpen)
	} else {
		if err := o.open.Run(toOpen); err != nil { // open url, this won't block
			o.logger.Warn("Could not open browser (%s).\n", err.Error())
			o.logger.Info(navMsg, toOpen)
		} else {
			o.logger.Info("Opening %s", toOpen)
		}
	}

	var secret string
	// wait for listener to be done
	select {
	case s := <-srv.c:
		secret = s
	case e := <-srv.errc:
		err = e
	case <-ctx.Done():
		err = ctx.Err()
	}
	return secret, err
}

type server struct {
	l    net.Listener
	url  *url.URL
	c    chan string
	errc chan error
	s    *http.Server
}

func newServer(l net.Listener, u *url.URL) *server {
	return &server{l: l,
		c:    make(chan string),
		errc: make(chan error),
		url:  u,
	}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Origin", s.url.String())
	c, err := r.Cookie(cookie)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.c <- c.Value
}

func (s *server) Start() {
	s.s = &http.Server{Handler: s}

	go func() {
		s.errc <- s.s.Serve(s.l) // blocks
		close(s.errc)
	}()
}

func (s *server) Stop() error {
	close(s.c)
	return s.s.Close()
}
