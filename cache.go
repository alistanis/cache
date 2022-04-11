//
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

	GetRequest    *RequestChannel[Request[K, GetResponse[K, V]], GetResponse[K, V]]
	PutRequest    *RequestChannel[Request[KVPair[K, V], struct{}], struct{}]
	RemoveRequest *RequestChannel[Request[K, bool], bool]

	EachRequest *RequestChannel[Request[func(K, V), struct{}], struct{}]
	MetaRequest *RequestChannel[Request[struct{}, MetaResponse], MetaResponse]
	size        int
	capacity    int
}

// MetaResponse is a structure for returning metadata responses, in this case, just the Length and Capacity of the
// current cache
type MetaResponse struct {
	Len int
	Cap int
}

// Cache is a thread safe and lockless in memory cache object. This is achieved by partitioning values across many
// smaller LRU caches and interacting with those caches over channels. Each smaller cache maintains access to its own
// elements and communicates information back to the Cache object, which then responds back to the original caller.
type Cache[K comparable, V any] struct {
	caches  []*cache[K, V]
	clients []*Client[K, V]

	GetRequest    *RequestChannel[Request[K, GetResponse[K, V]], GetResponse[K, V]]
	PutRequest    *RequestChannel[Request[KVPair[K, V], struct{}], struct{}]
	RemoveRequest *RequestChannel[Request[K, bool], bool]

	EachRequest *RequestChannel[Request[func(K, V), struct{}], struct{}]

	MetaRequest *RequestChannel[Request[struct{}, MetaResponse], MetaResponse]
}

// New initializes and returns a Cache object. Each internal cache runs its own goroutine, the number of which is
// determined by the 'concurrency' parameter
func New[K comparable, V any](ctx context.Context, capacityPerPartition, concurrency int) *Cache[K, V] {

	g := &Cache[K, V]{caches: make([]*cache[K, V], 0, concurrency), clients: make([]*Client[K, V], 0, concurrency)}
	for i := 0; i < concurrency; i++ {
		g.caches = append(g.caches, newCache[K, V](capacityPerPartition))
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

// Wait waits to close all clients and channels associated with this cache
func (g *Cache[K, V]) Wait() {
	for _, c := range g.clients {
		c.Wait()
	}
}

// newCache returns a new cache with the given capacity.
func newCache[K comparable, V any](capacity int) *cache[K, V] {

	c := &cache[K, V]{
		table:         make(map[K]*list.Node[KVPair[K, V]]),
		list:          list.New[KVPair[K, V]](),
		size:          0,
		capacity:      capacity,
		GetRequest:    NewRequestChannel[Request[K, GetResponse[K, V]], GetResponse[K, V]](),
		PutRequest:    NewRequestChannel[Request[KVPair[K, V], struct{}], struct{}](),
		RemoveRequest: NewRequestChannel[Request[K, bool], bool](),
		EachRequest:   NewRequestChannel[Request[func(K, V), struct{}], struct{}](),
		MetaRequest:   NewRequestChannel[Request[struct{}, MetaResponse], MetaResponse](),
	}

	return c
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

	kv := KVPair[K, V]{
		Key: k,
		Val: e,
	}

	n := c.list.PushFront(kv)
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

	c.list.Remove(e)
	c.size--
	delete(c.table, kv.Key)
}

// Remove causes the kv pair associated with the given key to be immediately
// evicted from the cache.
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

// Resize changes the maximum capacity for this cache to 'size'.
func (c *cache[K, V]) Resize(size int) {
	if c.capacity == size {
		return
	} else if c.capacity < size {
		c.capacity = size
		return
	}

	if c.size == size {
		c.capacity = size
		return
	}

	for i := 0; i < c.capacity-size; i++ {
		c.evict()
	}

	c.capacity = size
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

// Request is a request object sent over a channel. The Response type is used to create a Response chan.
type Request[T any, Response any] struct {
	ResponseChannel chan Response
	RequestBody     T
}

// NewRequest creates a new Request
func NewRequest[T any, Response any](request T, responseChannel chan Response) *Request[T, Response] {
	return &Request[T, Response]{responseChannel, request}
}

// RequestChannel is a struct wrapper around a generic request channel type
type RequestChannel[Request any, Response any] struct {
	channel chan *Request
}

// NewRequestChannel returns a new RequestChannel wrapper for Request and Response over channels
func NewRequestChannel[Request any, Response any]() *RequestChannel[Request, Response] {
	return &RequestChannel[Request, Response]{channel: make(chan *Request)}
}

// Close shuts the RequestChannel channel down
func (r RequestChannel[Request, Response]) Close() {
	close(r.channel)
}

// GetResponse is a struct wrapper for a Cache Get response
type GetResponse[K comparable, V any] struct {
	KVPair[K, V]
	Found bool
}

// Client is a structure responsible for talking to cache objects
type Client[K comparable, V any] struct {
	GetRequest         *RequestChannel[Request[K, GetResponse[K, V]], GetResponse[K, V]]
	GetResponseChannel chan GetResponse[K, V]

	PutRequest         *RequestChannel[Request[KVPair[K, V], struct{}], struct{}]
	PutResponseChannel chan struct{}

	RemoveRequest         *RequestChannel[Request[K, bool], bool]
	RemoveResponseChannel chan bool

	EachRequest         *RequestChannel[Request[func(K, V), struct{}], struct{}]
	EachResponseChannel chan struct{}

	MetaRequest             *RequestChannel[Request[struct{}, MetaResponse], MetaResponse]
	MetaDataResponseChannel chan MetaResponse

	finished chan struct{}
}

// Get gets an object associated with key k
func (c *Client[K, V]) Get(k K) GetResponse[K, V] {
	c.GetRequest.channel <- NewRequest[K, GetResponse[K, V]](k, c.GetResponseChannel)
	return <-c.GetResponseChannel
}

// Put puts an object with key k and value v
func (c *Client[K, V]) Put(k K, v V) {
	pair := KVPair[K, V]{k, v}
	req := NewRequest[KVPair[K, V], struct{}](pair, c.PutResponseChannel)

	c.PutRequest.channel <- req
	<-c.PutResponseChannel
}

// Remove deletes a value associated with key k
func (c *Client[K, V]) Remove(k K) bool {
	c.RemoveRequest.channel <- NewRequest[K, bool](k, c.RemoveResponseChannel)
	return <-c.RemoveResponseChannel
}

// Each runs the function fn on each value associated with this client's cache
func (c *Client[K, V]) Each(fn func(k K, v V)) {
	c.EachRequest.channel <- NewRequest[func(K, V), struct{}](fn, c.EachResponseChannel)
	<-c.EachResponseChannel
}

// Meta returns metadata about this client's cache
func (c *Client[K, V]) Meta() (data MetaResponse) {
	c.MetaRequest.channel <- NewRequest[struct{}, MetaResponse](struct{}{}, c.MetaDataResponseChannel)
	return <-c.MetaDataResponseChannel
}

// Wait waits until all work is finished for this client and closes all communication channels
func (c *Client[K, V]) Wait() {

	<-c.finished
	c.GetRequest.Close()
	c.PutRequest.Close()
	c.EachRequest.Close()
	c.RemoveRequest.Close()
	c.MetaRequest.Close()
}

// NewClient returns a new client initialized for work
func (c *cache[K, V]) NewClient() *Client[K, V] {
	return &Client[K, V]{
		c.GetRequest, make(chan GetResponse[K, V]),
		c.PutRequest, make(chan struct{}),
		c.RemoveRequest, make(chan bool),
		c.EachRequest, make(chan struct{}),
		c.MetaRequest, make(chan MetaResponse),
		make(chan struct{})}
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
			case k := <-c.GetRequest.channel:
				v, found := c.Get(k.RequestBody)
				if found {
					k.ResponseChannel <- GetResponse[K, V]{KVPair: KVPair[K, V]{Key: k.RequestBody, Val: v}, Found: true}
				} else {
					k.ResponseChannel <- GetResponse[K, V]{KVPair: KVPair[K, V]{}, Found: false}
				}
			case kv := <-c.PutRequest.channel:
				c.Put(kv.RequestBody.Key, kv.RequestBody.Val)
				kv.ResponseChannel <- struct{}{}

			case k := <-c.RemoveRequest.channel:
				k.ResponseChannel <- c.Remove(k.RequestBody)

			case e := <-c.EachRequest.channel:
				c.Each(e.RequestBody)
				e.ResponseChannel <- struct{}{}

			case v := <-c.MetaRequest.channel:
				v.ResponseChannel <- MetaResponse{Len: c.Size(), Cap: c.Capacity()}
			}

		}

	}(ctx)
	return
}
