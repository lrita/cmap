# cmap [![Build Status](https://travis-ci.org/lrita/cmap.svg?branch=master)](https://travis-ci.org/lrita/cmap) [![GoDoc](https://godoc.org/github.com/lrita/cmap?status.png)](https://godoc.org/github.com/lrita/cmap) [![codecov](https://codecov.io/gh/lrita/cmap/branch/master/graph/badge.svg)](https://codecov.io/gh/lrita/cmap) [![Go Report Card](https://goreportcard.com/badge/github.com/lrita/cmap)](https://goreportcard.com/report/github.com/lrita/cmap)

The `map` type in Go doesn't support concurrent reads and writes. `cmap(concurrent-map)` provides a high-performance solution to this by sharding the map with minimal time spent waiting for locks.

The `sync.Map` has a few key differences from this map. The stdlib `sync.Map` is designed for append-only scenarios. So if you want to use the map for something more like in-memory db, you might benefit from using our version. You can read more about it in the golang repo, for example [here](https://github.com/golang/go/issues/21035) and [here](https://stackoverflow.com/questions/11063473/map-with-concurrent-access)

_Here we fork some README document from [concurrent-map](https://github.com/orcaman/concurrent-map)_

## usage

Import the package:

```go
import (
	"github.com/lrita/cmap"
)

```

```bash
go get "github.com/lrita/cmap"
```

The package is now imported under the "cmap" namespace.

## example

```go

	// Create a new map.
	var m cmap.Cmap

	// Stores item within map, sets "bar" under key "foo"
	m.Store("foo", "bar")

	// Retrieve item from map.
	if tmp, ok := m.Load("foo"); ok {
		bar := tmp.(string)
	}

	// Deletes item under key "foo"
	m.Delete("foo")

	// If you are using g1.18+, you can use the generics implementation

	var n cmap.Map[string, string]

	// Stores item within map, sets "bar" under key "foo"
	n.Store("foo", "bar")

	// Retrieve item from map.
	if tmp, ok := n.Load("foo"); ok {
	    bar := tmp
	}

	// Deletes item under key "foo"
	n.Delete("foo")
```

## benchmark

```bash
goos: darwin
goarch: amd64
pkg: github.com/lrita/cmap
BenchmarkLoadMostlyHits/*cmap_test.DeepCopyMap-4         	50000000	        34.5 ns/op
BenchmarkLoadMostlyHits/*cmap_test.RWMutexMap-4          	20000000	        65.2 ns/op
BenchmarkLoadMostlyHits/*sync.Map-4                      	50000000	        34.8 ns/op
BenchmarkLoadMostlyHits/*cmap.Cmap-4                     	30000000	        53.5 ns/op
BenchmarkLoadMostlyMisses/*cmap_test.DeepCopyMap-4       	50000000	        26.7 ns/op
BenchmarkLoadMostlyMisses/*cmap_test.RWMutexMap-4        	20000000	        62.5 ns/op
BenchmarkLoadMostlyMisses/*sync.Map-4                    	50000000	        22.7 ns/op
BenchmarkLoadMostlyMisses/*cmap.Cmap-4                   	30000000	        40.3 ns/op
--- SKIP: BenchmarkLoadOrStoreBalanced/*cmap_test.DeepCopyMap
    cmap_bench_test.go:91: DeepCopyMap has quadratic running time.
BenchmarkLoadOrStoreBalanced/*cmap_test.RWMutexMap-4     	 3000000	       437 ns/op
BenchmarkLoadOrStoreBalanced/*sync.Map-4                 	 3000000	       546 ns/op
BenchmarkLoadOrStoreBalanced/*cmap.Cmap-4                	 3000000	       497 ns/op
--- SKIP: BenchmarkLoadOrStoreUnique/*cmap_test.DeepCopyMap
    cmap_bench_test.go:123: DeepCopyMap has quadratic running time.
BenchmarkLoadOrStoreUnique/*cmap_test.RWMutexMap-4       	 2000000	       990 ns/op
BenchmarkLoadOrStoreUnique/*sync.Map-4                   	 1000000	      1032 ns/op
BenchmarkLoadOrStoreUnique/*cmap.Cmap-4                  	 2000000	       892 ns/op
BenchmarkLoadOrStoreCollision/*cmap_test.DeepCopyMap-4   	100000000	        18.2 ns/op
BenchmarkLoadOrStoreCollision/*cmap_test.RWMutexMap-4    	10000000	       165 ns/op
BenchmarkLoadOrStoreCollision/*sync.Map-4                	100000000	        19.6 ns/op
BenchmarkLoadOrStoreCollision/*cmap.Cmap-4               	20000000	        65.7 ns/op
BenchmarkRange/*cmap_test.DeepCopyMap-4                  	  200000	      8646 ns/op
BenchmarkRange/*cmap_test.RWMutexMap-4                   	   20000	     62046 ns/op
BenchmarkRange/*sync.Map-4                               	  200000	      9317 ns/op
BenchmarkRange/*cmap.Cmap-4                              	   50000	     31107 ns/op
BenchmarkAdversarialAlloc/*cmap_test.DeepCopyMap-4       	 2000000	       531 ns/op
BenchmarkAdversarialAlloc/*cmap_test.RWMutexMap-4        	20000000	        74.3 ns/op
BenchmarkAdversarialAlloc/*sync.Map-4                    	 5000000	       390 ns/op
BenchmarkAdversarialAlloc/*cmap.Cmap-4                   	30000000	        53.6 ns/op
BenchmarkAdversarialDelete/*cmap_test.DeepCopyMap-4      	 5000000	       273 ns/op
BenchmarkAdversarialDelete/*cmap_test.RWMutexMap-4       	20000000	        94.4 ns/op
BenchmarkAdversarialDelete/*sync.Map-4                   	10000000	       137 ns/op
BenchmarkAdversarialDelete/*cmap.Cmap-4                  	30000000	        43.3 ns/op
```
