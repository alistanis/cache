package cache

// KVPair is a key value pair
type KVPair[K comparable, V any] struct {
	Key K
	Val V
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

// metaResponse is a structure for returning metadata responses, in this case, just the Length and Capacity of the
// current lruCache
type metaResponse struct {
	// Len is the length of the lruCache
	Len int
	// Cap is the capacity of the lruCache
	Cap int
}

// MetaResponse is a slice of metaResponse
type MetaResponse []metaResponse

// Len returns the aggregate length of all metaResponses (number of items in cache)
func (m MetaResponse) Len() (length int) {
	for _, mr := range m {
		length += mr.Len
	}
	return
}

// Cap returns the total capacity of all metaResponses (total cache capacity)
func (m MetaResponse) Cap() (capacity int) {
	for _, mr := range m {
		capacity += mr.Cap
	}
	return
}
