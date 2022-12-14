// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmap_test

import (
	"math/rand"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"testing/quick"

	"github.com/lrita/cmap"
)

type mapOp string

const (
	opLoad        = mapOp("Load")
	opStore       = mapOp("Store")
	opLoadOrStore = mapOp("LoadOrStore")
	opDelete      = mapOp("Delete")
)

var mapOps = [...]mapOp{opLoad, opStore, opLoadOrStore, opDelete}

// mapCall is a quick.Generator for calls on mapInterface.
type mapCall struct {
	op   mapOp
	k, v interface{}
}

func (c mapCall) apply(m mapInterface) (interface{}, bool) {
	switch c.op {
	case opLoad:
		return m.Load(c.k)
	case opStore:
		m.Store(c.k, c.v)
		return nil, false
	case opLoadOrStore:
		return m.LoadOrStore(c.k, c.v)
	case opDelete:
		m.Delete(c.k)
		return nil, false
	default:
		panic("invalid mapOp")
	}
}

type mapResult struct {
	value interface{}
	ok    bool
}

func randValue(r *rand.Rand) interface{} {
	b := make([]byte, r.Intn(4))
	for i := range b {
		b[i] = 'a' + byte(rand.Intn(26))
	}
	return string(b)
}

func (mapCall) Generate(r *rand.Rand, size int) reflect.Value {
	c := mapCall{op: mapOps[rand.Intn(len(mapOps))], k: randValue(r)}
	switch c.op {
	case opStore, opLoadOrStore:
		c.v = randValue(r)
	}
	return reflect.ValueOf(c)
}

func applyCalls(m mapInterface, calls []mapCall) (results []mapResult, final map[interface{}]interface{}) {
	for _, c := range calls {
		v, ok := c.apply(m)
		results = append(results, mapResult{v, ok})
	}

	final = make(map[interface{}]interface{})
	m.Range(func(k, v interface{}) bool {
		final[k] = v
		return true
	})

	return results, final
}

func applyMap(calls []mapCall) ([]mapResult, map[interface{}]interface{}) {
	return applyCalls(new(cmap.Cmap), calls)
}

func applyRWMutexMap(calls []mapCall) ([]mapResult, map[interface{}]interface{}) {
	return applyCalls(new(RWMutexMap), calls)
}

func applyDeepCopyMap(calls []mapCall) ([]mapResult, map[interface{}]interface{}) {
	return applyCalls(new(DeepCopyMap), calls)
}

func TestMapMatchesRWMutex(t *testing.T) {
	if err := quick.CheckEqual(applyMap, applyRWMutexMap, nil); err != nil {
		t.Error(err)
	}
}

func TestMapMatchesDeepCopy(t *testing.T) {
	if err := quick.CheckEqual(applyMap, applyDeepCopyMap, nil); err != nil {
		t.Error(err)
	}
}

func TestConcurrentRange(t *testing.T) {
	const mapSize = 1 << 10

	m := new(cmap.Cmap)
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

		m.Range(func(ki, vi interface{}) bool {
			k, v := ki.(int64), vi.(int64)
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

func TestMapCreation(t *testing.T) {
	m := cmap.Cmap{}

	if m.Count() != 0 {
		t.Error("new map should be empty.")
	}
	if !m.IsEmpty() {
		t.Error("new map should be empty.")
	}
}

func TestStoreOperationDuplicatedKey(t *testing.T) {
	m := cmap.Cmap{}
	m.Store(t, "")
	m.Store(t, "")
	if v := m.Count(); v != 1 {
		t.Errorf("map Count() should be %d, got %d", 1, v)
	}
	m.LoadOrStore("m", "")
	if v := m.Count(); v != 2 {
		t.Errorf("map Count() should be %d, got %d", 2, v)
	}
	m.Delete(t)
	if v := m.Count(); v != 1 {
		t.Errorf("map Count() should be %d, got %d", 1, v)
	}
	m.Delete(t)
	if v := m.Count(); v != 1 {
		t.Errorf("map Count() should be %d, got %d", 1, v)
	}
}

func TestMapStoreAndLoad(t *testing.T) {
	const mapSize = 1 << 14

	var (
		m    cmap.Cmap
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

	m.Range(func(ki, vi interface{}) bool {
		k, v := ki.(int64), vi.(int64)
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
