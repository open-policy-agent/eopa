package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/otel"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/types"
	"github.com/open-policy-agent/opa/util"
)

const (
	mongoDBSendName = "mongodb.send"
	// mongoDBSendBuiltinCacheKey is the key in the builtin context cache that
	// points to the mongodb.send() specific intra-query cache resides at.
	mongoDBSendBuiltinCacheKey            mongoDBSendKey = "MONGODB_SEND_CACHE_KEY"
	mongoDBInterQueryCacheDurationDefault                = 60 * time.Second
)

var (
	mongoDBClients = mongoDBClientPool{clients: make(map[mongoDBClientKey]*mongo.Client)}

	mongoDBAllowedKeys = ast.NewSet(
		ast.StringTerm("cache"),
		ast.StringTerm("cache_duration"),
		ast.StringTerm("find"),
		ast.StringTerm("find_one"),
		ast.StringTerm("raise_error"),
		ast.StringTerm("uri"),
	)

	mongoDBRequiredKeys = ast.NewSet(ast.StringTerm("uri"))

	// Marked non-deterministic because query results can be non-deterministic.
	mongoDBSend = &ast.Builtin{
		Name:        mongoDBSendName,
		Description: "Returns query result rows to the given MongoDB operation.",
		Decl: types.NewFunction(
			types.Args(
				types.Named("request", types.NewObject(nil, types.NewDynamicProperty(types.S, types.A))),
			),
			types.Named("response", types.NewObject(nil, types.NewDynamicProperty(types.A, types.A))),
		),
		Nondeterministic: true,
	}

	mongoDBSendLatencyMetricKey    = "rego_builtin_mongodb_send"
	mongoDBSendInterQueryCacheHits = mongoDBSendLatencyMetricKey + "_interquery_cache_hits"
)

type (
	mongoDBClientPool struct {
		mu      sync.Mutex
		clients map[mongoDBClientKey]*mongo.Client
	}

	mongoDBClientKey struct {
		uri string
	}

	mongoDBSendKey string
)

func builtinMongoDBSend(bctx topdown.BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	_, span := otel.Tracer(mongoDBSendName).Start(bctx.Context, "execute")
	defer span.End()

	pos := 1
	obj, err := builtins.ObjectOperand(operands[0].Value, pos)
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	requestKeys := ast.NewSet(obj.Keys()...)
	invalidKeys := requestKeys.Diff(mongoDBAllowedKeys)
	if invalidKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "invalid request parameters(s): %v", invalidKeys)
	}

	missingKeys := mongoDBRequiredKeys.Diff(requestKeys)
	if missingKeys.Len() != 0 {
		return builtins.NewOperandErr(pos, "missing required request parameters(s): %v", missingKeys)
	}

	find, err := getRequestObjectWithDefault(obj, "find", nil)
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	findOne, err := getRequestObjectWithDefault(obj, "find_one", nil)
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	cacheKey := ast.NewObject()
	var commonFind ast.Object

	switch {
	case find == nil && findOne == nil:
		return builtins.NewOperandErr(pos, "missing required request parameters(s): find or find_one")
	case find != nil && findOne != nil:
		return builtins.NewOperandErr(pos, "extra request parameters(s): find or find_one")
	case find != nil:
		cacheKey.Insert(ast.StringTerm("find"), ast.NewTerm(find))
		commonFind = find
	case findOne != nil:
		cacheKey.Insert(ast.StringTerm("find_one"), ast.NewTerm(findOne))
		commonFind = findOne
	}

	uri, err := getRequestString(obj, "uri")
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	raiseError, err := getRequestBoolWithDefault(obj, "raise_error", true)
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	interQueryCacheEnabled, err := getRequestBoolWithDefault(obj, "cache", false)
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	ttl, err := getRequestTimeoutWithDefault(obj, "cache_duration", interQueryCacheDurationDefault)
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	// TODO: Improve error handling to allow separation between
	// types of errors (invalid queries, connectivity errors,
	// etc.)

	database, err := getRequestString(commonFind, "database")
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	collection, err := getRequestString(commonFind, "collection")
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	filter, err := getRequestObject(commonFind, "filter")
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	opt, err := getRequestObjectWithDefault(commonFind, "options", ast.NewObject())
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	canonical, err := getRequestBoolWithDefault(commonFind, "canonical", false)
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	bctx.Metrics.Timer(mongoDBSendLatencyMetricKey).Start()

	if responseObj, ok, err := checkCaches(bctx, cacheKey, interQueryCacheEnabled, mongoDBSendBuiltinCacheKey, mongoDBSendInterQueryCacheHits); ok {
		if err != nil {
			return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
		}

		return iter(ast.NewTerm(responseObj))
	}

	m := map[string]interface{}{}
	queryErr := func() error {
		client, err := mongoDBClients.Get(bctx.Context, uri)
		if err != nil {
			return err
		}

		j, err := ast.JSON(filter)
		if err != nil {
			return err
		}

		data, err := json.Marshal(j)
		if err != nil {
			return err
		}

		var filter interface{}
		if err := bson.UnmarshalExtJSON(data, true, &filter); err != nil {
			return err
		}

		coll := client.Database(database).Collection(collection)

		if find != nil {
			var o options.FindOptions
			if opt.Len() > 0 {
				v, err := ast.JSON(opt)
				if err != nil {
					return err
				}

				data, err := json.Marshal(v)
				if err != nil {
					return err
				}

				if err := json.Unmarshal(data, &o); err != nil {
					return err
				}
			}

			cursor, err := coll.Find(bctx.Context, filter, &o)
			if err != nil {
				return err
			}

			var docs []bson.M
			if err = cursor.All(bctx.Context, &docs); err != nil {
				return err
			}

			results := make([]interface{}, 0, len(docs))
			for _, doc := range docs {
				data, err = bson.MarshalExtJSON(doc, canonical, false)
				if err != nil {
					return err
				}

				var result interface{}
				if err := util.UnmarshalJSON(data, &result); err != nil {
					return err
				}

				results = append(results, result)
			}

			if len(results) > 0 {
				m["documents"] = results
			}

			return nil
		}

		var o options.FindOneOptions
		if opt.Len() > 0 {
			v, err := ast.JSON(opt)
			if err != nil {
				return err
			}

			data, err := json.Marshal(v)
			if err != nil {
				return err
			}

			if err := json.Unmarshal(data, &o); err != nil {
				return err
			}
		}

		var doc bson.M
		if err := coll.FindOne(bctx.Context, filter, &o).Decode(&doc); errors.Is(err, mongo.ErrNoDocuments) {
			return nil
		} else if err != nil {
			return err
		}

		data, err = bson.MarshalExtJSON(doc, canonical, false)
		if err != nil {
			return err
		}

		var result interface{}
		if err := util.UnmarshalJSON(data, &result); err != nil {
			return err
		}

		m["document"] = result

		return nil
	}()

	if queryErr != nil {
		if !raiseError {
			// Unpack the driver specific error type to
			// get more details, if possible.

			e := map[string]interface{}{}
			v := reflect.ValueOf(queryErr)

			if v.Kind() == reflect.Struct {
				if c := v.FieldByName("Code"); c.CanInt() {
					e["code"] = c.Int()
				}
				if m := v.FieldByName("Message"); m.Kind() == reflect.String {
					e["message"] = m.Interface()
				}
			} else {
				e["message"] = string(queryErr.Error())
			}

			m["error"] = e
			queryErr = nil
		} else {
			return handleBuiltinErr(mongoDBSendName, bctx.Location, queryErr)
		}
	}

	responseObj, err := ast.InterfaceToValue(m)
	if err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	if err := insertCaches(bctx, cacheKey, responseObj.(ast.Object), queryErr, interQueryCacheEnabled, ttl, mongoDBSendBuiltinCacheKey); err != nil {
		return handleBuiltinErr(mongoDBSendName, bctx.Location, err)
	}

	bctx.Metrics.Timer(mongoDBSendLatencyMetricKey).Stop()

	return iter(ast.NewTerm(responseObj))
}

func (p *mongoDBClientPool) Get(ctx context.Context, uri string) (*mongo.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := mongoDBClientKey{
		uri,
	}
	client, ok := p.clients[key]
	if ok {
		return client, nil
	}

	p.mu.Unlock()
	client, err := p.open(ctx, uri)
	p.mu.Lock()

	if err != nil {
		return nil, err
	}

	if existing, ok := p.clients[key]; ok {
		return existing, nil
	}

	p.clients[key] = client
	return client, nil
}

func (p *mongoDBClientPool) open(ctx context.Context, uri string) (*mongo.Client, error) {
	return mongo.Connect(ctx, options.Client().ApplyURI(uri))
}

func init() {
	topdown.RegisterBuiltinFunc(mongoDBSendName, builtinMongoDBSend)
}
