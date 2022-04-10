package cache

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Example() {
	c := New[int, int](2)

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

func TestCache(t *testing.T) {

	Assert := assert.New(t)

	cache := New[int, string](4)
	cache.Put(42, "42")

	Assert.EqualValues(cache.Size(), 1)

	s, ok := cache.Get(42)
	Assert.True(ok)

	Assert.EqualValues("42", s)

	s, ok = cache.Get(0)
	Assert.False(ok)

	// replace existing value
	cache.Put(42, "24")
	s, ok = cache.Get(42)
	Assert.True(ok)

	Assert.EqualValues("24", s)

	cache.Resize(1)

	cache.Put(24, "42")

	Assert.EqualValues(1, cache.Size())
	s, ok = cache.Get(24)

	Assert.True(ok)
	Assert.EqualValues("42", s)

	cache.Resize(2)

	// resizing does not crash
	Assert.NotPanics(func() { cache.Resize(2) })

	cache.Put(42, "24")

	Assert.EqualValues(2, cache.Size())

	s, ok = cache.Get(42)
	Assert.True(ok)
	Assert.EqualValues("24", s)

	cache.Put(1, "2")

	Assert.EqualValues(2, cache.Size())
	s, ok = cache.Get(42)
	Assert.True(ok)
	Assert.EqualValues("24", s)

	_, ok = cache.Get(24)
	Assert.False(ok)

	n := cache.list.Front()

	Assert.EqualValues("24", n.Value.Val)

	cache.Resize(4)

	Assert.EqualValues(4, cache.Capacity())
	Assert.EqualValues(2, cache.Size())

	cache.Put(2, "3")
	cache.Put(3, "4")
	Assert.EqualValues(4, cache.Size())

	cache.Put(4, "5")

	Assert.EqualValues(4, cache.Size())

	cache.Resize(1)

	Assert.EqualValues(1, cache.Size())
	s, ok = cache.Get(4)

	Assert.True(ok)
	Assert.EqualValues("5", s)

	cache.evict()
	cache.evict()
	cache.evict()
	cache.evict()

	Assert.NotPanics(func() { cache.evict() })
}

type sPair struct {
	s1 string
	s2 string
}

func TestCache_Serve(t *testing.T) {
	Assert := assert.New(t)

	cache := New[string, string](5)
	ctx, cancel := context.WithCancel(context.Background())
	c := cache.Serve(ctx)

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
	c.Wait()

}
