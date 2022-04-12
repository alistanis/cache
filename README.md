# cache

Cache is a thread safe and lockless in memory cache object. This is achieved by partitioning values across many
smaller LRU (least recently used) caches and interacting with those caches over channels. 
Each smaller cache maintains access to its own elements and communicates information back to the Cache object, 
which then responds back to the original caller.

# examples

### Single Cache Partition

```go

package main

import (
	"context"
	"fmt"
	
	"github.com/alistanis/cache"
)

// Example_singleCache is a simple example that is shown with a concurrency of 1 in order to 
// illustrate how the smaller LRU caches work.
func main() {
    ctx, cancel := context.WithCancel(context.Background())

    c := cache.New[int, int](ctx, 5, 1)
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

// Example serves as a general example for using the Cache object. Since elements are spread across many partitions,
// order can not be guaranteed, and items will not be evicted in pure LRU terms; it is possible that some partitions
// may see more traffic than others and may be more eviction heavy, but generally, access patterns amortize evenly.
func main() {

	ctx, cancel := context.WithCancel(context.Background())
	concurrency := 10 // runtime.NumCPU() instead of 10 for actual use
	c := cache.New[int, int](ctx, 6, concurrency)
	defer c.Wait()

	fmt.Println(c.Meta().Len)
	fmt.Println(c.Meta().Cap)
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
	fmt.Println(c.Meta().Len)
	fmt.Println(c.Meta().Cap)
	// Output:
	// 0
	// 60
	// 60
	// 60
	cancel()
}
```

# Benchmarks

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