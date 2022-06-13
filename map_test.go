//go:build go1.18
// +build go1.18

package cmap_test

import (
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"testing/quick"

	"github.com/lrita/cmap"
)

type StringMap struct {
	m cmap.Map[string, interface{}]
}

func (m *StringMap) Load(k interface{}) (interface{}, bool) {
	return m.m.Load(k.(string))
}

func (m *StringMap) Store(key, value interface{}) {
	m.m.Store(key.(string), value)
}

func (m *StringMap) LoadOrStore(key, value interface{}) (actual interface{}, loaded bool) {
	return m.m.LoadOrStore(key.(string), value)
}

func (m *StringMap) Delete(key interface{}) {
	m.m.Delete(key.(string))
}

func (m *StringMap) Range(fn func(key, value interface{}) bool) {
	m.m.Range(func(k string, v interface{}) bool {
		return fn(k, v)
	})
}

func applyGMap(calls []mapCall) ([]mapResult, map[interface{}]interface{}) {
	return applyCalls(new(StringMap), calls)
}

func TestGMapMatchesRWMutex(t *testing.T) {
	if err := quick.CheckEqual(applyGMap, applyRWMutexMap, nil); err != nil {
		t.Error(err)
	}
}

func TestGMapMatchesDeepCopy(t *testing.T) {
	if err := quick.CheckEqual(applyGMap, applyDeepCopyMap, nil); err != nil {
		t.Error(err)
	}
}

func TestGMapConcurrentRange(t *testing.T) {
	const mapSize = 1 << 10

	m := new(cmap.Map[int64, any])
	for n := int64(1); n <= mapSize; n++ {
		m.Store(n, int64(n))
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	defer func() {
		close(done)
		wg.Wait()
	}()
	for g := int64(runtime.GOMAXPROCS(0)); g > 0; g-- {
		r := rand.New(rand.NewSource(g))
		wg.Add(1)
		go func(g int64) {
			defer wg.Done()
			for i := int64(0); ; i++ {
				select {
				case <-done:
					return
				default:
				}
				for n := int64(1); n < mapSize; n++ {
					if r.Int63n(mapSize) == 0 {
						m.Store(n, n*i*g)
					} else {
						m.Load(n)
					}
				}
			}
		}(g)
	}

	iters := 1 << 10
	if testing.Short() {
		iters = 16
	}
	for n := iters; n > 0; n-- {
		seen := make(map[int64]bool, mapSize)

		m.Range(func(ki int64, vi interface{}) bool {
			k, v := ki, vi.(int64)
			if v%k != 0 {
				t.Fatalf("while Storing multiples of %v, Range saw value %v", k, v)
			}
			if seen[k] {
				t.Fatalf("Range visited key %v twice", k)
			}
			seen[k] = true
			return true
		})

		if len(seen) != mapSize {
			t.Fatalf("Range visited %v elements of %v-element Map", len(seen), mapSize)
		}
	}
}

func TestGMapCreation(t *testing.T) {
	m := cmap.Map[int, int]{}

	if m.Count() != 0 {
		t.Error("new map should be empty.")
	}
	if !m.IsEmpty() {
		t.Error("new map should be empty.")
	}
}

func TestGMapStoreOperationDuplicatedKey(t *testing.T) {
	m := cmap.Map[string, interface{}]{}
	m.Store("t", "")
	m.Store("t", "")
	if v := m.Count(); v != 1 {
		t.Errorf("map Count() should be %d, got %d", 1, v)
	}
	m.LoadOrStore("m", "")
	if v := m.Count(); v != 2 {
		t.Errorf("map Count() should be %d, got %d", 2, v)
	}
	m.Delete("t")
	if v := m.Count(); v != 1 {
		t.Errorf("map Count() should be %d, got %d", 1, v)
	}
	m.Delete("t")
	if v := m.Count(); v != 1 {
		t.Errorf("map Count() should be %d, got %d", 1, v)
	}
}

func TestGMapStoreAndLoad(t *testing.T) {
	const mapSize = 1 << 14

	var (
		m    cmap.Map[int64, interface{}]
		wg   sync.WaitGroup
		seen = make(map[int64]bool, mapSize)
	)

	for n := int64(1); n <= mapSize; n++ {
		nn := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Store(nn, nn)
		}()
	}

	wg.Wait()

	m.Range(func(ki int64, vi interface{}) bool {
		k, v := ki, vi.(int64)
		if v%k != 0 {
			t.Fatalf("while Storing multiples of %v, Range saw value %v", k, v)
		}
		if seen[k] {
			t.Fatalf("Range visited key %v twice", k)
		}
		seen[k] = true
		return true
	})

	if len(seen) != mapSize {
		t.Fatalf("Range visited %v elements of %v-element Map", len(seen), mapSize)
	}

	for n := int64(1); n <= mapSize; n++ {
		nn := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Delete(nn)
		}()
	}

	wg.Wait()

	if !m.IsEmpty() {
		t.Fatalf("Map should be empty, remained %v", m.Count())
	}
}
