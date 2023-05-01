package ekm

import (
	"net/url"
	"sync"

	vault "github.com/hashicorp/vault/api"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
)

type (
	httpsendState struct {
		logger   logging.Logger
		httpsend map[url.URL]map[string]string
		vlogical *vault.Logical
	}
)

var (
	mutex  sync.Mutex
	lState *httpsendState
)

func registerHTTPSend(l logging.Logger, h map[url.URL]map[string]string, v *vault.Logical) {
	mutex.Lock()
	defer mutex.Unlock()
	if lState == nil {
		patchHTTPSend()
	}
	state := new(httpsendState)
	state.logger = l
	state.httpsend = h
	state.vlogical = v
	lState = state
}

func getState() *httpsendState {
	mutex.Lock()
	defer mutex.Unlock()
	return lState
}

func patchHTTPSend() {
	original := topdown.GetBuiltin(ast.HTTPSend.Name)
	topdown.RegisterBuiltinFunc(
		ast.HTTPSend.Name,
		func(bctx topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
			state := getState()
			if state.httpsend == nil {
				return original(bctx, operands, iter) // invoke original
			}

			constraints, err := builtins.ObjectOperand(operands[0].Value, 1)
			if err != nil {
				return original(bctx, operands, iter) // invoke original
			}

			cu := constraints.Get(ast.StringTerm("url"))
			if cu == nil {
				state.logger.Info("missing url")
				return original(bctx, operands, iter) // invoke original
			}

			var opURL *url.URL
			if u, ok := cu.Value.(ast.String); ok {
				u := string(u)
				opURL, err = url.Parse(u)
				if err != nil {
					state.logger.Info("url error: %v, %v", u, err)
					return original(bctx, operands, iter) // invoke original
				}
			}

			srv := url.URL{Scheme: opURL.Scheme, Host: opURL.Host}

			value, ok := state.httpsend[srv]
			if !ok { // no matching URL
				return original(bctx, operands, iter) // invoke original
			}

			var bearer string // special case bearer and scheme (authorization header)
			var scheme string

			// overwrite (non authorization) operands
			for k, v := range value {
				nv, err := lookupKey(state.vlogical, v)
				if err != nil {
					state.logger.Error("lookupKey %v: failed %v", v, err)
					continue
				}
				if k == "bearer" {
					bearer = nv
					continue
				}
				if k == "scheme" {
					scheme = nv
					continue
				}
				constraints.Insert(ast.StringTerm(k), ast.StringTerm(nv)) // add/overwrite existing
			}

			if bearer != "" {
				// create Authorization token header
				if scheme == "" {
					scheme = "Bearer"
				}
				hdrs := ast.StringTerm("headers")
				h := constraints.Get(hdrs)
				if h != nil { // add to existing headers
					if hValue, ok := h.Value.(ast.Object); ok {
						hValue.Insert(ast.StringTerm("Authorization"), ast.StringTerm(scheme+" "+bearer))
					} else {
						state.logger.Info("httpsend invalid type: %T", h.Value)
					}
				} else { // new headers
					obj := ast.NewObject()
					obj.Insert(ast.StringTerm("Authorization"), ast.StringTerm(scheme+" "+bearer))
					constraints.Insert(hdrs, ast.NewTerm(obj))
				}
			}

			//state.logger.Info("operands", operands)
			return original(bctx, operands, iter)
		},
	)
}
