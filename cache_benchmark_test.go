package cache

import (
	"context"
	"runtime"
	"testing"
)

func BenchmarkCache_SingleThread(b *testing.B) {

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
			c.Get(i)
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
			c.Get(i)
			i++
		}
	})
}
