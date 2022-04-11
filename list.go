package cache

// List implements a doubly-linked list.
type List[E any] struct {
	// root is a sentinel value and is used to access the rest of the list, never directly
	root Node[E]
	len  int
}

// Node is a node in the linked list.
type Node[E any] struct {
	// Prev is the previous Node, Next is the next Node in the list
	Prev, Next *Node[E]
	// List is a pointer to the list that owns this node
	List *List[E]
	// Value is the value of the element stored in this node
	Value E
}

// Init initializes or clears list l.
func (l *List[E]) Init() *List[E] {
	l.root.Next = &l.root
	l.root.Prev = &l.root
	l.len = 0
	return l
}

// lazyInit lazily initializes a zero List value.
func (l *List[E]) lazyInit() {
	if l.root.Next == nil {
		l.Init()
	}
}

// NewList returns an empty linked list.
func NewList[E any]() *List[E] {
	return new(List[E]).Init()
}

// Len returns the number of elements of list l.
// The complexity is O(1).
func (l *List[E]) Len() int { return l.len }

// Front returns the first element of list l or nil if the list is empty.
func (l *List[E]) Front() *Node[E] {
	if l.len == 0 {
		return nil
	}
	return l.root.Next
}

// Back returns the last element of list l or nil if the list is empty.
func (l *List[E]) Back() *Node[E] {
	if l.len == 0 {
		return nil
	}
	return l.root.Prev
}

// insert inserts e after at, increments l.len, and returns e.
func (l *List[E]) insert(e, at *Node[E]) *Node[E] {
	e.Prev = at
	e.Next = at.Next
	e.Prev.Next = e
	e.Next.Prev = e
	e.List = l
	l.len++
	return e
}

// insertValue is a convenience wrapper for insert(&Element{Value: v}, at).
func (l *List[E]) insertValue(v E, at *Node[E]) *Node[E] {
	return l.insert(&Node[E]{Value: v}, at)
}

// remove removes e from its list, decrements l.len
func (l *List[E]) remove(e *Node[E]) {
	e.Prev.Next = e.Next
	e.Next.Prev = e.Prev
	e.Next = nil // avoid memory leaks
	e.Prev = nil // avoid memory leaks
	e.List = nil
	l.len--
}

// move places the Node 'e' next to the Node 'at'.
func (l *List[E]) move(e, at *Node[E]) {
	if e == at {
		return
	}
	e.Prev.Next = e.Next
	e.Next.Prev = e.Prev

	e.Prev = at
	e.Next = at.Next
	e.Prev.Next = e
	e.Next.Prev = e
}

// Remove removes e from l if e is an element of list l.
// It returns the element value e.Value.
// The element must not be nil.
func (l *List[E]) Remove(e *Node[E]) E {
	if e.List == l {
		// if e.list == l, l must have been initialized when e was inserted
		// in l or l == nil (e is a zero Element) and l.remove will crash
		l.remove(e)
	}
	return e.Value
}

// PushFront inserts a new element e with value v at the front of list l and returns e.
func (l *List[E]) PushFront(v E) *Node[E] {
	l.lazyInit()
	return l.insertValue(v, &l.root)
}

// PushBack inserts a new element e with value v at the back of list l and returns e.
func (l *List[E]) PushBack(v E) *Node[E] {
	l.lazyInit()
	return l.insertValue(v, l.root.Prev)
}

// InsertBefore inserts a new element e with value v immediately before mark and returns e.
// If mark is not an element of l, the list is not modified.
// The mark must not be nil.
func (l *List[E]) InsertBefore(v E, mark *Node[E]) *Node[E] {
	if mark.List != l {
		return nil
	}
	// see comment in List.Remove about initialization of l
	return l.insertValue(v, mark.Prev)
}

// InsertAfter inserts a new element e with value v immediately after mark and returns e.
// If mark is not an element of l, the list is not modified.
// The mark must not be nil.
func (l *List[E]) InsertAfter(v E, mark *Node[E]) *Node[E] {
	if mark.List != l {
		return nil
	}
	// see comment in List.Remove about initialization of l
	return l.insertValue(v, mark)
}

// MoveToFront moves element e to the front of list l.
// If e is not an element of l, the list is not modified.
// The element must not be nil.
func (l *List[E]) MoveToFront(e *Node[E]) {
	if e.List != l || l.root.Next == e {
		return
	}
	// see comment in List.Remove about initialization of l
	l.move(e, &l.root)
}

// MoveToBack moves element e to the back of list l.
// If e is not an element of l, the list is not modified.
// The element must not be nil.
func (l *List[E]) MoveToBack(e *Node[E]) {
	if e.List != l || l.root.Prev == e {
		return
	}
	// see comment in List.Remove about initialization of l
	l.move(e, l.root.Prev)
}

// MoveBefore moves element e to its new position before mark.
// If e or mark is not an element of l, or e == mark, the list is not modified.
// The element and mark must not be nil.
func (l *List[E]) MoveBefore(e, mark *Node[E]) {
	if e.List != l || e == mark || mark.List != l {
		return
	}
	l.move(e, mark.Prev)
}

// MoveAfter moves element e to its new position after mark.
// If e or mark is not an element of l, or e == mark, the list is not modified.
// The element and mark must not be nil.
func (l *List[E]) MoveAfter(e, mark *Node[E]) {
	if e.List != l || e == mark || mark.List != l {
		return
	}
	l.move(e, mark)
}

// PushBackList inserts a copy of another list at the back of list l.
// The lists l and other may be the same. They must not be nil.
func (l *List[E]) PushBackList(other *List[E]) {
	l.lazyInit()
	for i, e := other.Len(), other.Front(); i > 0; i, e = i-1, e.Next {
		l.insertValue(e.Value, l.root.Prev)
	}
}

// PushFrontList inserts a copy of another list at the front of list l.
// The lists l and other may be the same. They must not be nil.
func (l *List[E]) PushFrontList(other *List[E]) {
	l.lazyInit()
	for i, e := other.Len(), other.Back(); i > 0; i, e = i-1, e.Prev {
		l.insertValue(e.Value, &l.root)
	}
}
