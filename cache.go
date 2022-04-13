// Copyright 2022, 2009 Christopher Cooper, the Go Authors. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file
// and the BSD-Style license in the Go SDK.

// Package cache implements a thread safe and lock free LRU (Least Recently Used) cache
package cache

import (
	"context"
	"github.com/alistanis/cache/list"
	"github.com/mitchellh/hashstructure/v2"
)

// lruCache is a generic LRU lruCache backed with a doubly linked list and a map
type lruCache[K comparable, V any] struct {
	table map[K]*list.Node[KVPair[K, V]]
	list  *list.List[KVPair[K, V]]

	// GetChannel is a request channel parameterized for getting values from the lruCache
	GetChannel *RequestChannel[Request[K], GetResponse[K, V]]
	// PutChannel is a channel parameterized for putting values into the lruCache
	PutChannel *RequestChannel[Request[KVPair[K, V]], struct{}]
	// RemoveChannel is a channel parameterized for removing values from the lruCache
	RemoveChannel *RequestChannel[Request[K], bool]
	// EachChannel is a channel parameterized for running a function on each object in this lruCache
	EachChannel *RequestChannel[Request[FnWrap[K, V]], struct{}]
	// MetaChannel is a channel parameterized for retrieving metadata about this lruCache
	MetaChannel *RequestChannel[Request[struct{}], metaResponse]
	// ResizeChannel is a channel parameterized for resizing this lruCache
	ResizeChannel *RequestChannel[Request[int], int]
	// EvictionChannel is a channel parameterized for evicting this lruCache
	EvictionChannel *RequestChannel[Request[int], int]

	client  *client[K, V]
	evictFn func(k K, v V)

	size     int
	capacity int
}

// Cache is a thread safe and lockless in memory lruCache object. This is achieved by partitioning values across many
// smaller LRU caches and interacting with those caches over channels. Each smaller lruCache maintains access to its own
// elements and communicates information back to the Cache object, which then responds back to the original caller.
type Cache[K comparable, V any] struct {
	caches []*lruCache[K, V]
}

// New initializes and returns a Cache object. Each internal lruCache runs its own goroutine, the number of which is
// determined by the 'concurrency' parameter. The total number of elements that can be placed in the Cache at any time is
// `capacityPerPartition * concurrency`
func New[K comparable, V any](ctx context.Context, capacityPerPartition, concurrency int) *Cache[K, V] {
	return WithEvictionFunction[K, V](ctx, capacityPerPartition, concurrency, nil)
}

// WithEvictionFunction returns a new *Cache that will call an eviction function on every *list.Node when it is evicted from
// its *cache
func WithEvictionFunction[K comparable, V any](ctx context.Context, capacityPerPartition, concurrency int, fn func(k K, v V)) *Cache[K, V] {
	g := &Cache[K, V]{caches: make([]*lruCache[K, V], 0, concurrency)}
	for i := 0; i < concurrency; i++ {
		c := newLruCacheWithEvictionFunction[K, V](capacityPerPartition, fn)
		c.client = c.serve(ctx)
		g.caches = append(g.caches, c)
	}

	return g
}

// Put puts an object in the lruCache with the key k and value v
func (g *Cache[K, V]) Put(k K, v V) {
	hash, _ := hashstructure.Hash(k, hashstructure.FormatV2, nil)

	index := hash % uint64(len(g.caches))
	g.caches[index].client.Put(k, v)
}

// Get retrieves a value from the lruCache associated with key k, returning value v and bool found
func (g *Cache[K, V]) Get(k K) (v V, found bool) {
	hash, _ := hashstructure.Hash(k, hashstructure.FormatV2, nil)

	index := hash % uint64(len(g.caches))
	resp := g.caches[index].client.Get(k)
	return resp.Val, resp.Found
}

// Remove removes an object from the lruCache associated with key k, returning found
func (g *Cache[K, V]) Remove(k K) (found bool) {
	hash, _ := hashstructure.Hash(k, hashstructure.FormatV2, nil)

	index := hash % uint64(len(g.caches))
	return g.caches[index].client.Remove(k)
}

// Each calls the function f on each element in the lruCache. This call is thread safe but not atomic across lruCache
// boundaries. This call is not particularly recommended if atomic consistency across the entire lruCache is needed
// and other goroutines may be mutating values in the lruCache. It is consistent when actively processing values
// inside an individual lruCache partition, however.
func (g *Cache[K, V]) Each(f func(k K, v V)) {
	for _, c := range g.caches {
		c.client.Each(f)
	}
}

// Meta returns metadata about the lruCache, namely Length and Capacity
func (g *Cache[K, V]) Meta() (data MetaResponse) {
	for _, c := range g.caches {
		d := c.client.Meta()
		data = append(data, d)
	}
	return
}

// Resize resizes all *lruCaches to the given size
func (g *Cache[K, V]) Resize(i int) int {
	evicted := 0
	for _, c := range g.caches {
		evicted += c.client.Resize(i)
	}
	return evicted
}

// Evict the entire lruCache
func (g *Cache[K, V]) Evict() int {
	return g.EvictTo(0)
}

// EvictTo down to i per lruCache
func (g *Cache[K, V]) EvictTo(i int) int {
	evicted := 0
	for _, c := range g.caches {
		evicted += c.client.EvictTo(i)
	}
	return evicted
}

// Wait waits to close all clients and channels associated with this lruCache
func (g *Cache[K, V]) Wait() {
	for _, c := range g.caches {
		c.client.Wait()
	}
}

// Client returns the *client for g.caches[index]
// This can be used to manually tune individual caches or to
// build your own partitioning algorithm on top of the existing LRUs.
// Calling functions on the returned *client object is thread safe.
func (g *Cache[K, V]) Client(index int) *client[K, V] {
	return g.caches[index].client
}

// newLruCache returns a new lruCache with the given capacity.
func newLruCache[K comparable, V any](capacity int) *lruCache[K, V] {
	return newLruCacheWithEvictionFunction[K, V](capacity, nil)
}

func newLruCacheWithEvictionFunction[K comparable, V any](capacity int, fn func(k K, v V)) *lruCache[K, V] {

	return &lruCache[K, V]{
		table:           make(map[K]*list.Node[KVPair[K, V]]),
		list:            list.New[KVPair[K, V]](),
		size:            0,
		capacity:        capacity,
		GetChannel:      NewRequestChannel[Request[K], GetResponse[K, V]](),
		PutChannel:      NewRequestChannel[Request[KVPair[K, V]], struct{}](),
		RemoveChannel:   NewRequestChannel[Request[K], bool](),
		EachChannel:     NewRequestChannel[Request[FnWrap[K, V]], struct{}](),
		MetaChannel:     NewRequestChannel[Request[struct{}], metaResponse](),
		ResizeChannel:   NewRequestChannel[Request[int], int](),
		EvictionChannel: NewRequestChannel[Request[int], int](),
		evictFn:         fn,
	}
}

// Get returns the value associated with a given key, and a boolean indicating
// whether the key exists in the table.
func (c *lruCache[K, V]) Get(k K) (V, bool) {
	if n, ok := c.table[k]; ok {
		c.list.MoveToFront(n)
		return n.Value.Val, true
	}
	var v V
	return v, false
}

// Put adds a new key/value pair to the table.
func (c *lruCache[K, V]) Put(k K, e V) {
	if _, ok := c.table[k]; ok {
		c.Remove(k)
	}

	if c.size == c.capacity {
		c.evict()
	}

	n := c.list.PushFront(KVPair[K, V]{
		Key: k,
		Val: e,
	})
	c.size++
	c.table[k] = n
}

// evict removes the least recently used value in this lruCache
func (c *lruCache[K, V]) evict() {
	e := c.list.Back()
	if e == nil {
		return
	}
	kv := e.Value
	if c.evictFn != nil {
		c.evictFn(kv.Key, kv.Val)
	}
	c.list.Remove(e)
	c.size--
	delete(c.table, kv.Key)
}

// Remove causes the kv pair associated with the given key to be immediately
// removed from the lruCache.
func (c *lruCache[K, V]) Remove(k K) bool {
	var ok bool
	var n *list.Node[KVPair[K, V]]
	if n, ok = c.table[k]; ok {
		c.list.Remove(n)
		c.size--
		delete(c.table, k)
	}
	return ok
}

func (c *lruCache[K, V]) EvictTo(size int) int {
	if size < 0 || c.size <= size {
		return 0
	}
	evicted := c.size - size
	for i := c.size; i > size; i-- {
		c.evict()
	}
	return evicted
}

// Resize changes the maximum capacity for this lruCache to 'size'.
func (c *lruCache[K, V]) Resize(size int) int {
	if c.capacity == size {
		return 0
	} else if c.capacity < size {
		c.capacity = size
		return 0
	}

	if c.size == size {
		c.capacity = size
		return 0
	}

	evicted := c.capacity - size
	for i := 0; i < c.capacity-size; i++ {
		c.evict()
	}

	c.capacity = size
	return evicted
}

// Size returns the number of active elements in the lruCache.
func (c *lruCache[K, V]) Size() int {
	return c.size
}

// Capacity returns the maximum capacity of the lruCache.
func (c *lruCache[K, V]) Capacity() int {
	return c.capacity
}

// Each calls 'fn' on every value in the lruCache, from most recently used to
// least recently used.
func (c *lruCache[K, V]) Each(fn func(K, V)) {
	for e := c.list.Front(); e != c.list.Back().Next; e = e.Next {
		kv := e.Value
		fn(kv.Key, kv.Val)
	}
}

// NewClient returns a new client initialized for work
func (c *lruCache[K, V]) NewClient() *client[K, V] {
	return &client[K, V]{
		c.GetChannel,
		c.PutChannel,
		c.RemoveChannel,
		c.EachChannel,
		c.MetaChannel,
		c.ResizeChannel,
		c.EvictionChannel,
		make(chan struct{}),
	}
}

func (c *lruCache[K, V]) handleGetRequest(r *Request[K]) {
	v, found := c.Get(r.RequestBody)
	if found {
		c.GetChannel.response <- GetResponse[K, V]{KVPair: KVPair[K, V]{Key: r.RequestBody, Val: v}, Found: true}
	} else {
		c.GetChannel.response <- GetResponse[K, V]{KVPair: KVPair[K, V]{}, Found: false}
	}
}

// serve returns a new client and starts a goroutine responsible for handling all IO for this lruCache
func (c *lruCache[K, V]) serve(ctx context.Context) (client *client[K, V]) {

	client = c.NewClient()
	go func(ctx context.Context) {

		for {
			select {
			case <-ctx.Done():
				client.finished <- struct{}{}
				return

			case gr := <-c.GetChannel.request:
				c.handleGetRequest(gr)

			case kv := <-c.PutChannel.request:
				c.Put(kv.RequestBody.Key, kv.RequestBody.Val)
				c.PutChannel.response <- struct{}{}

			case k := <-c.RemoveChannel.request:
				c.RemoveChannel.response <- c.Remove(k.RequestBody)

			case e := <-c.EachChannel.request:
				c.Each(e.RequestBody.fn)
				c.EachChannel.response <- struct{}{}

			case <-c.MetaChannel.request:
				c.MetaChannel.response <- metaResponse{Len: c.Size(), Cap: c.Capacity()}

			case r := <-c.ResizeChannel.request:
				c.ResizeChannel.response <- c.Resize(r.RequestBody)

			case i := <-c.EvictionChannel.request:
				c.EvictionChannel.response <- c.EvictTo(i.RequestBody)

			}

		}

	}(ctx)
	return
}
