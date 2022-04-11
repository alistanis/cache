package cache

import (
	"context"
	"github.com/mitchellh/hashstructure/v2"
)

// KVPair is a key value pair
type KVPair[K comparable, V any] struct {
	Key K
	Val V
}

// cache is a generic LRU cache backed with a doubly linked list and a map
type cache[K comparable, V any] struct {
	table map[K]*Node[KVPair[K, V]]
	list  *List[KVPair[K, V]]

	GetRequest    *RequestChannel[Request[K, GetResponse[K, V]], GetResponse[K, V]]
	PutRequest    *RequestChannel[Request[KVPair[K, V], struct{}], struct{}]
	RemoveRequest *RequestChannel[Request[K, bool], bool]

	EachRequest *RequestChannel[Request[func(K, V), struct{}], struct{}]
	MetaRequest *RequestChannel[Request[struct{}, MetaResponse], MetaResponse]
	size        int
	capacity    int
}

type MetaResponse struct {
	Len int
	Cap int
}

type Cache[K comparable, V any] struct {
	caches  []*cache[K, V]
	clients []*Client[K, V]

	GetRequest    *RequestChannel[Request[K, GetResponse[K, V]], GetResponse[K, V]]
	PutRequest    *RequestChannel[Request[KVPair[K, V], struct{}], struct{}]
	RemoveRequest *RequestChannel[Request[K, bool], bool]

	EachRequest *RequestChannel[Request[func(K, V), struct{}], struct{}]

	MetaRequest *RequestChannel[Request[struct{}, MetaResponse], MetaResponse]
}

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

func (g *Cache[K, V]) Put(k K, v V) {
	hash, _ := hashstructure.Hash(k, hashstructure.FormatV2, nil)

	index := hash % uint64(len(g.caches))
	g.clients[index].Put(k, v)

}

func (g *Cache[K, V]) Get(k K) (v V, found bool) {
	hash, _ := hashstructure.Hash(k, hashstructure.FormatV2, nil)

	index := hash % uint64(len(g.caches))
	resp := g.clients[index].Get(k)
	return resp.Val, resp.Found
}

func (g *Cache[K, V]) Remove(k K) (found bool) {
	hash, _ := hashstructure.Hash(k, hashstructure.FormatV2, nil)

	index := hash % uint64(len(g.caches))
	return g.clients[index].Remove(k)
}

func (g *Cache[K, V]) Each(f func(k K, v V)) {
	for _, c := range g.clients {
		c.Each(f)
	}
}

func (g *Cache[K, V]) Meta() (data MetaResponse) {
	for _, c := range g.clients {
		d := c.Meta()
		data.Len += d.Len
		data.Cap += d.Cap
	}
	return
}

func (g *Cache[K, V]) Wait() {
	for _, c := range g.clients {
		c.Wait()
	}
}

// newCache returns a new cache with the given capacity.
func newCache[K comparable, V any](capacity int) *cache[K, V] {

	c := &cache[K, V]{
		table:         make(map[K]*Node[KVPair[K, V]]),
		list:          NewList[KVPair[K, V]](),
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
	var n *Node[KVPair[K, V]]
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

type Request[T any, Response any] struct {
	ResponseChannel chan Response
	RequestBody     T
}

func NewRequest[T any, Response any](request T, responseChannel chan Response) *Request[T, Response] {
	return &Request[T, Response]{responseChannel, request}
}

type RequestChannel[Request any, Response any] struct {
	channel chan *Request
}

func NewRequestChannel[Request any, Response any]() *RequestChannel[Request, Response] {
	return &RequestChannel[Request, Response]{channel: make(chan *Request)}
}

func (r RequestChannel[Request, Response]) Close() {
	close(r.channel)
}

type GetResponse[K comparable, V any] struct {
	KVPair[K, V]
	Found bool
}

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

func (c *Client[K, V]) Get(k K) GetResponse[K, V] {
	c.GetRequest.channel <- NewRequest[K, GetResponse[K, V]](k, c.GetResponseChannel)
	return <-c.GetResponseChannel
}

func (c *Client[K, V]) Put(k K, v V) {
	pair := KVPair[K, V]{k, v}
	req := NewRequest[KVPair[K, V], struct{}](pair, c.PutResponseChannel)

	c.PutRequest.channel <- req
	<-c.PutResponseChannel
}

func (c *Client[K, V]) Remove(k K) bool {
	c.RemoveRequest.channel <- NewRequest[K, bool](k, c.RemoveResponseChannel)
	return <-c.RemoveResponseChannel
}

func (c *Client[K, V]) Each(fn func(k K, v V)) {
	c.EachRequest.channel <- NewRequest[func(K, V), struct{}](fn, c.EachResponseChannel)
	<-c.EachResponseChannel
}

func (c *Client[K, V]) Meta() (data MetaResponse) {
	c.MetaRequest.channel <- NewRequest[struct{}, MetaResponse](struct{}{}, c.MetaDataResponseChannel)
	return <-c.MetaDataResponseChannel
}

func (c *Client[K, V]) Wait() {

	<-c.finished
	c.GetRequest.Close()
	c.PutRequest.Close()
	c.EachRequest.Close()
	c.RemoveRequest.Close()
	c.MetaRequest.Close()
}

func (c *cache[K, V]) NewClient() *Client[K, V] {
	return &Client[K, V]{
		c.GetRequest, make(chan GetResponse[K, V]),
		c.PutRequest, make(chan struct{}),
		c.RemoveRequest, make(chan bool),
		c.EachRequest, make(chan struct{}),
		c.MetaRequest, make(chan MetaResponse),
		make(chan struct{})}
}

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
