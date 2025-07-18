//go:build use_opa_fork

package storage

import (
	"context"
	"fmt"

	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/storage/disk"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/styrainc/enterprise-opa-private/pkg/storage/inmem"
	"github.com/styrainc/enterprise-opa-private/pkg/storage/sql"
)

func init() {
	bundle.RegisterStoreFunc(New)
}

func New2(ctx context.Context, logger logging.Logger, prom prometheus.Registerer, config []byte, id string) (storage.Store, error) {
	diskOptions, err := disk.OptionsFromConfig(config, id)
	if err != nil {
		return nil, err
	}

	sqlOptions, err := sql.OptionsFromConfig(ctx, config, id)
	if err != nil {
		return nil, err
	}

	root := inmem.New()
	var options Options

	if diskOptions != nil {
		// storage/disk replaces the in-memory root. Convert
		// the disk partitions to tree but for now don't
		// support wildcards in the middle of the partition
		// paths.
		root, err = disk.New(ctx, logger, prom, *diskOptions)
		if err != nil {
			return nil, err
		}

		var paths [][2]storage.Path
		for _, path := range diskOptions.Partitions {
			wildcards := 0
			for i := len(path) - 1; i >= 0 && path[i] == "*"; i-- {
				wildcards++
			}

			n := 0
			for _, seg := range path {
				if seg == "*" {
					n++
				}
			}

			if n > wildcards {
				return nil, fmt.Errorf("only wildcards in the end of partition path supported")
			}

			paths = append(paths, [2]storage.Path{
				path[:len(path)-wildcards],
				path,
			})
		}

		options.Stores = []StoreOptions{
			{
				Paths: paths,
				New: func(context.Context, logging.Logger, prometheus.Registerer, interface{}) (storage.Store, error) {
					return root, nil
				},
				Options: *diskOptions,
			},
		}

	} else if sqlOptions != nil {
		// storage/disk uses the in-memory store for
		// everything but the persisted tables configured.
		var paths [][2]storage.Path
		for _, table := range sqlOptions.Tables {
			paths = append(paths, [2]storage.Path{
				table.AbsolutePath(nil),
				table.Path,
			})
		}

		options.Stores = []StoreOptions{
			{
				Paths:   paths,
				New:     sql.New,
				Options: *sqlOptions,
			},
		}
	}

	return newInternal(ctx, logger, prom, root, options)
}
