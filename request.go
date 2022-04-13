package cache

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
