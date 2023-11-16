package login

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// timeout determines how long the CLI command will wait for
// an incoming request
const timeout = time.Minute

// cookie is the name of the secret session cookie we're retrieving
const cookie = "gossid"

// TODO: start CLI input fall-back
func Start(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
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
	fmt.Println("Opening", toOpen)

	// open url, this won't block
	if err := open.Run(toOpen); err != nil {
		return "", err
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
	url  string
	c    chan string
	errc chan error
	s    *http.Server
}

func newServer(l net.Listener, url string) *server {
	return &server{l: l,
		c:    make(chan string),
		errc: make(chan error),
		url:  url,
	}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Origin", s.url)
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
