// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package inmem

import (
	"container/list"
	"strconv"

	bjson "github.com/styrainc/enterprise-opa-private/pkg/json"
	"github.com/styrainc/enterprise-opa-private/pkg/storage/errors"
	"github.com/styrainc/enterprise-opa-private/pkg/storage/ptr"

	"github.com/open-policy-agent/opa/storage"
)

// transaction implements the low-level read/write operations on the in-memory
// store and contains the state required for pending transactions.
//
// For write transactions, the struct contains a logical set of updates
// performed by write operations in the transaction. Each write operation
// compacts the set such that two updates never overlap:
//
// - If new update path is a prefix of existing update path, existing update is
// removed, new update is added.
//
// - If existing update path is a prefix of new update path, existing update is
// modified.
//
// - Otherwise, new update is added.
//
// Read transactions do not require any special handling and simply passthrough
// to the underlying store. Read transactions do not support upgrade.
type transaction struct {
	xid      uint64
	write    bool
	stale    bool
	db       *store
	updates  *list.List
	policies map[string]policyUpdate
	context  *storage.Context
}

type policyUpdate struct {
	value  []byte
	remove bool
}

func newTransaction(xid uint64, write bool, context *storage.Context, db *store) *transaction {
	return &transaction{
		xid:      xid,
		write:    write,
		db:       db,
		policies: map[string]policyUpdate{},
		updates:  list.New(),
		context:  context,
	}
}

func (txn *transaction) ID() uint64 {
	return txn.xid
}

func (txn *transaction) Write(op storage.PatchOp, path storage.Path, value bjson.Json) error {

	if !txn.write {
		return &storage.Error{
			Code:    storage.InvalidTransactionErr,
			Message: "data write during read transaction",
		}
	}

	if len(path) == 0 {
		return txn.updateRoot(op, value)
	}

	for curr := txn.updates.Front(); curr != nil; {
		update := curr.Value.(*update)

		// Check if new update masks existing update exactly. In this case, the
		// existing update can be removed and no other updates have to be
		// visited (because no two updates overlap.)
		if update.path.Equal(path) {
			if update.remove {
				if op != storage.AddOp {
					return errors.NewNotFoundError(path)
				}
			}
			txn.updates.Remove(curr)
			break
		}

		// Check if new update masks existing update. In this case, the
		// existing update has to be removed but other updates may overlap, so
		// we must continue.
		if update.path.HasPrefix(path) {
			remove := curr
			curr = curr.Next()
			txn.updates.Remove(remove)
			continue
		}

		// Check if new update modifies existing update. In this case, the
		// existing update is mutated.
		if path.HasPrefix(update.path) {
			if update.remove {
				return errors.NewNotFoundError(path)
			}
			suffix := path[len(update.path):]
			newUpdate, err := newUpdate(update.value, op, suffix, 0, value)
			if err != nil {
				return err
			}
			update.value = newUpdate.Apply(update.value)
			return nil
		}

		curr = curr.Next()
	}

	update, err := newUpdate(txn.db.data, op, path, 0, value)
	if err != nil {
		return err
	}

	txn.updates.PushFront(update)
	return nil
}

func (txn *transaction) updateRoot(op storage.PatchOp, value bjson.Json) error {
	if op == storage.RemoveOp {
		return invalidPatchError(rootCannotBeRemovedMsg)
	}
	if _, ok := value.(bjson.Object); !ok {
		return invalidPatchError(rootMustBeObjectMsg)
	}
	txn.updates.Init()
	txn.updates.PushFront(&update{
		path:   storage.Path{},
		remove: false,
		value:  value,
	})
	return nil
}

func (txn *transaction) Commit() (result storage.TriggerEvent) {
	result.Context = txn.context
	for curr := txn.updates.Front(); curr != nil; curr = curr.Next() {
		action := curr.Value.(*update)
		updated := action.Apply(txn.db.data)
		txn.db.data = updated

		result.Data = append(result.Data, storage.DataEvent{
			Path:    action.path,
			Data:    action.value,
			Removed: action.remove,
		})
	}
	for id, update := range txn.policies {
		if update.remove {
			delete(txn.db.policies, id)
		} else {
			txn.db.policies[id] = update.value
		}

		result.Policy = append(result.Policy, storage.PolicyEvent{
			ID:      id,
			Data:    update.value,
			Removed: update.remove,
		})
	}
	return result
}

func (txn *transaction) Read(path storage.Path) (interface{}, error) {

	if !txn.write {
		return ptr.Ptr(txn.db.data, path)
	}

	merge := []*update{}

	for curr := txn.updates.Front(); curr != nil; curr = curr.Next() {

		update := curr.Value.(*update)

		if path.HasPrefix(update.path) {
			if update.remove {
				return nil, errors.NewNotFoundError(path)
			}
			return ptr.Ptr(update.value, path[len(update.path):])
		}

		if update.path.HasPrefix(path) {
			merge = append(merge, update)
		}
	}

	v, err := ptr.Ptr(txn.db.data, path)

	if err != nil {
		return nil, err
	}

	data := v.(bjson.Json)

	if len(merge) == 0 {
		return data, nil
	}

	cpy := data.Clone(true).(bjson.Json) // TODO?

	for _, update := range merge {
		cpy = update.Relative(path).Apply(cpy)
	}

	return cpy, nil
}

func (txn *transaction) ListPolicies() []string {
	var ids []string
	for id := range txn.db.policies {
		if _, ok := txn.policies[id]; !ok {
			ids = append(ids, id)
		}
	}
	for id, update := range txn.policies {
		if !update.remove {
			ids = append(ids, id)
		}
	}
	return ids
}

func (txn *transaction) GetPolicy(id string) ([]byte, error) {
	if update, ok := txn.policies[id]; ok {
		if !update.remove {
			return update.value, nil
		}
		return nil, errors.NewNotFoundErrorf("policy id %q", id)
	}
	if exist, ok := txn.db.policies[id]; ok {
		return exist, nil
	}
	return nil, errors.NewNotFoundErrorf("policy id %q", id)
}

func (txn *transaction) UpsertPolicy(id string, bs []byte) error {
	if !txn.write {
		return &storage.Error{
			Code:    storage.InvalidTransactionErr,
			Message: "policy write during read transaction",
		}
	}
	txn.policies[id] = policyUpdate{bs, false}
	return nil
}

func (txn *transaction) DeletePolicy(id string) error {
	if !txn.write {
		return &storage.Error{
			Code:    storage.InvalidTransactionErr,
			Message: "policy write during read transaction",
		}
	}
	txn.policies[id] = policyUpdate{nil, true}
	return nil
}

// update contains state associated with an update to be applied to the
// in-memory data store.
type update struct {
	path   storage.Path // data path modified by update
	remove bool         // indicates whether update removes the value at path
	value  bjson.Json   // value to add/replace at path (ignored if remove is true)
}

func newUpdate(data bjson.Json, op storage.PatchOp, path storage.Path, idx int, value bjson.Json) (*update, error) {
	switch data := data.(type) {
	case bjson.Object:
		return newUpdateObject(data, op, path, idx, value)
	case bjson.Array:
		return newUpdateArray(data, op, path, idx, value)
	case bjson.Null, bjson.Bool, bjson.Float, *bjson.String:
		return nil, errors.NewNotFoundError(path)
	}

	return nil, &storage.Error{
		Code:    storage.InternalErr,
		Message: "invalid data value encountered",
	}
}

func newUpdateArray(data bjson.Array, op storage.PatchOp, path storage.Path, idx int, value bjson.Json) (*update, error) {
	if idx == len(path)-1 {
		if path[idx] == "-" || path[idx] == strconv.Itoa(data.Len()) {
			if op != storage.AddOp {
				return nil, invalidPatchError("%v: invalid patch path", path)
			}
			cpy := data.Slice(0, data.Len()).Clone(false).(bjson.Array)
			cpy = cpy.Append(value)
			return &update{path[:len(path)-1], false, cpy}, nil
		}

		pos, err := ptr.ValidateArrayIndex(data, path[idx], path)
		if err != nil {
			return nil, err
		}

		switch op {
		case storage.AddOp:
			cpy := data.Slice(0, pos).Clone(false).(bjson.Array)
			cpy = cpy.Append(value)
			for i := pos; i < data.Len(); i++ {
				cpy = cpy.Append(data.Value(i))
			}
			return &update{path[:len(path)-1], false, cpy}, nil

		case storage.RemoveOp:
			cpy := data.Slice(0, data.Len()).Clone(false).(bjson.Array)
			cpy = cpy.RemoveIdx(pos).(bjson.Array)
			return &update{path[:len(path)-1], false, cpy}, nil

		default:
			cpy := data.Slice(0, data.Len()).Clone(false).(bjson.Array)
			cpy = cpy.SetIdx(pos, value).(bjson.Array)
			return &update{path[:len(path)-1], false, cpy}, nil
		}
	}

	pos, err := ptr.ValidateArrayIndex(data, path[idx], path)
	if err != nil {
		return nil, err
	}

	return newUpdate(data.Value(pos), op, path, idx+1, value)
}

func newUpdateObject(data bjson.Object, op storage.PatchOp, path storage.Path, idx int, value bjson.Json) (*update, error) {
	if idx == len(path)-1 {
		switch op {
		case storage.ReplaceOp, storage.RemoveOp:
			if v := data.Value(path[idx]); v == nil {
				return nil, errors.NewNotFoundError(path)
			}
		}
		return &update{path, op == storage.RemoveOp, value}, nil
	}

	if data := data.Value(path[idx]); data != nil {
		return newUpdate(data, op, path, idx+1, value)
	}

	return nil, errors.NewNotFoundError(path)
}
func (u *update) Apply(data bjson.Json) bjson.Json {
	if len(u.path) == 0 {
		return u.value
	}

	if u.remove {
		data, _ = u.set(data, u.path, nil) // TODO
		return data
	}

	data, _ = u.set(data, u.path, u.value)
	return data
}

func (u *update) Relative(path storage.Path) *update {
	cpy := *u
	cpy.path = cpy.path[len(path):]
	return &cpy
}

func (u *update) set(data bjson.Json, path storage.Path, value bjson.Json) (bjson.Json, bool) {
	if len(path) == 0 {
		return value, true
	}

	switch parent := data.(type) {
	case bjson.Object:
		existing := parent.Value(path[0])
		if updated, ok := u.set(existing, path[1:], value); updated == nil {
			return parent.Remove(path[0]), true
		} else if ok {
			parent, _ = parent.Set(path[0], updated)
			return parent, true // TODO
		}

		return parent, false

	case bjson.Array:
		idx, err := strconv.Atoi(path[0])
		if err != nil {
			panic(err)
		}

		existing := parent.Value(idx)
		if updated, ok := u.set(existing, path[1:], value); updated == nil {
			panic("not reached")
		} else if ok {
			parent = parent.SetIdx(idx, updated).(bjson.Array)
		}

		return parent, false

	default:
		panic("not reached")
	}
}
