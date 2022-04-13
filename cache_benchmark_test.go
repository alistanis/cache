package cache

import (
	"context"
	"fmt"
	"runtime"
	"testing"
)

func BenchmarkCache_IntInt_SingleThread(b *testing.B) {

	ctx, cancel := context.WithCancel(context.Background())
	c := New[int, int](ctx, 100, runtime.NumCPU())
	defer c.Wait()
	defer cancel()

	b.Run("Put", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			c.Put(i, i)
		}
	})

	b.Run("Get", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			v, found := c.Get(i)
			if found {
				if v != i {
					b.Fatal("uh oh")
				}
			}
		}
	})

}

func BenchmarkCache_IntInt_ParallelPut(b *testing.B) {

	b.RunParallel(func(pb *testing.PB) {
		ctx, cancel := context.WithCancel(context.Background())
		c := New[int, int](ctx, 100, runtime.NumCPU())
		defer c.Wait()
		defer cancel()
		i := 0
		for pb.Next() {
			c.Put(i, i)
			i++
		}
	})
}

func BenchmarkCache_IntInt_ParallelGet(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		ctx, cancel := context.WithCancel(context.Background())
		c := New[int, int](ctx, 100, runtime.NumCPU())
		defer c.Wait()
		defer cancel()

		for i := 0; i < 100*runtime.NumCPU(); i++ {
			c.Put(i, i)
		}

		i := 0
		for pb.Next() {
			v, found := c.Get(i)
			if found {
				if v != i {
					b.Fatal("uh oh")
				}
			}
			i++
		}
	})
}

func indexToString(i int) string {
	return fmt.Sprintf("key:%d", i)
}

func BenchmarkCache_StringString_SingleThread(b *testing.B) {

	ctx, cancel := context.WithCancel(context.Background())
	c := New[string, string](ctx, 100, runtime.NumCPU())
	defer c.Wait()
	defer cancel()

	b.Run("Put", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			c.Put(indexToString(i), indexToString(i))
		}
	})

	b.Run("Get", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			v, found := c.Get(indexToString(i))
			if found {
				if v != indexToString(i) {
					b.Fatal("uh oh")
				}
			}
		}
	})

}

func BenchmarkCache_StringString_ParallelPut(b *testing.B) {

	b.RunParallel(func(pb *testing.PB) {
		ctx, cancel := context.WithCancel(context.Background())
		c := New[string, string](ctx, 100, runtime.NumCPU())
		defer c.Wait()
		defer cancel()
		i := 0
		for pb.Next() {
			c.Put(indexToString(i), indexToString(i))
			i++
		}
	})
}

func BenchmarkCache_StringString_ParallelGet(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		ctx, cancel := context.WithCancel(context.Background())
		c := New[string, string](ctx, 100, runtime.NumCPU())
		defer c.Wait()
		defer cancel()

		for i := 0; i < 100*runtime.NumCPU(); i++ {
			c.Put(indexToString(i), indexToString(i))
		}

		i := 0
		for pb.Next() {
			v, found := c.Get(indexToString(i))
			if found {
				if v != indexToString(i) {
					b.Fatal("uh oh")
				}
			}
			i++
		}
	})
}
