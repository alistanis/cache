package cache

import (
	"context"
	"fmt"
	"github.com/alistanis/cache/list"
	"github.com/stretchr/testify/assert"
	"log"
	"math/rand"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"
)

// Example_singleCache is a simple example that is shown with a concurrency of 1 in order to illustrate how the smaller LRU
// caches work.
func Example_singleCache() {
	ctx, cancel := context.WithCancel(context.Background())
	capacityPerPartition := 5
	// this determines how many LRU backing
	// caches are used and how many caches can be accessed concurrently
	numberOfCaches := 1

	c := New[int, int](ctx, capacityPerPartition, numberOfCaches)
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

// Example serves as a general example for using the Cache object. Since elements are spread across many partitions,
// order can not be guaranteed, and items will not be evicted in pure LRU terms; it is possible that some partitions
// may see more traffic than others and may be more eviction heavy, but generally, access patterns amortize evenly.
func Example() {

	ctx, cancel := context.WithCancel(context.Background())
	concurrency := 10 // runtime.NumCPU() instead of 10 for actual use
	c := New[int, int](ctx, 6, concurrency)
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
			v = 0
		} else {
			if i != v {
				panic("uh oh")
			}
		}

	}

	// we've put enough values into the lruCache that 10 partitions are filled with 6 elements each
	fmt.Println(c.Meta().Len())
	fmt.Println(c.Meta().Cap())
	// Output:
	// 0
	// 60
	// 60
	// 60
	cancel()
}

func Test_lruCache(t *testing.T) {

	Assert := assert.New(t)

	c := newLruCache[int, string](4)
	c.Put(42, "42")

	Assert.EqualValues(c.Size(), 1)

	s, ok := c.Get(42)
	Assert.True(ok)

	Assert.EqualValues("42", s)

	s, ok = c.Get(0)
	Assert.False(ok)

	// replace existing value
	c.Put(42, "24")
	s, ok = c.Get(42)
	Assert.True(ok)

	Assert.EqualValues("24", s)

	c.Resize(1)

	c.Put(24, "42")

	Assert.EqualValues(1, c.Size())
	s, ok = c.Get(24)

	Assert.True(ok)
	Assert.EqualValues("42", s)

	c.Resize(2)

	// resizing does not crash
	Assert.NotPanics(func() { c.Resize(2) })

	c.Put(42, "24")

	Assert.EqualValues(2, c.Size())

	s, ok = c.Get(42)
	Assert.True(ok)
	Assert.EqualValues("24", s)

	c.Put(1, "2")

	Assert.EqualValues(2, c.Size())
	s, ok = c.Get(42)
	Assert.True(ok)
	Assert.EqualValues("24", s)

	_, ok = c.Get(24)
	Assert.False(ok)

	n := c.list.Front()

	Assert.EqualValues("24", n.Value.Val)

	c.Resize(4)

	Assert.EqualValues(4, c.Capacity())
	Assert.EqualValues(2, c.Size())

	c.Put(2, "3")
	c.Put(3, "4")
	Assert.EqualValues(4, c.Size())

	c.Put(4, "5")

	Assert.EqualValues(4, c.Size())

	c.Resize(1)

	Assert.EqualValues(1, c.Size())
	s, ok = c.Get(4)

	Assert.True(ok)
	Assert.EqualValues("5", s)

	c.evict()
	c.evict()
	c.evict()
	c.evict()

	Assert.NotPanics(func() { c.evict() })
}

type sPair struct {
	s1 string
	s2 string
}

func Test_cache_Serve(t *testing.T) {
	Assert := assert.New(t)

	ca := newLruCache[string, string](5)
	ctx, cancel := context.WithCancel(context.Background())
	c := ca.serve(ctx)
	defer c.Wait()
	pairs := []sPair{
		{"Hello", "Goodbye"},
		{"1", "2"},
		{"4", "5"},
		{"24", "42"},
		{"3.14", "6.28"},
	}

	for _, p := range pairs {
		c.Put(p.s1, p.s2)
	}

	r := c.Get("24")

	Assert.True(r.Found)
	Assert.EqualValues("42", r.Val)

	r = c.Get("nil")

	Assert.False(r.Found)

	c.Each(func(s1 string, s2 string) {
		fmt.Println(s1 + " " + s2)
	})

	cancel()

}

func TestCache(t *testing.T) {
	Assert := assert.New(t)
	ctx, cancel := context.WithCancel(context.Background())

	capacity := 1000
	c := New[int, int](ctx, capacity, runtime.NumCPU())
	defer c.Wait()

	for i := 0; i < capacity*10*runtime.NumCPU(); i++ {
		c.Put(i, i)
	}

	met := c.Meta()
	Assert.EqualValues(capacity*runtime.NumCPU(), met.Cap())
	Assert.EqualValues(capacity*runtime.NumCPU(), met.Len())

	c.Put(1, 1)

	found := c.Remove(1)
	Assert.True(found)

	found = c.Remove(1)
	Assert.False(found)

	c.Put(1, 1)

	v, found := c.Get(1)
	Assert.EqualValues(1, v)
	Assert.True(found)

	i := make([]int, 0, capacity*runtime.NumCPU())

	c.Each(func(k, v int) {
		i = append(i, v)
	})

	Assert.EqualValues(capacity*runtime.NumCPU(), len(i))

	cancel()
}

func TestCache_String(t *testing.T) {
	Assert := assert.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	capacity := 1000

	c := New[string, string](ctx, capacity, runtime.NumCPU())
	defer c.Wait()
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, 8)
	for i := 0; i < capacity*10*runtime.NumCPU(); i++ {
		read, err := r.Read(b)
		Assert.EqualValues(8, read)
		Assert.Nil(err)

		c.Put(string(b), string(b))
	}

	met := c.Meta()
	Assert.EqualValues(capacity*runtime.NumCPU(), met.Cap())
	Assert.EqualValues(capacity*runtime.NumCPU(), met.Len())

	v, found := c.Get(string(b))
	Assert.True(found)
	Assert.EqualValues(string(b), v)

	cancel()
}

func TestCache_Concurrently(t *testing.T) {
	Assert := assert.New(t)
	ctx, cancel1 := context.WithCancel(context.Background())

	capacity := 100
	c := New[int, int](ctx, capacity, runtime.NumCPU())
	defer c.Wait()

	for i := 0; c.Meta().Len() != capacity*runtime.NumCPU(); i++ {
		c.Put(i, i)
	}

	is := make([]int, 0, runtime.NumCPU()*capacity)
	c.Each(func(k, v int) {
		is = append(is, v)
	})

	firstPartitionValues := make([]int, 0, capacity)

	for i := 0; i < capacity; i++ {
		index := i % len(is)
		if index != 0 {
			continue
		}
		firstPartitionValues = append(firstPartitionValues, is[index])
	}

	getWait := make(chan struct{})
	putWait := make(chan struct{})
	c2, cancel2 := context.WithCancel(context.Background())
	go func(ct context.Context) {
		for {
			select {
			case <-ct.Done():
				getWait <- struct{}{}
				return
			default:
				for _, i := range firstPartitionValues {
					v, found := c.Get(i)
					Assert.True(found)
					Assert.EqualValues(i, v)
				}
			}
		}
	}(c2)

	go func() {

		for i := 0; i < capacity; i++ {
			index := i % len(is)
			if index != 0 {
				continue
			}
			c.Put(i, i)
		}
		putWait <- struct{}{}
	}()

	<-putWait
	cancel2()
	<-getWait

	Assert.EqualValues(capacity*runtime.NumCPU(), c.Meta().Len())

	cancel1()
}

func TestCache_EvictionFunction(t *testing.T) {
	Assert := assert.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	errC := make(chan error)

	c := WithEvictionFunction(ctx, 1, 1, func(s string, f *os.File) {
		_, err := f.Stat()
		if err != nil {
			errC <- err
			return
		}

		log.Printf("Closing file at path %s, fd: %d", s, f.Fd())

		errC <- f.Close()
	})

	defer c.Wait()
	defer cancel()

	d, err := os.MkdirTemp("", "")
	Assert.Nil(err)
	defer func(path string) {
		err := os.RemoveAll(path)
		Assert.Nil(err)

	}(d)

	exit := make(chan struct{})
	go func() {
		for e := range errC {
			if e != nil {
				t.Error(e)
			}
		}
		exit <- struct{}{}
	}()

	f, err := os.CreateTemp(d, "")
	if err != nil {
		t.Error(err)
	}

	c.Put(f.Name(), f)

	f2, err := os.CreateTemp(d, "")
	if err != nil {
		t.Error(err)
	}

	// evict f and cause the eviction function to fire, closing the file
	c.Put(f2.Name(), f2)

	// now evict f2
	evicted := c.Evict()
	Assert.EqualValues(1, evicted)
	Assert.Zero(c.Evict())

	f, err = os.CreateTemp(d, "")
	if err != nil {
		t.Error(err)
	}

	c.Put(f.Name(), f)
	Assert.EqualValues(1, c.Resize(0))

	close(errC)
	<-exit
}

func TestCache_Client(t *testing.T) {
	Assert := assert.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	capacity := 5
	concurrency := 1
	ca := New[int, int](ctx, capacity, concurrency)

	defer ca.Wait()
	defer cancel()

	c := ca.Client(0)

	Assert.EqualValues(5, c.Meta().Cap)

	c.Put(1, 1)
	c.Put(2, 2)
	c.Put(42, 42)
	c.Put(3, 3)
	c.Get(42)
	c.Put(314, 314)

	vals := []int{314, 42, 3, 2, 1}

	Assert.EqualValues(5, c.Meta().Len)
	i := 0
	c.Each(func(k, v int) {
		Assert.EqualValues(vals[i], v)
		i++
	})

	evicted := c.EvictTo(0)
	Assert.EqualValues(5, evicted)
	Assert.EqualValues(0, c.Meta().Len)
	Assert.EqualValues(5, c.Meta().Cap)

	c.Put(1, 1)
	c.Put(2, 2)
	c.Put(42, 42)
	c.Put(3, 3)
	c.Get(42)
	c.Put(314, 314)

	evicted = c.Resize(2)
	Assert.EqualValues(3, evicted)
	Assert.EqualValues(2, c.Meta().Len)
	Assert.EqualValues(2, c.Meta().Cap)

	vals = []int{314, 42}
	i = 0
	c.Each(func(k, v int) {
		Assert.EqualValues(vals[i], v)
		i++
	})
}

func TestCache_Memory(t *testing.T) {
	Assert := assert.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	capacity := 5
	concurrency := 1
	c := New[int, int](ctx, capacity, concurrency)

	defer c.Wait()
	defer cancel()

	c.Put(1, 1)
	c.Put(42, 42)
	c.Put(314159, 314159)

	iSize := int(unsafe.Sizeof(0))
	s := iSize * 3 * 2
	s += iSize * 3
	s += int(unsafe.Sizeof(&list.Node[int]{})) * 3
	s += int(unsafe.Sizeof(KVPair[int, int]{0, 0})) * 3
	Assert.EqualValues(s, c.Memory())
}

type TestStruct struct {
	I int
	S string
	T *TestStruct
}

func TestCache_Memory_Struct(t *testing.T) {
	Assert := assert.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	capacity := 5
	concurrency := 1
	c := New[int, TestStruct](ctx, capacity, concurrency)

	defer c.Wait()
	defer cancel()

	c.Put(1, TestStruct{1, "", &TestStruct{2, "", nil}})
	c.Put(42, TestStruct{1, "", nil})
	c.Put(314159, TestStruct{1, "hello", nil})
	iSize := int(unsafe.Sizeof(0))
	s := iSize * 3 * 2
	s += int(unsafe.Sizeof(TestStruct{})) * 3
	s += int(unsafe.Sizeof(&list.Node[TestStruct]{})) * 3
	s += int(unsafe.Sizeof(KVPair[int, TestStruct]{0, TestStruct{}})) * 3
	Assert.EqualValues(s, c.Memory())
}
