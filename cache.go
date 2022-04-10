package cache

import (
	"context"
	"log"
)

// KVPair is a key value pair
type KVPair[K comparable, V any] struct {
	Key K
	Val V
}

// Cache is a generic LRU cache backed with a doubly linked list and a map
type Cache[K comparable, V any] struct {
	table map[K]*Node[KVPair[K, V]]
	list  *List[KVPair[K, V]]

	GetRequest *RequestChannel[Request[K, GetResponse[K, V]], GetResponse[K, V]]

	PutRequest *RequestChannel[Request[KVPair[K, V], struct{}], struct{}]

	EachRequest *RequestChannel[Request[func(K, V), struct{}], struct{}]

	size     int
	capacity int
}

// New returns a new Cache with the given capacity.
func New[K comparable, V any](capacity int) *Cache[K, V] {
	get := NewRequestChannel[Request[K, GetResponse[K, V]], GetResponse[K, V]]()
	put := NewRequestChannel[Request[KVPair[K, V], struct{}], struct{}]()
	each := NewRequestChannel[Request[func(K, V), struct{}], struct{}]()
	c := &Cache[K, V]{
		table:       make(map[K]*Node[KVPair[K, V]]),
		list:        NewList[KVPair[K, V]](),
		size:        0,
		capacity:    capacity,
		GetRequest:  get,
		PutRequest:  put,
		EachRequest: each,
	}

	return c
}

// Get returns the value associated with a given key, and a boolean indicating
// whether the key exists in the table.
func (c *Cache[K, V]) Get(k K) (V, bool) {
	if n, ok := c.table[k]; ok {
		c.list.MoveToFront(n)
		return n.Value.Val, true
	}
	var v V
	return v, false
}

// Put adds a new key/value pair to the table.
func (c *Cache[K, V]) Put(k K, e V) {
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

func (c *Cache[K, V]) evict() {
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
func (c *Cache[K, V]) Remove(k K) {
	if n, ok := c.table[k]; ok {
		c.list.Remove(n)
		c.size--
		delete(c.table, k)
	}
}

// Resize changes the maximum capacity for this cache to 'size'.
func (c *Cache[K, V]) Resize(size int) {
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
func (c *Cache[K, V]) Size() int {
	return c.size
}

// Capacity returns the maximum capacity of the cache.
func (c *Cache[K, V]) Capacity() int {
	return c.capacity
}

// Each calls 'fn' on every value in the cache, from most recently used to
// least recently used.
func (c *Cache[K, V]) Each(fn func(K, V)) {
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

	EachRequest         *RequestChannel[Request[func(K, V), struct{}], struct{}]
	EachResponseChannel chan struct{}

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

func (c *Client[K, V]) Each(fn func(k K, v V)) {
	c.EachRequest.channel <- NewRequest[func(K, V), struct{}](fn, c.EachResponseChannel)
	<-c.EachResponseChannel
}

func (c *Client[K, V]) Wait() {
	<-c.finished
	c.GetRequest.Close()
	c.PutRequest.Close()
	c.EachRequest.Close()
}

func (c *Cache[K, V]) NewClient() *Client[K, V] {
	return &Client[K, V]{
		c.GetRequest, make(chan GetResponse[K, V]),
		c.PutRequest, make(chan struct{}),
		c.EachRequest, make(chan struct{}),
		make(chan struct{})}
}

func (c *Cache[K, V]) Serve(ctx context.Context) (client *Client[K, V]) {

	client = c.NewClient()
	go func(ctx context.Context) {
		defer func() {
			client.finished <- struct{}{}
		}()

		for {
			select {
			case <-ctx.Done():
				log.Println("Context cancelled, returning from serve")
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
			case e := <-c.EachRequest.channel:
				c.Each(e.RequestBody)
				e.ResponseChannel <- struct{}{}
			}
		}

	}(ctx)

	return
}
