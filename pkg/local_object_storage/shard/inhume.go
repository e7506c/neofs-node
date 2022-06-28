package shard

import (
	"context"
	"errors"
	"fmt"

	meta "github.com/nspcc-dev/neofs-node/pkg/local_object_storage/metabase"
	"github.com/nspcc-dev/neofs-node/pkg/local_object_storage/shard/mode"
	oid "github.com/nspcc-dev/neofs-sdk-go/object/id"
	"go.uber.org/zap"
)

// InhumePrm encapsulates parameters for inhume operation.
type InhumePrm struct {
	target       []oid.Address
	tombstone    *oid.Address
	forceRemoval bool
}

// InhumeRes encapsulates results of inhume operation.
type InhumeRes struct{}

// SetTarget sets a list of objects that should be inhumed and tombstone address
// as the reason for inhume operation.
//
// tombstone should not be nil, addr should not be empty.
// Should not be called along with MarkAsGarbage.
func (p *InhumePrm) SetTarget(tombstone oid.Address, addrs ...oid.Address) {
	p.target = addrs
	p.tombstone = &tombstone
}

// MarkAsGarbage marks object to be physically removed from shard.
//
// Should not be called along with SetTarget.
func (p *InhumePrm) MarkAsGarbage(addr ...oid.Address) {
	if p != nil {
		p.target = addr
		p.tombstone = nil
	}
}

// ForceRemoval forces object removing despite any restrictions imposed
// on deleting that object. Expected to be used only in control service.
func (p *InhumePrm) ForceRemoval() {
	if p != nil {
		p.tombstone = nil
		p.forceRemoval = true
	}
}

// ErrLockObjectRemoval is returned when inhume operation is being
// performed on lock object, and it is not a forced object removal.
var ErrLockObjectRemoval = meta.ErrLockObjectRemoval

// Inhume calls metabase. Inhume method to mark object as removed. It won't be
// removed physically from blobStor and metabase until `Delete` operation.
//
// Allows inhuming non-locked objects only. Returns apistatus.ObjectLocked
// if at least one object is locked.
//
// Returns ErrReadOnlyMode error if shard is in "read-only" mode.
func (s *Shard) Inhume(prm InhumePrm) (InhumeRes, error) {
	if s.GetMode() != mode.ReadWrite {
		return InhumeRes{}, ErrReadOnlyMode
	}

	if s.hasWriteCache() {
		for i := range prm.target {
			_ = s.writeCache.Delete(prm.target[i])
		}
	}

	var metaPrm meta.InhumePrm
	metaPrm.SetAddresses(prm.target...)
	metaPrm.SetLockObjectHandling()

	if prm.tombstone != nil {
		metaPrm.SetTombstoneAddress(*prm.tombstone)
	} else {
		metaPrm.SetGCMark()
	}

	if prm.forceRemoval {
		metaPrm.SetForceGCMark()
	}

	res, err := s.metaBase.Inhume(metaPrm)
	if err != nil {
		if errors.Is(err, meta.ErrLockObjectRemoval) {
			return InhumeRes{}, ErrLockObjectRemoval
		}

		s.log.Debug("could not mark object to delete in metabase",
			zap.String("error", err.Error()),
		)

		return InhumeRes{}, fmt.Errorf("metabase inhume: %w", err)
	}

	if deletedLockObjs := res.DeletedLockObjects(); len(deletedLockObjs) != 0 {
		s.deletedLockCallBack(context.Background(), deletedLockObjs)
	}

	return InhumeRes{}, nil
}
