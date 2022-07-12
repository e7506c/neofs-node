package shard

import (
	"fmt"

	meta "github.com/nspcc-dev/neofs-node/pkg/local_object_storage/metabase"
	"github.com/nspcc-dev/neofs-node/pkg/local_object_storage/writecache"
	objectSDK "github.com/nspcc-dev/neofs-sdk-go/object"
	oid "github.com/nspcc-dev/neofs-sdk-go/object/id"
)

// HeadPrm groups the parameters of Head operation.
type HeadPrm struct {
	addr oid.Address
	raw  bool
}

// HeadRes groups the resulting values of Head operation.
type HeadRes struct {
	obj *objectSDK.Object
}

// WithAddress is a Head option to set the address of the requested object.
//
// Option is required.
func (p *HeadPrm) WithAddress(addr oid.Address) {
	if p != nil {
		p.addr = addr
	}
}

// WithRaw is a Head option to set raw flag value. If flag is unset, then Head
// returns header of virtual object, otherwise it returns SplitInfo of virtual
// object.
func (p *HeadPrm) WithRaw(raw bool) {
	if p != nil {
		p.raw = raw
	}
}

// Object returns the requested object header.
func (r HeadRes) Object() *objectSDK.Object {
	return r.obj
}

// Head reads header of the object from the shard.
//
// Returns any error encountered.
//
// Returns an error of type apistatus.ObjectNotFound if object is missing in Shard.
// Returns an error of type apistatus.ObjectAlreadyRemoved if the requested object has been marked as removed in shard.
func (s *Shard) Head(prm HeadPrm) (HeadRes, error) {
	// object can be saved in write-cache (if enabled) or in metabase

	if s.hasWriteCache() {
		// try to read header from write-cache
		header, err := s.writeCache.Head(prm.addr)
		if err == nil {
			return HeadRes{
				obj: header,
			}, nil
		} else if !writecache.IsErrNotFound(err) {
			// in this case we think that object is presented in write-cache, but corrupted
			return HeadRes{}, fmt.Errorf("could not read header from write-cache: %w", err)
		}

		// otherwise object seems to be flushed to metabase
	}

	var headParams meta.GetPrm
	headParams.SetAddress(prm.addr)
	headParams.SetRaw(prm.raw)

	res, err := s.metaBase.Get(headParams)
	if err != nil {
		return HeadRes{}, err
	}

	return HeadRes{
		obj: res.Header(),
	}, nil
}
