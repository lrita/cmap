//go:build go1.18
// +build go1.18

package cmap

import (
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Map is a "thread" generics safe map of type AnyComparableType:Any
// (AnyComparableType exclude interface type).
// To avoid lock bottlenecks this map is dived to several map shards.
type Map[K comparable, V any] struct {
	lock  sync.Mutex
	inode unsafe.Pointer // *inode2
	typ   *rtype
	count int64
}

type bucket2[K comparable, V any] struct {
	lock   sync.RWMutex
	init   int64
	m      map[K]V
	frozen bool
}

type entry2[K any, V any] struct {
	key   K
	value V
}

type inode2[K comparable, V any] struct {
	mask             uintptr
	overflow         int64
	growThreshold    int64
	shrinkThreshold  int64
	resizeInProgress int64
	pred             unsafe.Pointer // *inode
	buckets          []bucket2[K, V]
}

// Store sets the value for a key.
func (m *Map[K, V]) Store(key K, value V) {
	hash := m.ehash(key)
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
func (m *Map[K, V]) Load(key K) (value V, ok bool) {
	hash := m.ehash(key)
	_, b := m.getInodeAndBucket(hash)
	return b.tryLoad(key)
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
// The loaded result is true if the value was loaded, false if stored.
func (m *Map[K, V]) LoadOrStore(key K, value V) (actual V, loaded bool) {
	hash := m.ehash(key)
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
func (m *Map[K, V]) Delete(key K) {
	hash := m.ehash(key)
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
func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	n := m.getInode()
	for i := 0; i < len(n.buckets); i++ {
		b := &(n.buckets[i])
		if !b.inited() {
			n.initBucket(m, uintptr(i))
		}
		for _, e := range b.clone() {
			if !f(e.key, e.value) {
				return
			}
		}
	}
}

// Count returns the number of elements within the map.
func (m *Map[K, V]) Count() int {
	return int(atomic.LoadInt64(&m.count))
}

// IsEmpty checks if map is empty.
func (m *Map[K, V]) IsEmpty() bool {
	return m.Count() == 0
}

func (m *Map[K, V]) getInode() *inode2[K, V] {
	n := (*inode2[K, V])(atomic.LoadPointer(&m.inode))
	if n == nil {
		m.lock.Lock()
		n = (*inode2[K, V])(atomic.LoadPointer(&m.inode))
		if n == nil {
			n = &inode2[K, V]{
				mask:            uintptr(mInitialSize - 1),
				growThreshold:   int64(mInitialSize * mOverflowThreshold),
				shrinkThreshold: 0,
				buckets:         make([]bucket2[K, V], mInitialSize),
			}
			atomic.StorePointer(&m.inode, unsafe.Pointer(n))
		}
		m.lock.Unlock()
	}
	return n
}

func (m *Map[K, V]) getInodeAndBucket(hash uintptr) (*inode2[K, V], *bucket2[K, V]) {
	n := m.getInode()
	i := hash & n.mask
	b := &(n.buckets[i])
	if !b.inited() {
		n.initBucket(m, i)
	}
	return n, b
}

func (n *inode2[K, V]) initBuckets(m *Map[K, V]) {
	for i := range n.buckets {
		n.initBucket(m, uintptr(i))
	}
	atomic.StorePointer(&n.pred, nil)
}

func (n *inode2[K, V]) initBucket(m *Map[K, V], i uintptr) {
	b := &(n.buckets[i])
	b.lock.Lock()
	if b.inited() {
		b.lock.Unlock()
		return
	}

	b.m = make(map[K]V)
	p := (*inode2[K, V])(atomic.LoadPointer(&n.pred)) // predecessor
	if p != nil {
		if n.mask > p.mask {
			// Grow
			pb := &(p.buckets[i&p.mask])
			if !pb.inited() {
				p.initBucket(m, i&p.mask)
			}
			for k, v := range pb.freeze() {
				hash := m.ehash(k)
				if hash&n.mask == i {
					b.m[k] = v
				}
			}
		} else {
			// Shrink
			pb0 := &(p.buckets[i])
			if !pb0.inited() {
				p.initBucket(m, i)
			}
			pb1 := &(p.buckets[i+uintptr(len(n.buckets))])
			if !pb1.inited() {
				p.initBucket(m, i+uintptr(len(n.buckets)))
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

func (b *bucket2[K, V]) inited() bool {
	return atomic.LoadInt64(&b.init) == 1
}

func (b *bucket2[K, V]) freeze() map[K]V {
	b.lock.Lock()
	b.frozen = true
	m := b.m
	b.lock.Unlock()
	return m
}

func (b *bucket2[K, V]) clone() []entry2[K, V] {
	b.lock.RLock()
	entries := make([]entry2[K, V], 0, len(b.m))
	for k, v := range b.m {
		entries = append(entries, entry2[K, V]{key: k, value: v})
	}
	b.lock.RUnlock()
	return entries
}

func (b *bucket2[K, V]) tryLoad(key K) (value V, ok bool) {
	b.lock.RLock()
	value, ok = b.m[key]
	b.lock.RUnlock()
	return
}

func (b *bucket2[K, V]) tryStore(m *Map[K, V], n *inode2[K, V], check bool, key K, value V) (done bool) {
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

	l0 := len(b.m) // Using length check existence is faster than accessing.
	b.m[key] = value
	length := len(b.m)
	b.lock.Unlock()

	if l0 == length {
		return true
	}

	// Update counter
	grow := atomic.AddInt64(&m.count, 1) >= n.growThreshold
	if length > mOverflowThreshold {
		grow = grow || atomic.AddInt64(&n.overflow, 1) >= mOverflowGrowThreshold
	}

	// Grow
	if grow && atomic.CompareAndSwapInt64(&n.resizeInProgress, 0, 1) {
		nlen := len(n.buckets) << 1
		node := &inode2[K, V]{
			mask:            uintptr(nlen) - 1,
			pred:            unsafe.Pointer(n),
			growThreshold:   int64(nlen) * mOverflowThreshold,
			shrinkThreshold: int64(nlen) >> 1,
			buckets:         make([]bucket2[K, V], nlen),
		}
		ok := atomic.CompareAndSwapPointer(&m.inode, unsafe.Pointer(n), unsafe.Pointer(node))
		if !ok {
			panic("BUG: failed swapping head")
		}
		go node.initBuckets(m)
	}

	return true
}

func (b *bucket2[K, V]) tryDelete(m *Map[K, V], n *inode2[K, V], key K) (done bool) {
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
	if shrink && len(n.buckets) > mInitialSize && atomic.CompareAndSwapInt64(&n.resizeInProgress, 0, 1) {
		nlen := len(n.buckets) >> 1
		node := &inode2[K, V]{
			mask:            uintptr(nlen) - 1,
			pred:            unsafe.Pointer(n),
			growThreshold:   int64(nlen) * mOverflowThreshold,
			shrinkThreshold: int64(nlen) >> 1,
			buckets:         make([]bucket2[K, V], nlen),
		}
		ok := atomic.CompareAndSwapPointer(&m.inode, unsafe.Pointer(n), unsafe.Pointer(node))
		if !ok {
			panic("BUG: failed swapping head")
		}
		go node.initBuckets(m)
	}
	return true
}

// tflag is used by an rtype to signal what extra type information is
// available in the memory directly following the rtype value.
//
// tflag values must be kept in sync with copies in:
//	cmd/compile/internal/reflectdata/reflect.go
//	cmd/link/internal/ld/decodesym.go
//	runtime/type.go
type tflag uint8

const (
	// tflagUncommon means that there is a pointer, *uncommonType,
	// just beyond the outer type structure.
	//
	// For example, if t.Kind() == Struct and t.tflag&tflagUncommon != 0,
	// then t has uncommonType data and it can be accessed as:
	//
	//	type tUncommon struct {
	//		structType
	//		u uncommonType
	//	}
	//	u := &(*tUncommon)(unsafe.Pointer(t)).u
	tflagUncommon tflag = 1 << 0

	// tflagExtraStar means the name in the str field has an
	// extraneous '*' prefix. This is because for most types T in
	// a program, the type *T also exists and reusing the str data
	// saves binary size.
	tflagExtraStar tflag = 1 << 1

	// tflagNamed means the type has a name.
	tflagNamed tflag = 1 << 2

	// tflagRegularMemory means that equal and hash functions can treat
	// this type as a single region of t.size bytes.
	tflagRegularMemory tflag = 1 << 3
)

// rtype is the common implementation of most values.
// It is embedded in other struct types.
//
// rtype must be kept in sync with ../runtime/type.go:/^type._type.
type rtype struct {
	size       uintptr
	ptrdata    uintptr // number of bytes in the type that can contain pointers
	hash       uint32  // hash of type; avoids computation in hash tables
	tflag      tflag   // extra type information flags
	align      uint8   // alignment of variable with this type
	fieldAlign uint8   // alignment of struct field with this type
	kind       uint8   // enumeration for C
}

//func (t *rtype) IsRegularMemory() bool {
//	return t.tflag&tflagRegularMemory != 0
//}

func (t *rtype) IsDirectIface() bool {
	const kindDirectIface = 1 << 5
	return t.kind&kindDirectIface != 0
}

// eface must be kept in sync with ../src/runtime/runtime2.go:/^eface.
type eface struct {
	typ  *rtype
	data unsafe.Pointer
}

func efaceOf(ep *any) *eface {
	return (*eface)(unsafe.Pointer(ep))
}

func (m *Map[K, V]) ehash(i K) uintptr {
	if m.typ == nil {
		func() {
			m.lock.Lock()
			defer m.lock.Unlock()
			if m.typ == nil {
				// if K is interface type, then the direct reflect.TypeOf(K).Kind return reflect.Ptr
				if typ := reflect.TypeOf(&i); typ.Elem().Kind() == reflect.Interface {
					panic("not support interface type")
				}
				var e any = i
				m.typ = efaceOf(&e).typ
			}
		}()
	}

	var f eface
	f.typ = m.typ
	if f.typ.IsDirectIface() {
		f.data = *(*unsafe.Pointer)(unsafe.Pointer(&i))
	} else {
		f.data = noescape(unsafe.Pointer(&i))
	}

	return nilinterhash(noescape(unsafe.Pointer(&f)), 0xdeadbeef)
}
