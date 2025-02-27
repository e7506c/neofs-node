package writecache

import (
	"errors"

	"github.com/nspcc-dev/neofs-node/pkg/core/object"
	storagelog "github.com/nspcc-dev/neofs-node/pkg/local_object_storage/internal/log"
	objectSDK "github.com/nspcc-dev/neofs-sdk-go/object"
)

// ErrBigObject is returned when object is too big to be placed in cache.
var ErrBigObject = errors.New("too big object")

// Put puts object to write-cache.
func (c *cache) Put(o *objectSDK.Object) error {
	c.modeMtx.RLock()
	defer c.modeMtx.RUnlock()
	if c.readOnly() {
		return ErrReadOnly
	}

	sz := uint64(o.ToV2().StableSize())
	if sz > c.maxObjectSize {
		return ErrBigObject
	}

	data, err := o.Marshal()
	if err != nil {
		return err
	}

	oi := objectInfo{
		addr: object.AddressOf(o).EncodeToString(),
		obj:  o,
		data: data,
	}

	c.mtx.Lock()

	if sz <= c.smallObjectSize && c.curMemSize+sz <= c.maxMemSize {
		c.curMemSize += sz
		c.mem = append(c.mem, oi)

		c.mtx.Unlock()

		storagelog.Write(c.log, storagelog.AddressField(oi.addr), storagelog.OpField("in-mem PUT"))

		return nil
	}

	c.mtx.Unlock()

	if sz <= c.smallObjectSize {
		c.persistSmallObjects([]objectInfo{oi})
	} else {
		c.persistBigObject(oi)
	}
	return nil
}
