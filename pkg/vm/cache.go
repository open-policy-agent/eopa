package vm

import (
	"context"
	"encoding/json"
	"fmt"
	gstrings "strings"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/config"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/topdown/cache"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	"github.com/styrainc/enterprise-opa-private/pkg/storage/ptr"
)

type (
	// CacheHook receives OPA configuration change callbacks and
	// holds the current cache configuration.
	CacheHook struct {
		config atomic.Pointer[cacheConfig] // nil if not enabled
	}

	cacheConfig struct {
		InputPaths []storage.Path
		TTL        time.Duration
	}

	// CacheConfig is under "extra/eval_cache" in OPA config.
	CacheConfig struct {
		Enabled    bool     `json:"enabled"`
		InputPaths []string `json:"input_paths"`
		TTL        string   `json:"ttl"`
	}

	// evalCacheResult holds the cache evaluation result. In the
	// cache, the keys include the input field values the result
	// depends on.
	evalCacheResult struct {
		expires time.Time
		value   string
	}
)

const defaultEvalCacheTTL = "10s"

var hook CacheHook

func NewCacheHook() *CacheHook {
	return &hook
}

func (c *CacheHook) OnConfigDiscovery(ctx context.Context, conf *config.Config) (*config.Config, error) {
	return c.onConfig(ctx, conf)
}

func (c *CacheHook) OnConfig(ctx context.Context, conf *config.Config) (*config.Config, error) {
	return c.onConfig(ctx, conf)
}

func (c *CacheHook) onConfig(_ context.Context, conf *config.Config) (*config.Config, error) {
	if conf.Extra["eval_cache"] == nil {
		return conf, nil
	}

	config := CacheConfig{TTL: defaultEvalCacheTTL}
	if err := json.Unmarshal(conf.Extra["eval_cache"], &config); err != nil {
		return conf, err
	}

	if !config.Enabled {
		c.config.Store(nil)
		return conf, nil
	}

	paths := make([]storage.Path, 0, len(config.InputPaths))

	for _, p := range config.InputPaths {
		path, ok := storage.ParsePath(gstrings.TrimSpace(p))
		if !ok {
			return conf, fmt.Errorf("invalid cache input path: %v", p)
		}

		paths = append(paths, path)
	}

	ttl, err := time.ParseDuration(config.TTL)
	if err != nil {
		return conf, err
	}

	c.config.Store(&cacheConfig{
		InputPaths: paths,
		TTL:        ttl,
	})

	return conf, nil
}

// getEvalCacheKey converts the provided input (in binary JSON format)
// to a cache key.
func (vm *VM) getEvalCacheKey(ctx context.Context, plan int, input *interface{}) (ast.Object, error) {
	if input == nil {
		return nil, nil
	}

	config := hook.config.Load()
	if config == nil {
		return nil, nil
	}

	// Convert the input field values to a cache key, which is an
	// AST array. If the path is not found in the input, use an
	// AST set as a special marker to indicate a value found for
	// the path; the input is JSON and hence can't hold a set.

	values := make([]*ast.Term, 0, len(config.InputPaths))

	for _, path := range config.InputPaths {
		node := *input
	next:
		for i := range path {
			key := path[i]
			switch curr := node.(type) {
			case Object: // no bjson.Object as the input is converted using FromInterface().
				var ok bool
				if node, ok, _ = curr.Get(ctx, bjson.NewString(key)); !ok {
					values = append(values, ast.NewTerm(ast.NewSet()))
					break next
				}
			case bjson.Array:
				pos, err := ptr.ValidateArrayIndex(curr, key, path)
				if err != nil {
					values = append(values, ast.NewTerm(ast.NewSet()))
					break next
				}
				node = curr.Value(pos)
			default:
				values = append(values, ast.NewTerm(ast.NewSet()))
				break next
			}
		}

		v, err := vm.ops.ToAST(ctx, node)
		if err != nil {
			return nil, err
		}

		values = append(values, ast.NewTerm(v))
	}

	return ast.NewObject(
		[2]*ast.Term{ast.StringTerm("vm"), ast.UIntNumberTerm(uint64(uintptr(unsafe.Pointer(vm))))},
		[2]*ast.Term{ast.StringTerm("plan"), ast.IntNumberTerm(plan)},
		[2]*ast.Term{ast.StringTerm("input_fields"), ast.NewTerm(ast.NewArray(values...))},
	), nil
}

func (vm *VM) checkEvalCache(cache cache.InterQueryCache, key ast.Object, time time.Time) (ast.Value, bool) {
	if cache == nil || key == nil {
		return nil, false
	}

	value, ok := cache.Get(key)
	if !ok {
		return nil, false
	}

	result := value.(evalCacheResult)
	if result.expires.Before(time) {
		return nil, false
	}

	return result.Value(), true
}

func (vm *VM) putEvalCache(cache cache.InterQueryCache, key ast.Object, value ast.Value, time time.Time) {
	if cache == nil || key == nil {
		return
	}

	config := hook.config.Load()
	if config == nil {
		return
	}

	expires := time.Add(config.TTL)
	cache.Insert(key, newEvalCacheResult(value, expires))
}

func newEvalCacheResult(value ast.Value, expires time.Time) evalCacheResult {
	return evalCacheResult{value: value.String(), expires: expires}
}

func (e evalCacheResult) SizeInBytes() int64 {
	return int64(len(e.value))
}

func (e evalCacheResult) Clone() (cache.InterQueryCacheValue, error) {
	return e, nil
}

func (e evalCacheResult) Value() ast.Value {
	return ast.MustParseTerm(string(e.value)).Value
}
