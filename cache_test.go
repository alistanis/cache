package cache

import (
	"context"
	"fmt"
	"testing"
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

type sPair struct {
	s1 string
	s2 string
}

func TestCache_Serve(t *testing.T) {
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

	if !r.Found {
		t.Fatal("24 was not found")
	}

	if r.Val != "42" {
		t.Fatal("r.Val != 42")
	}

	c.Each(func(s1 string, s2 string) {
		fmt.Println(s1 + " " + s2)
	})

	cancel()
	c.Wait()

}
