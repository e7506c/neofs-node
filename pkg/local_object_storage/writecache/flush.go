package writecache

import (
	"sync"
	"time"

	"github.com/mr-tron/base58"
	"github.com/nspcc-dev/neofs-node/pkg/local_object_storage/blobovnicza"
	"github.com/nspcc-dev/neofs-node/pkg/local_object_storage/blobstor"
	"github.com/nspcc-dev/neofs-node/pkg/local_object_storage/blobstor/fstree"
	meta "github.com/nspcc-dev/neofs-node/pkg/local_object_storage/metabase"
	"github.com/nspcc-dev/neofs-sdk-go/object"
	oid "github.com/nspcc-dev/neofs-sdk-go/object/id"
	"go.etcd.io/bbolt"
	"go.uber.org/zap"
)

const (
	// flushBatchSize is amount of keys which will be read from cache to be flushed
	// to the main storage. It is used to reduce contention between cache put
	// and cache persist.
	flushBatchSize = 512
	// defaultFlushWorkersCount is number of workers for putting objects in main storage.
	defaultFlushWorkersCount = 20
	// defaultFlushInterval is default time interval between successive flushes.
	defaultFlushInterval = time.Second
)

// flushLoop periodically flushes changes from the database to memory.
func (c *cache) flushLoop() {
	var wg sync.WaitGroup

	for i := 0; i < c.workersCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.flushWorker(i)
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		c.flushBigObjects()
	}()

	tt := time.NewTimer(defaultFlushInterval)
	defer tt.Stop()

	for {
		select {
		case <-tt.C:
			c.flush()
			tt.Reset(defaultFlushInterval)
		case <-c.closeCh:
			c.log.Debug("waiting for workers to quit")
			wg.Wait()
			return
		}
	}
}

func (c *cache) flush() {
	lastKey := []byte{}
	var m []objectInfo
	for {
		m = m[:0]
		sz := 0

		c.modeMtx.RLock()
		if c.readOnly() {
			c.modeMtx.RUnlock()
			time.Sleep(time.Second)
			continue
		}

		// We put objects in batches of fixed size to not interfere with main put cycle a lot.
		_ = c.db.View(func(tx *bbolt.Tx) error {
			b := tx.Bucket(defaultBucket)
			cs := b.Cursor()
			for k, v := cs.Seek(lastKey); k != nil && len(m) < flushBatchSize; k, v = cs.Next() {
				if _, ok := c.flushed.Peek(string(k)); ok {
					continue
				}

				sz += len(k) + len(v)
				m = append(m, objectInfo{
					addr: string(k),
					data: cloneBytes(v),
				})
			}
			return nil
		})

		for i := range m {
			obj := object.New()
			if err := obj.Unmarshal(m[i].data); err != nil {
				continue
			}

			select {
			case c.flushCh <- obj:
			case <-c.closeCh:
				c.modeMtx.RUnlock()
				return
			}
		}

		if len(m) == 0 {
			c.modeMtx.RUnlock()
			break
		}

		c.evictObjects(len(m))
		for i := range m {
			c.flushed.Add(m[i].addr, true)
		}
		c.modeMtx.RUnlock()

		c.log.Debug("flushed items from write-cache",
			zap.Int("count", len(m)),
			zap.String("start", base58.Encode(lastKey)))
	}
}

func (c *cache) flushBigObjects() {
	tick := time.NewTicker(defaultFlushInterval * 10)
	for {
		select {
		case <-tick.C:
			c.modeMtx.RLock()
			if c.readOnly() {
				c.modeMtx.RUnlock()
				break
			}

			evictNum := 0

			var prm fstree.IterationPrm
			prm.WithLazyHandler(func(addr oid.Address, f func() ([]byte, error)) error {
				sAddr := addr.EncodeToString()

				if _, ok := c.store.flushed.Peek(sAddr); ok {
					return nil
				}

				data, err := f()
				if err != nil {
					c.log.Error("can't read a file", zap.Stringer("address", addr))
					return nil
				}

				c.mtx.Lock()
				_, compress := c.compressFlags[sAddr]
				c.mtx.Unlock()

				if _, err := c.blobstor.PutRaw(addr, data, compress); err != nil {
					c.log.Error("cant flush object to blobstor", zap.Error(err))
					return nil
				}

				if compress {
					c.mtx.Lock()
					delete(c.compressFlags, sAddr)
					c.mtx.Unlock()
				}

				// mark object as flushed
				c.store.flushed.Add(sAddr, false)

				evictNum++

				return nil
			})

			_ = c.fsTree.Iterate(prm)

			// evict objects which were successfully written to BlobStor
			c.evictObjects(evictNum)
			c.modeMtx.RUnlock()
		case <-c.closeCh:
		}
	}
}

// flushWorker runs in a separate goroutine and write objects to the main storage.
// If flushFirst is true, flushing objects from cache database takes priority over
// putting new objects.
func (c *cache) flushWorker(num int) {
	priorityCh := c.directCh
	switch num % 3 {
	case 0:
		priorityCh = c.flushCh
	case 1:
		priorityCh = c.metaCh
	}

	var obj *object.Object
	for {
		metaOnly := false

		// Give priority to direct put.
		// TODO(fyrchik): #1150 do this once in N iterations depending on load
		select {
		case obj = <-priorityCh:
			metaOnly = num%3 == 1
		default:
			select {
			case obj = <-c.directCh:
			case obj = <-c.flushCh:
			case obj = <-c.metaCh:
				metaOnly = true
			case <-c.closeCh:
				return
			}
		}

		err := c.writeObject(obj, metaOnly)
		if err != nil {
			c.log.Error("can't flush object to the main storage", zap.Error(err))
		}
	}
}

// writeObject is used to write object directly to the main storage.
func (c *cache) writeObject(obj *object.Object, metaOnly bool) error {
	var id *blobovnicza.ID

	if !metaOnly {
		var prm blobstor.PutPrm
		prm.SetObject(obj)

		res, err := c.blobstor.Put(prm)
		if err != nil {
			return err
		}

		id = res.BlobovniczaID()
	}

	var pPrm meta.PutPrm
	pPrm.SetObject(obj)
	pPrm.SetBlobovniczaID(id)

	_, err := c.metabase.Put(pPrm)
	return err
}

func cloneBytes(a []byte) []byte {
	b := make([]byte, len(a))
	copy(b, a)
	return b
}
