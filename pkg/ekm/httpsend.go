// Copyright 2025 The OPA Authors
// SPDX-License-Identifier: Apache-2.0

package ekm

import (
	"net/url"
	"sync"

	vault "github.com/hashicorp/vault/api"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/topdown/builtins"
)

type (
	httpsendState struct {
		logger   logging.Logger
		httpsend map[url.URL]map[string]any
		vlogical *vault.Logical
	}
)

var (
	mutex  sync.Mutex
	lState *httpsendState
)

func registerHTTPSend(l logging.Logger, h map[url.URL]map[string]any, v *vault.Logical) {
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

func insertHeaders(constraints ast.Object, state *httpsendState, key string, value string) {
	hdrs := ast.StringTerm("headers")
	h := constraints.Get(hdrs)

	if h != nil { // add to existing headers
		if hValue, ok := h.Value.(ast.Object); ok {
			hValue.Insert(ast.StringTerm(key), ast.StringTerm(value))
		} else {
			state.logger.Info("httpsend invalid type: %T", h.Value)
		}
	} else { // new headers
		obj := ast.NewObject()
		obj.Insert(ast.StringTerm(key), ast.StringTerm(value))
		constraints.Insert(hdrs, ast.NewTerm(obj))
	}
}

func insertHeadersSchemeToken(constraints ast.Object, state *httpsendState, key string, value map[string]any) {
	var scheme string // special case bearer and scheme
	var bearer string

	// extract scheme and bearer from map
	for k, v := range value {
		v, ok := v.(string)
		if !ok {
			state.logger.Error("headers %v invalid type %T", k, v)
			continue
		}
		if k != "scheme" && k != "bearer" {
			state.logger.Error("unexpected field %v", k)
			continue
		}
		nv, err := lookupKey(state.vlogical, v)
		if err != nil {
			state.logger.Error("lookupKey %v: failed %v", v, err)
			continue
		}
		if k == "scheme" {
			scheme = nv
		}
		if k == "bearer" {
			bearer = nv
		}
	}
	if bearer == "" {
		state.logger.Error("invalid headers %v, expected bearer", key)
		return
	}
	if scheme == "" {
		scheme = "Bearer"
	}
	insertHeaders(constraints, state, key, scheme+" "+bearer)
}

// NOTE(sr): In topdown, RegisterBuiltinFunc will overwrite the entry in the map that's
// consulted via GetBuiltin. So, the original we store here is the one that has been set
// through the OPA code, and it will not be altered by pathHTTPSend.
var original = topdown.GetBuiltin(ast.HTTPSend.Name)

func resetHTTPSend() {
	topdown.RegisterBuiltinFunc(ast.HTTPSend.Name, original)
}

func patchHTTPSend() {
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

			// overwrite (non authorization) operands
			for k, v := range value {

				// insert headers
				if k == "headers" {
					v, ok := v.(map[string]any)
					if !ok {
						state.logger.Error("headers %v invalid type %T", k, v)
						continue
					}
					for k2, v2 := range v {
						switch v2 := v2.(type) {
						case string:
							nv, err := lookupKey(state.vlogical, v2)
							if err != nil {
								state.logger.Error("lookupKey %v: failed %v", k2, v2, err)
								continue
							}

							// inject arbitrary header
							insertHeaders(constraints, state, k2, nv)
							continue

						case map[string]any:
							// inject header from scheme+token
							insertHeadersSchemeToken(constraints, state, k2, v2)

						default:
							state.logger.Error("key %v unexpected value %T", k2, v2)
						}
					}
					continue
				}

				// insert parameter
				v, ok := v.(string)
				if !ok {
					state.logger.Error("value %v invalid type %T", k, v)
					continue
				}

				nv, err := lookupKey(state.vlogical, v)
				if err != nil {
					state.logger.Error("lookupKey %v: failed %v", v, err)
					continue
				}
				constraints.Insert(ast.StringTerm(k), ast.StringTerm(nv)) // add/overwrite existing
			}

			//state.logger.Info("operands", operands)
			return original(bctx, operands, iter)
		},
	)
}
