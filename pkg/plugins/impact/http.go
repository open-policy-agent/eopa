package impact

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/loader"
)

const (
	httpPrefix = "POST /v0/impact"
	metricName = "v0/impact"
)

type httpError struct {
	Error string `json:"error"`
}

func returnError(w http.ResponseWriter, err error) {
	returnErrorCode(w, err, http.StatusBadRequest)
}

func returnInternal(w http.ResponseWriter, err error) {
	returnErrorCode(w, err, http.StatusInternalServerError)
}

func returnErrorCode(w http.ResponseWriter, err error, code int) {
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(httpError{err.Error()}); err != nil {
		panic(err)
	}
}

func (i *Impact) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost: // OK
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{}) // disable Server.WriteTimeout for sending a slow response

	job, err := jobFromRequest(r)
	if err != nil {
		returnError(w, err)
		return
	}
	if err := i.StartJob(r.Context(), job); err != nil {
		returnError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	// Flush if we can.
	if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		returnInternal(w, err)
		return
	}

	enc := json.NewEncoder(w)
	for res := range job.Results() {
		if err := enc.Encode(res); err != nil {
			returnInternal(w, err)
			return
		}
		// Flush if we can.
		if err := rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
			returnInternal(w, err)
			return
		}
	}
}

func jobFromRequest(r *http.Request) (Job, error) {
	qu, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	if !qu.Has("rate") {
		return nil, errors.New("missing \"rate\" parameter")
	}
	rate, err := strconv.ParseFloat(qu.Get("rate"), 32)
	if err != nil {
		return nil, err
	}

	if !qu.Has("duration") {
		return nil, errors.New("missing \"duration\" parameter")
	}
	dur, err := time.ParseDuration(qu.Get("duration"))
	if err != nil {
		return nil, err
	}

	publishEquals := false
	if qu.Has("equals") {
		publishEquals, err = strconv.ParseBool(qu.Get("equals"))
		if err != nil {
			return nil, err
		}
	}

	bndl, err := bundleFromReader(r.Context(), r.Body)
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			return nil, errors.New("missing bundle payload")
		}
		return nil, err
	}

	return NewJob(r.Context(), float32(rate), publishEquals, bndl, dur), nil
}

func bundleFromReader(_ context.Context, rd io.ReadCloser) (*bundle.Bundle, error) {
	defer rd.Close()
	path := "tmp.tar.gz"

	return loader.NewFileLoader().
		WithReader(rd).
		WithSkipBundleVerification(true).
		AsBundle(path)
}

type mw struct {
	next http.Handler
}

func HTTPMiddleware(next http.Handler) http.Handler {
	return &mw{next: next}
}

func (m *mw) Flush() {
	if f, ok := m.next.(http.Flusher); ok {
		f.Flush()
	}
}

type lia struct{}

func Enable(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, lia{}, path)
}

func liaEnabled(ctx context.Context) string {
	path, _ := ctx.Value(lia{}).(string)
	return path
}

func (m *mw) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.next.ServeHTTP(w, r.WithContext(Enable(r.Context(), r.URL.Path)))
}
