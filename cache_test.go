package cache

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Example() {
	c := newCache[int, int](2)

	c.Put(42, 42)
	c.Put(10, 10)
	c.Get(42)
	c.Put(0, 0) // evicts 10

	c.Each(func(key int, val int) {
		fmt.Println(key)
	})
	// Output:
	// 0
	// 42
}

func Test_cache(t *testing.T) {

	Assert := assert.New(t)

	c := newCache[int, string](4)
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

	ca := newCache[string, string](5)
	ctx, cancel := context.WithCancel(context.Background())
	c := ca.Serve(ctx)
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
	Assert.EqualValues(capacity*runtime.NumCPU(), met.Cap)
	Assert.EqualValues(capacity*runtime.NumCPU(), met.Len)

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
	Assert.EqualValues(capacity*runtime.NumCPU(), met.Cap)
	Assert.EqualValues(capacity*runtime.NumCPU(), met.Len)

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

	for i := 0; c.Meta().Len != capacity*runtime.NumCPU(); i++ {
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

	Assert.EqualValues(capacity*runtime.NumCPU(), c.Meta().Len)

	cancel1()
}
