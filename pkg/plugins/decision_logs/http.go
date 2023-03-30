package decisionlogs

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type decisionID struct{}

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

func (m *mw) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.next.ServeHTTP(w, r.WithContext(WithDecisionID(r.Context())))
}

func DecisionIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(decisionID{}).(string)
	return id
}

func WithDecisionID(ctx context.Context) context.Context {
	return context.WithValue(ctx, decisionID{}, uuid.NewString())
}
