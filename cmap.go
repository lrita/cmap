package cmap

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/lrita/runtime2"
)

const (
	mInitialSize           = 1 << 4
	mOverflowThreshold     = 1 << 5
	mOverflowGrowThreshold = 1 << 7
)

// Cmap is a "thread" safe map of type AnyComparableType:Any.
// To avoid lock bottlenecks this map is dived to several map shards.
type Cmap struct {
	lock  sync.Mutex
	inode unsafe.Pointer // *inode
	count int64
}

type inode struct {
	mask            uintptr
	overflow        int64
	growThreshold   int64
	shrinkThreshold int64
	resizeInProgess int64
	pred            unsafe.Pointer // *inode
	buckets         []bucket
}

type entry struct {
	key, value interface{}
}

type bucket struct {
	lock   sync.RWMutex
	init   int64
	m      map[interface{}]interface{}
	frozen bool
}

// Store sets the value for a key.
func (m *Cmap) Store(key, value interface{}) {
	hash := runtime2.Hash(key)
	for {
		inode, b := m.getInodeAndBucket(hash)
		if b.tryStore(m, inode, false, key, value) {
			return
		}
	}
}

// Load returns the value stored in the map for a key, or nil if no
// value is present.
// The ok result indicates whether value was found in the map.
func (m *Cmap) Load(key interface{}) (value interface{}, ok bool) {
	hash := runtime2.Hash(key)
	_, b := m.getInodeAndBucket(hash)
	return b.tryLoad(key)
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
// The loaded result is true if the value was loaded, false if stored.
func (m *Cmap) LoadOrStore(key, value interface{}) (actual interface{}, loaded bool) {
	hash := runtime2.Hash(key)
	for {
		inode, b := m.getInodeAndBucket(hash)
		actual, loaded = b.tryLoad(key)
		if loaded {
			return
		}
		if b.tryStore(m, inode, true, key, value) {
			return value, false
		}
	}
}

// Delete deletes the value for a key.
func (m *Cmap) Delete(key interface{}) {
	hash := runtime2.Hash(key)
	for {
		inode, b := m.getInodeAndBucket(hash)
		if b.tryDelete(m, inode, key) {
			return
		}
	}
}

// Range calls f sequentially for each key and value present in the map.
// If f returns false, range stops the iteration.
//
// Range does not necessarily correspond to any consistent snapshot of the Map's
// contents: no key will be visited more than once, but if the value for any key
// is stored or deleted concurrently, Range may reflect any mapping for that key
// from any point during the Range call.
//
// Range may be O(N) with the number of elements in the map even if f returns
// false after a constant number of calls.
func (m *Cmap) Range(f func(key, value interface{}) bool) {
	n := m.getInode()
	for i := 0; i < len(n.buckets); i++ {
		b := &(n.buckets[i])
		if !b.inited() {
			n.initBucket(uintptr(i))
		}
		for _, e := range b.clone() {
			if !f(e.key, e.value) {
				return
			}
		}
	}
}

// Count returns the number of elements within the map.
func (m *Cmap) Count() int {
	return int(atomic.LoadInt64(&m.count))
}

// IsEmpty checks if map is empty.
func (m *Cmap) IsEmpty() bool {
	return m.Count() == 0
}

func (m *Cmap) getInode() *inode {
	n := (*inode)(atomic.LoadPointer(&m.inode))
	if n == nil {
		m.lock.Lock()
		n = (*inode)(atomic.LoadPointer(&m.inode))
		if n == nil {
			n = &inode{
				mask:            uintptr(mInitialSize - 1),
				growThreshold:   int64(mInitialSize * mOverflowThreshold),
				shrinkThreshold: 0,
				buckets:         make([]bucket, mInitialSize),
			}
			atomic.StorePointer(&m.inode, unsafe.Pointer(n))
		}
		m.lock.Unlock()
	}
	return n
}

func (m *Cmap) getInodeAndBucket(hash uintptr) (*inode, *bucket) {
	n := m.getInode()
	i := hash & n.mask
	b := &(n.buckets[i])
	if !b.inited() {
		n.initBucket(i)
	}
	return n, b
}

func (n *inode) initBuckets() {
	for i := range n.buckets {
		n.initBucket(uintptr(i))
	}
	atomic.StorePointer(&n.pred, nil)
}

func (n *inode) initBucket(i uintptr) {
	b := &(n.buckets[i])
	b.lock.Lock()
	if b.inited() {
		b.lock.Unlock()
		return
	}

	b.m = make(map[interface{}]interface{})
	p := (*inode)(atomic.LoadPointer(&n.pred)) // predecessor
	if p != nil {
		if n.mask > p.mask {
			// Grow
			pb := &(p.buckets[i&p.mask])
			if !pb.inited() {
				p.initBucket(i & p.mask)
			}
			for k, v := range pb.freeze() {
				hash := runtime2.Hash(k)
				if hash&n.mask == i {
					b.m[k] = v
				}
			}
		} else {
			// Shrink
			pb0 := &(p.buckets[i])
			if !pb0.inited() {
				p.initBucket(i)
			}
			pb1 := &(p.buckets[i+uintptr(len(n.buckets))])
			if !pb1.inited() {
				p.initBucket(i + uintptr(len(n.buckets)))
			}
			for k, v := range pb0.freeze() {
				b.m[k] = v
			}
			for k, v := range pb1.freeze() {
				b.m[k] = v
			}
		}
		if len(b.m) > mOverflowThreshold {
			atomic.AddInt64(&n.overflow, int64(len(b.m)-mOverflowThreshold))
		}
	}

	atomic.StoreInt64(&b.init, 1)
	b.lock.Unlock()
}

func (b *bucket) inited() bool {
	return atomic.LoadInt64(&b.init) == 1
}

func (b *bucket) freeze() map[interface{}]interface{} {
	b.lock.Lock()
	b.frozen = true
	m := b.m
	b.lock.Unlock()
	return m
}

func (b *bucket) clone() []entry {
	b.lock.RLock()
	entries := make([]entry, 0, len(b.m))
	for k, v := range b.m {
		entries = append(entries, entry{key: k, value: v})
	}
	b.lock.RUnlock()
	return entries
}

func (b *bucket) tryLoad(key interface{}) (value interface{}, ok bool) {
	b.lock.RLock()
	value, ok = b.m[key]
	b.lock.RUnlock()
	return
}

func (b *bucket) tryStore(m *Cmap, n *inode, check bool, key, value interface{}) (done bool) {
	b.lock.Lock()
	if b.frozen {
		b.lock.Unlock()
		return
	}

	if check {
		if _, ok := b.m[key]; ok {
			b.lock.Unlock()
			return
		}
	}

	b.m[key] = value
	length := len(b.m)
	b.lock.Unlock()

	// Update counter
	grow := atomic.AddInt64(&m.count, 1) >= n.growThreshold
	if length > mOverflowThreshold {
		grow = grow || atomic.AddInt64(&n.overflow, 1) >= mOverflowGrowThreshold
	}

	// Grow
	if grow && atomic.CompareAndSwapInt64(&n.resizeInProgess, 0, 1) {
		nlen := len(n.buckets) << 1
		node := &inode{
			mask:            uintptr(nlen) - 1,
			pred:            unsafe.Pointer(n),
			growThreshold:   int64(nlen) * mOverflowThreshold,
			shrinkThreshold: int64(nlen) >> 1,
			buckets:         make([]bucket, nlen),
		}
		ok := atomic.CompareAndSwapPointer(&m.inode, unsafe.Pointer(n), unsafe.Pointer(node))
		if !ok {
			panic("BUG: failed swapping head")
		}
		go node.initBuckets()
	}

	return true
}

func (b *bucket) tryDelete(m *Cmap, n *inode, key interface{}) (done bool) {
	b.lock.Lock()
	if b.frozen {
		b.lock.Unlock()
		return
	}

	l0 := len(b.m)
	delete(b.m, key)
	length := len(b.m)
	b.lock.Unlock()

	if l0 == length {
		return true
	}

	// Update counter
	shrink := atomic.AddInt64(&m.count, -1) < n.shrinkThreshold
	if length >= mOverflowThreshold {
		atomic.AddInt64(&n.overflow, -1)
	}
	// Shrink
	if shrink && len(n.buckets) > mInitialSize && atomic.CompareAndSwapInt64(&n.resizeInProgess, 0, 1) {
		nlen := len(n.buckets) >> 1
		node := &inode{
			mask:            uintptr(nlen) - 1,
			pred:            unsafe.Pointer(n),
			growThreshold:   int64(nlen) * mOverflowThreshold,
			shrinkThreshold: int64(nlen) >> 1,
			buckets:         make([]bucket, nlen),
		}
		ok := atomic.CompareAndSwapPointer(&m.inode, unsafe.Pointer(n), unsafe.Pointer(node))
		if !ok {
			panic("BUG: failed swapping head")
		}
		go node.initBuckets()
	}
	return true
}
