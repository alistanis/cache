# cache
![Coverage](https://img.shields.io/badge/Coverage-100.0%25-brightgreen)

Cache is a thread safe, generic, and lockless in memory LRU cache object. This is achieved by partitioning values across
many
smaller LRU (least recently used) caches and interacting with those caches over channels.
Each smaller cache maintains access to its own elements and communicates information back to the Cache object,
which then responds back to the original caller.

# behavior

### LRU backing caches

```go
type lruCache[K comparable, V any] struct {
table map[K]*list.Node[KVPair[K, V]]
list  *list.List[KVPair[K, V]]
...
client  *client[K, V]
evictFn func (k K, v V)
}
```

The LRU backing caches behaves exactly like a normal LRU and are composed of a doubly linked list and a map to allow key
lookups.
When an entry is added or accessed it is pushed to the front of the list, and when enough items are added to the cache
the oldest
items(the back of the list) are evicted to make room for more recently used entries. The list implementation itself is a
ported
version from the go stdlib which uses type parameters instead of runtime interfaces and can be found in the `/list`
directory.

Each `*lruCache` spawns a single goroutine when `*lruCache.serve(ctx)` is called.

### client

```go
type client[K comparable, V any] struct {
// GetChannel is a channel for retrieving values from the cache for which this client is associated
GetChannel *RequestChannel[Request[K], GetResponse[K, V]]
// PutChannel is a channel for placing values into the cache for which this client is associated
PutChannel *RequestChannel[Request[KVPair[K, V]], struct{}]
...
}
```

The client abstraction contains a collection of channels by which it communicates with `*lruCache` objects. This allows
each
`*lruCache` to run a single goroutine on which it listens for requests over these channels, processes them, and sends
responses
without the need for locking anything.

### Cache

```go
type Cache[K comparable, V any] struct {
caches []*cache[K, V]
}
```

# examples

### Single Cache Partition

```go

package main

import (
	"context"
	"fmt"

	"github.com/alistanis/cache"
)

// Simple example that is shown with a concurrency of 1 in order to 
// illustrate how the smaller LRU caches work.
func main() {
	ctx, cancel := context.WithCancel(context.Background())
	concurrency := 1
	lruCacheLimit := 5
	c := cache.New[int, int](ctx, lruCacheLimit, concurrency)
	defer c.Wait()

	c.Put(42, 42)
	c.Put(1, 1)
	c.Get(42)
	c.Put(0, 0)
	c.Put(2, 2)
	c.Put(3, 3)
	c.Get(42)
	// evict 1
	c.Put(4, 4)

	c.Each(func(key int, val int) {
		fmt.Println(key)
	})

	cancel()
	// Output:
	// 4
	// 42
	// 3
	// 2
	// 0
}
```

### Many Cache Partitions

```go

package main

import (
	"context"
	"fmt"

	"github.com/alistanis/cache"
)

// General example for using the Cache object. Since elements are spread across many partitions,
// order can not be guaranteed, and items will not be evicted in pure LRU terms; it is possible that some partitions
// may see more traffic than others and may be more eviction heavy, but generally, access patterns amortize evenly.
func main() {

	ctx, cancel := context.WithCancel(context.Background())
	concurrency := 10 // runtime.NumCPU() instead of 10 for actual use
	c := cache.New[int, int](ctx, 6, concurrency)
	defer c.Wait()

	fmt.Println(c.Meta().Len())
	fmt.Println(c.Meta().Cap())
	finished := make(chan struct{})
	go func() {
		for i := 0; i < 4*concurrency; i++ {
			c.Put(i, i)
		}
		finished <- struct{}{}
	}()

	go func() {
		for i := 8 * concurrency; i > 3*concurrency; i-- {
			c.Put(i, i)
		}
		finished <- struct{}{}
	}()

	<-finished
	<-finished

	for i := 0; i < 8*concurrency; i++ {
		v, found := c.Get(i)
		if !found {
			// get value from backing store
			// res := db.Query(...)
			// v = getValFromRes(res)
			// put value back into cache
			// c.Put(i, v)
			v = 0
		} else {
			if i != v {
				panic("uh oh")
			}
		}

	}

	// we've put enough values into the cache that 10 partitions are filled with 6 elements each
	fmt.Println(c.Meta().Len())
	fmt.Println(c.Meta().Cap())
	// Output:
	// 0
	// 60
	// 60
	// 60
	cancel()
}
```

### Cache With Eviction Function/Cleanup

```go
package main

import (
	"context"
	"log"
	"os"
	"runtime"
	"syscall"

	"github.com/alistanis/cache"
)

func main() {

	ctx, cancel := context.WithCancel(context.Background())
	errC := make(chan error)

	// concurrency/partition of 1 to guarantee LRU order
	// size of 1 in order to demonstrate eviction
	// Type of cache elements can be inferred by the arguments to the eviction function
	c := cache.NewWithEvictionFunction(ctx, 1, 1, func(s string, f *os.File) {
		st, err := f.Stat()
		if err != nil {
			errC <- err
			return
		}
		// type assertion will fail on other platforms
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
			log.Printf("Closing file at path %s, fd: %d, inode: %d", s, f.Fd(), st.Sys().(*syscall.Stat_t).Ino)
		} else {
			log.Printf("Closing file at path %s, fd: %d", s, f.Fd())
		}
		errC <- f.Close()
	})

	defer c.Wait()
	defer cancel()

	d, err := os.MkdirTemp("", "")
	if err != nil {
		log.Fatal(err)
	}

	// cleanup temp resources after main exits
	defer func(path string) {
		err := os.RemoveAll(path)
		if err != nil {
			log.Fatal(err)
		}

	}(d)

	// exit channel that will block until we're 
	// finished collecting any/all errors
	exit := make(chan struct{})
	go func() {
		for e := range errC {
			if e != nil {
				log.Println(e)
			}
		}
		// signal that we're finished and can exit safely
		exit <- struct{}{}
	}()

	f, err := os.CreateTemp(d, "")
	if err != nil {
		log.Println(err)
		return
	}

	// first entry on the LRU
	c.Put(f.Name(), f)

	f2, err := os.CreateTemp(d, "")
	if err != nil {
		log.Println(err)
		return
	}

	// place f2 in the cache and evict f causing the eviction 
	// function to fire, closing the file and logging
	// 2022/04/13 07:31:47 Closing file at path /var/folders/q3/dt78p91s1b562lmq7qstllv00000gn/T/1705161844/1443821512, fd: 6, inode: 49662131

	c.Put(f2.Name(), f2)

	// now forcibly evict f2
	evicted := c.Evict()

	// 2022/04/13 07:31:47 Closing file at path /var/folders/q3/dt78p91s1b562lmq7qstllv00000gn/T/1705161844/767977656, fd: 7, inode: 49662130
	log.Println(evicted) // 1

	f, err = os.CreateTemp(d, "")
	if err != nil {
		log.Println(err)
		return
	}

	c.Put(f.Name(), f)
	// Evict f again by resizing
	log.Println(c.Resize(0)) // 1

	// We're finished so we can close the error channel
	close(errC)
	// Wait until errors are processed and exit
	<-exit
}
```

# Benchmarks

### MacBook Air (M1, 2020), 16GB Ram

```
go test -v -benchmem ./... -bench . -run=bench

goos: darwin
goarch: arm64
pkg: github.com/alistanis/cache
BenchmarkCache_IntInt_SingleThread
BenchmarkCache_IntInt_SingleThread/Put
BenchmarkCache_IntInt_SingleThread/Put-8         1442264               824.1 ns/op           110 B/op          6 allocs/op
BenchmarkCache_IntInt_SingleThread/Get
BenchmarkCache_IntInt_SingleThread/Get-8         1814316               662.7 ns/op            47 B/op          4 allocs/op
BenchmarkCache_IntInt_ParallelPut
BenchmarkCache_IntInt_ParallelPut-8              6645994               183.5 ns/op           110 B/op          6 allocs/op
BenchmarkCache_IntInt_ParallelGet
BenchmarkCache_IntInt_ParallelGet-8              8311953               138.8 ns/op            48 B/op          5 allocs/op
BenchmarkCache_StringString_SingleThread
BenchmarkCache_StringString_SingleThread/Put
BenchmarkCache_StringString_SingleThread/Put-8           1000000              1073 ns/op             209 B/op          9 allocs/op
BenchmarkCache_StringString_SingleThread/Get
BenchmarkCache_StringString_SingleThread/Get-8           1444695               828.3 ns/op            87 B/op          5 allocs/op
BenchmarkCache_StringString_ParallelPut
BenchmarkCache_StringString_ParallelPut-8                4905408               238.7 ns/op           209 B/op          9 allocs/op
BenchmarkCache_StringString_ParallelGet
BenchmarkCache_StringString_ParallelGet-8                6977521               170.2 ns/op            88 B/op          6 allocs/op
PASS
ok      github.com/alistanis/cache      12.475s
PASS
ok      github.com/alistanis/cache/list 0.093s

```

### MacBook Pro (M1 Pro, 16-inch, 2021), 16GB Ram
```
go test -v -benchmem ./... -bench . -run=bench
goos: darwin
goarch: arm64
pkg: github.com/alistanis/cache
BenchmarkCache_IntInt_SingleThread
BenchmarkCache_IntInt_SingleThread/Put
BenchmarkCache_IntInt_SingleThread/Put-10        1431043               825.7 ns/op           110 B/op          6 allocs/op
BenchmarkCache_IntInt_SingleThread/Get
BenchmarkCache_IntInt_SingleThread/Get-10        1772635               673.3 ns/op            47 B/op          4 allocs/op
BenchmarkCache_IntInt_ParallelPut
BenchmarkCache_IntInt_ParallelPut-10             6866359               179.5 ns/op           110 B/op          6 allocs/op
BenchmarkCache_IntInt_ParallelGet
BenchmarkCache_IntInt_ParallelGet-10             8667046               138.0 ns/op            48 B/op          5 allocs/op
BenchmarkCache_StringString_SingleThread
BenchmarkCache_StringString_SingleThread/Put
BenchmarkCache_StringString_SingleThread/Put-10                  1000000              1094 ns/op             209 B/op          9 allocs/op
BenchmarkCache_StringString_SingleThread/Get
BenchmarkCache_StringString_SingleThread/Get-10                  1455924               822.9 ns/op            87 B/op          5 allocs/op
BenchmarkCache_StringString_ParallelPut
BenchmarkCache_StringString_ParallelPut-10                       4883151               280.0 ns/op           209 B/op          9 allocs/op
BenchmarkCache_StringString_ParallelGet
BenchmarkCache_StringString_ParallelGet-10                       6611814               190.0 ns/op            88 B/op          6 allocs/op
PASS
ok      github.com/alistanis/cache      13.023s
PASS
ok      github.com/alistanis/cache/list 0.156s

```

### MacBook Pro (Intel(R) Core(TM) i9-9980HK CPU @ 2.40GHz, 16-inch, 2019), 64 GB Ram
```
go test -v -benchmem ./... -bench . -run=bench
goos: darwin
goarch: amd64
pkg: github.com/alistanis/cache
cpu: Intel(R) Core(TM) i9-9980HK CPU @ 2.40GHz
BenchmarkCache_IntInt_SingleThread
BenchmarkCache_IntInt_SingleThread/Put
BenchmarkCache_IntInt_SingleThread/Put-16         794200              1452 ns/op             110 B/op          6 allocs/op
BenchmarkCache_IntInt_SingleThread/Get
BenchmarkCache_IntInt_SingleThread/Get-16         976036              1184 ns/op              47 B/op          4 allocs/op
BenchmarkCache_IntInt_ParallelPut
BenchmarkCache_IntInt_ParallelPut-16             6931814               177.8 ns/op           111 B/op          6 allocs/op
BenchmarkCache_IntInt_ParallelGet
BenchmarkCache_IntInt_ParallelGet-16             8706753               138.9 ns/op            48 B/op          5 allocs/op
BenchmarkCache_StringString_SingleThread
BenchmarkCache_StringString_SingleThread/Put
BenchmarkCache_StringString_SingleThread/Put-16                   586318              2015 ns/op             209 B/op          9 allocs/op
BenchmarkCache_StringString_SingleThread/Get
BenchmarkCache_StringString_SingleThread/Get-16                   860658              1434 ns/op              87 B/op          6 allocs/op
BenchmarkCache_StringString_ParallelPut
BenchmarkCache_StringString_ParallelPut-16                       5286390               227.2 ns/op           209 B/op          9 allocs/op
BenchmarkCache_StringString_ParallelGet
BenchmarkCache_StringString_ParallelGet-16                       6639519               162.4 ns/op            88 B/op          6 allocs/op
PASS
ok      github.com/alistanis/cache      10.423s
PASS
ok      github.com/alistanis/cache/list 0.111s

```

### Linux, Intel(R) Core(TM) i7-10750H CPU @ 2.60GHz
```
go test -v -benchmem ./... -bench . -run=bench
goos: linux
goarch: amd64
pkg: github.com/alistanis/cache
cpu: Intel(R) Core(TM) i7-10750H CPU @ 2.60GHz
BenchmarkCache_IntInt_SingleThread
BenchmarkCache_IntInt_SingleThread/Put
BenchmarkCache_IntInt_SingleThread/Put-12        1000000              1062 ns/op             110 B/op          6 allocs/op
BenchmarkCache_IntInt_SingleThread/Get
BenchmarkCache_IntInt_SingleThread/Get-12        1349502               886.8 ns/op            47 B/op          4 allocs/op
BenchmarkCache_IntInt_ParallelPut
BenchmarkCache_IntInt_ParallelPut-12             6455076               197.1 ns/op           110 B/op          6 allocs/op
BenchmarkCache_IntInt_ParallelGet
BenchmarkCache_IntInt_ParallelGet-12             6827888               168.3 ns/op            48 B/op          5 allocs/op
BenchmarkCache_StringString_SingleThread
BenchmarkCache_StringString_SingleThread/Put
BenchmarkCache_StringString_SingleThread/Put-12                   842470              1446 ns/op             209 B/op          9 allocs/op
BenchmarkCache_StringString_SingleThread/Get
BenchmarkCache_StringString_SingleThread/Get-12                  1000000              1075 ns/op              87 B/op          6 allocs/op
BenchmarkCache_StringString_ParallelPut
BenchmarkCache_StringString_ParallelPut-12                       4743643               269.0 ns/op           209 B/op          9 allocs/op
BenchmarkCache_StringString_ParallelGet
BenchmarkCache_StringString_ParallelGet-12                       5551136               206.4 ns/op            88 B/op          6 allocs/op
PASS
ok      github.com/alistanis/cache      11.210s
PASS
ok      github.com/alistanis/cache/list 0.002s
```

