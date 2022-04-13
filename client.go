package cache

// client is a structure responsible for talking to lruCache objects
type client[K comparable, V any] struct {

	// GetChannel is a channel for retrieving values from the lruCache for which this client is associated
	GetChannel *RequestChannel[Request[K], GetResponse[K, V]]

	// PutChannel is a channel for placing values into the lruCache for which this client is associated
	PutChannel *RequestChannel[Request[KVPair[K, V]], struct{}]

	// RemoveChannel is a channel for removing values from the lruCache for which this client is associated
	RemoveChannel *RequestChannel[Request[K], bool]

	// EachChannel is a channel for running functions on each element in the lruCache for which this client is associated
	EachChannel *RequestChannel[Request[FnWrap[K, V]], struct{}]

	// MetaChannel is a channel for requesting metadata about ths lruCache for which this client is associated
	MetaChannel *RequestChannel[Request[struct{}], metaResponse]

	// ResizeChannel is a channel for resizing the lruCache
	ResizeChannel *RequestChannel[Request[int], int]

	// EvictionChannel is a channel for evicting the lruCache
	EvictionChannel *RequestChannel[Request[int], int]

	// MemoryChannel is a channel for reporting memory allocated in this lruCache
	MemoryChannel *RequestChannel[Request[struct{}], int]

	finished chan struct{}
}

// Get gets an object associated with key k
func (c *client[K, V]) Get(k K) GetResponse[K, V] {
	c.GetChannel.request <- NewRequest[K](k)
	return <-c.GetChannel.response
}

// Put puts an object with key k and value v
func (c *client[K, V]) Put(k K, v V) {
	pair := KVPair[K, V]{k, v}
	req := NewRequest[KVPair[K, V]](pair)

	c.PutChannel.request <- req
	<-c.PutChannel.response
}

// Remove deletes a value associated with key k
func (c *client[K, V]) Remove(k K) bool {
	c.RemoveChannel.request <- NewRequest[K](k)
	return <-c.RemoveChannel.response
}

// Each runs the function fn on each value associated with this client's lruCache
func (c *client[K, V]) Each(fn func(k K, v V)) {
	c.EachChannel.request <- NewRequest[FnWrap[K, V]](FnWrap[K, V]{fn})
	<-c.EachChannel.response
}

// Meta returns metadata about this client's lruCache
func (c *client[K, V]) Meta() (data metaResponse) {
	c.MetaChannel.request <- NewRequest[struct{}](struct{}{})
	return <-c.MetaChannel.response
}

// Memory returns a naive sum of memory that this client's lruCache is consuming
func (c *client[K, V]) Memory() int {
	c.MemoryChannel.request <- NewRequest[struct{}](struct{}{})
	return <-c.MemoryChannel.response
}

// Resize resizes this client's lruCache to i
func (c *client[K, V]) Resize(i int) int {
	c.ResizeChannel.request <- NewRequest[int](i)
	return <-c.ResizeChannel.response
}

// EvictTo evicts this client's lruCache until it has reached size i
func (c *client[K, V]) EvictTo(i int) int {
	c.EvictionChannel.request <- NewRequest[int](i)
	return <-c.EvictionChannel.response
}

// Wait waits until all work is finished for this client and closes all communication channels
func (c *client[K, V]) Wait() {

	<-c.finished
	c.GetChannel.Close()
	c.PutChannel.Close()
	c.EachChannel.Close()
	c.RemoveChannel.Close()
	c.MetaChannel.Close()
	c.EvictionChannel.Close()
	c.ResizeChannel.Close()
	c.MemoryChannel.Close()
}
