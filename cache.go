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

// KVPair is a key value pair
type KVPair[K comparable, V any] struct {
	Key K
	Val V
}

// cache is a generic LRU cache backed with a doubly linked list and a map
type cache[K comparable, V any] struct {
	table map[K]*list.Node[KVPair[K, V]]
	list  *list.List[KVPair[K, V]]

	// GetChannel is a request channel parameterized for getting values from the cache
	GetChannel *RequestChannel[Request[K], GetResponse[K, V]]
	// PutChannel is a channel parameterized for putting values into the cache
	PutChannel *RequestChannel[Request[KVPair[K, V]], struct{}]
	// RemoveChannel is a channel parameterized for removing values from the cache
	RemoveChannel *RequestChannel[Request[K], bool]
	// EachChannel is a channel parameterized for running a function on each object in this cache
	EachChannel *RequestChannel[Request[FnWrap[K, V]], struct{}]
	// MetaChannel is a channel parameterized for retrieving metadata about this cache
	MetaChannel *RequestChannel[Request[struct{}], MetaResponse]
	// ResizeChannel is a channel parameterized for resizing this cache
	ResizeChannel *RequestChannel[Request[int], int]
	// EvictionChannel is a channel parameterized for evicting this cache
	EvictionChannel *RequestChannel[Request[int], int]
	evictFn         func(k K, v V)

	size     int
	capacity int
}

// MetaResponse is a structure for returning metadata responses, in this case, just the Length and Capacity of the
// current cache
type MetaResponse struct {
	// Len is the length of the cache
	Len int
	// Cap is the capacity of the cache
	Cap int
}

// Cache is a thread safe and lockless in memory cache object. This is achieved by partitioning values across many
// smaller LRU caches and interacting with those caches over channels. Each smaller cache maintains access to its own
// elements and communicates information back to the Cache object, which then responds back to the original caller.
type Cache[K comparable, V any] struct {
	caches  []*cache[K, V]
	clients []*Client[K, V]
}

// New initializes and returns a Cache object. Each internal cache runs its own goroutine, the number of which is
// determined by the 'concurrency' parameter
func New[K comparable, V any](ctx context.Context, capacityPerPartition, concurrency int) *Cache[K, V] {
	return NewWithEvictionFunction[K, V](ctx, capacityPerPartition, concurrency, nil)
}

func NewWithEvictionFunction[K comparable, V any](ctx context.Context, capacityPerPartition, concurrency int, fn func(k K, v V)) *Cache[K, V] {
	g := &Cache[K, V]{caches: make([]*cache[K, V], 0, concurrency), clients: make([]*Client[K, V], 0, concurrency)}
	for i := 0; i < concurrency; i++ {
		g.caches = append(g.caches, newCacheWithEvictionFunction[K, V](capacityPerPartition, fn))
	}
	for _, c := range g.caches {
		g.clients = append(g.clients, c.Serve(ctx))
	}
	return g
}

// Put puts an object in the cache with the key k and value v
func (g *Cache[K, V]) Put(k K, v V) {
	hash, _ := hashstructure.Hash(k, hashstructure.FormatV2, nil)

	index := hash % uint64(len(g.caches))
	g.clients[index].Put(k, v)

}

// Get retrieves a value from the cache associated with key k, returning value v and bool found
func (g *Cache[K, V]) Get(k K) (v V, found bool) {
	hash, _ := hashstructure.Hash(k, hashstructure.FormatV2, nil)

	index := hash % uint64(len(g.caches))
	resp := g.clients[index].Get(k)
	return resp.Val, resp.Found
}

// Remove removes an object from the cache associated with key k, returning found
func (g *Cache[K, V]) Remove(k K) (found bool) {
	hash, _ := hashstructure.Hash(k, hashstructure.FormatV2, nil)

	index := hash % uint64(len(g.caches))
	return g.clients[index].Remove(k)
}

// Each calls the function f on each element in the cache. This call is thread safe but not atomic across cache
// boundaries. This call is not particularly recommended if atomic consistency across the entire cache is needed
// and other goroutines may be mutating values in the cache. It is consistent when actively processing values
// inside an individual cache partition, however.
func (g *Cache[K, V]) Each(f func(k K, v V)) {
	for _, c := range g.clients {
		c.Each(f)
	}
}

// Meta returns metadata about the cache, namely Length and Capacity
func (g *Cache[K, V]) Meta() (data MetaResponse) {
	for _, c := range g.clients {
		d := c.Meta()
		data.Len += d.Len
		data.Cap += d.Cap
	}
	return
}

func (g *Cache[K, V]) Resize(i int) int {
	evicted := 0
	for _, c := range g.clients {
		evicted += c.Resize(i)
	}
	return evicted
}

// Evict the entire cache
func (g *Cache[K, V]) Evict() int {
	return g.EvictTo(0)
}

// EvictTo down to i per cache
func (g *Cache[K, V]) EvictTo(i int) int {
	evicted := 0
	for _, c := range g.clients {
		evicted += c.EvictTo(i)
	}
	return evicted
}

// Wait waits to close all clients and channels associated with this cache
func (g *Cache[K, V]) Wait() {
	for _, c := range g.clients {
		c.Wait()
	}
}

// newCache returns a new cache with the given capacity.
func newCache[K comparable, V any](capacity int) *cache[K, V] {
	return newCacheWithEvictionFunction[K, V](capacity, nil)
}

func newCacheWithEvictionFunction[K comparable, V any](capacity int, fn func(k K, v V)) *cache[K, V] {

	return &cache[K, V]{
		table:           make(map[K]*list.Node[KVPair[K, V]]),
		list:            list.New[KVPair[K, V]](),
		size:            0,
		capacity:        capacity,
		GetChannel:      NewRequestChannel[Request[K], GetResponse[K, V]](),
		PutChannel:      NewRequestChannel[Request[KVPair[K, V]], struct{}](),
		RemoveChannel:   NewRequestChannel[Request[K], bool](),
		EachChannel:     NewRequestChannel[Request[FnWrap[K, V]], struct{}](),
		MetaChannel:     NewRequestChannel[Request[struct{}], MetaResponse](),
		ResizeChannel:   NewRequestChannel[Request[int], int](),
		EvictionChannel: NewRequestChannel[Request[int], int](),
		evictFn:         fn,
	}
}

// Get returns the value associated with a given key, and a boolean indicating
// whether the key exists in the table.
func (c *cache[K, V]) Get(k K) (V, bool) {
	if n, ok := c.table[k]; ok {
		c.list.MoveToFront(n)
		return n.Value.Val, true
	}
	var v V
	return v, false
}

// Put adds a new key/value pair to the table.
func (c *cache[K, V]) Put(k K, e V) {
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

// evict removes the least recently used value in this cache
func (c *cache[K, V]) evict() {
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
// removed from the cache.
func (c *cache[K, V]) Remove(k K) bool {
	var ok bool
	var n *list.Node[KVPair[K, V]]
	if n, ok = c.table[k]; ok {
		c.list.Remove(n)
		c.size--
		delete(c.table, k)
	}
	return ok
}

func (c *cache[K, V]) EvictTo(size int) int {
	if size < 0 || c.size <= size {
		return 0
	}
	evicted := c.size - size
	for i := c.size; i > size; i-- {
		c.evict()
	}
	return evicted
}

// Resize changes the maximum capacity for this cache to 'size'.
func (c *cache[K, V]) Resize(size int) int {
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

// Size returns the number of active elements in the cache.
func (c *cache[K, V]) Size() int {
	return c.size
}

// Capacity returns the maximum capacity of the cache.
func (c *cache[K, V]) Capacity() int {
	return c.capacity
}

// Each calls 'fn' on every value in the cache, from most recently used to
// least recently used.
func (c *cache[K, V]) Each(fn func(K, V)) {
	for e := c.list.Front(); e != c.list.Back().Next; e = e.Next {
		kv := e.Value
		fn(kv.Key, kv.Val)
	}
}

// Request is a request object sent over a request. The Response type is used to create a Response chan.
type Request[T any] struct {
	// RequestBody is the value of the request key
	RequestBody T
}

// NewRequest creates a new Request
func NewRequest[T any](request T) *Request[T] {
	return &Request[T]{request}
}

// RequestChannel is a struct wrapper around a generic request request type
type RequestChannel[Request any, Response any] struct {
	request  chan *Request
	response chan Response
}

// NewRequestChannel returns a new RequestChannel wrapper for Request and Response over channels
func NewRequestChannel[Request any, Response any]() *RequestChannel[Request, Response] {
	return &RequestChannel[Request, Response]{request: make(chan *Request), response: make(chan Response)}
}

// Close shuts the RequestChannel request down
func (r RequestChannel[Request, Response]) Close() {
	close(r.request)
	close(r.response)
}

// GetResponse is a struct wrapper for a Cache Get response
type GetResponse[K comparable, V any] struct {
	KVPair[K, V]
	Found bool
}

// FnWrap is a wrapper function for passing a generic function over a channel. On at least 1.18.1, sending
// a function over a channel with a non-primitive pointer type seems to implicitly convert the pointer type
// to *uint8 and causes compilation to fail.
type FnWrap[K comparable, V any] struct {
	fn func(k K, v V)
}

// Client is a structure responsible for talking to cache objects
type Client[K comparable, V any] struct {

	// GetChannel is a channel for retrieving values from the cache for which this client is associated
	GetChannel *RequestChannel[Request[K], GetResponse[K, V]]

	// PutChannel is a channel for placing values into the cache for which this client is associated
	PutChannel *RequestChannel[Request[KVPair[K, V]], struct{}]

	// RemoveChannel is a channel for removing values from the cache for which this client is associated
	RemoveChannel *RequestChannel[Request[K], bool]

	// EachChannel is a channel for running functions on each element in the cache for which this client is associated
	EachChannel *RequestChannel[Request[FnWrap[K, V]], struct{}]

	// MetaChannel is a channel for requesting metadata about ths cache for which this client is associated
	MetaChannel *RequestChannel[Request[struct{}], MetaResponse]

	// ResizeChannel is a channel for resizing the cache
	ResizeChannel *RequestChannel[Request[int], int]

	// EvictionChannel is a channel for evicting the cache
	EvictionChannel *RequestChannel[Request[int], int]

	finished chan struct{}
}

// Get gets an object associated with key k
func (c *Client[K, V]) Get(k K) GetResponse[K, V] {
	c.GetChannel.request <- NewRequest[K](k)
	return <-c.GetChannel.response
}

// Put puts an object with key k and value v
func (c *Client[K, V]) Put(k K, v V) {
	pair := KVPair[K, V]{k, v}
	req := NewRequest[KVPair[K, V]](pair)

	c.PutChannel.request <- req
	<-c.PutChannel.response
}

// Remove deletes a value associated with key k
func (c *Client[K, V]) Remove(k K) bool {
	c.RemoveChannel.request <- NewRequest[K](k)
	return <-c.RemoveChannel.response
}

// Each runs the function fn on each value associated with this client's cache
func (c *Client[K, V]) Each(fn func(k K, v V)) {
	c.EachChannel.request <- NewRequest[FnWrap[K, V]](FnWrap[K, V]{fn})
	<-c.EachChannel.response
}

// Meta returns metadata about this client's cache
func (c *Client[K, V]) Meta() (data MetaResponse) {
	c.MetaChannel.request <- NewRequest[struct{}](struct{}{})
	return <-c.MetaChannel.response
}

// Resize resizes this client's cache to i
func (c *Client[K, V]) Resize(i int) int {
	c.ResizeChannel.request <- NewRequest[int](i)
	return <-c.ResizeChannel.response
}

// EvictTo evicts this client's cache until it has reached size i
func (c *Client[K, V]) EvictTo(i int) int {
	c.EvictionChannel.request <- NewRequest[int](i)
	return <-c.EvictionChannel.response
}

// Wait waits until all work is finished for this client and closes all communication channels
func (c *Client[K, V]) Wait() {

	<-c.finished
	c.GetChannel.Close()
	c.PutChannel.Close()
	c.EachChannel.Close()
	c.RemoveChannel.Close()
	c.MetaChannel.Close()
	c.EvictionChannel.Close()
	c.ResizeChannel.Close()
}

// NewClient returns a new client initialized for work
func (c *cache[K, V]) NewClient() *Client[K, V] {
	return &Client[K, V]{
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

func (c *cache[K, V]) handleGetRequest(r *Request[K]) {
	v, found := c.Get(r.RequestBody)
	if found {
		c.GetChannel.response <- GetResponse[K, V]{KVPair: KVPair[K, V]{Key: r.RequestBody, Val: v}, Found: true}
	} else {
		c.GetChannel.response <- GetResponse[K, V]{KVPair: KVPair[K, V]{}, Found: false}
	}
}

// Serve returns a new client and starts a goroutine responsible for handling all IO for this cache
func (c *cache[K, V]) Serve(ctx context.Context) (client *Client[K, V]) {

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
				c.MetaChannel.response <- MetaResponse{Len: c.Size(), Cap: c.Capacity()}

			case r := <-c.ResizeChannel.request:
				c.ResizeChannel.response <- c.Resize(r.RequestBody)

			case i := <-c.EvictionChannel.request:
				c.EvictionChannel.response <- c.EvictTo(i.RequestBody)

			}

		}

	}(ctx)
	return
}
