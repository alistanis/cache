// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewList(t *testing.T) {

	Assert := assert.New(t)

	l := NewList[int]()

	n := l.Front()
	Assert.Nil(n)

	n1 := l.PushFront(42)

	Assert.EqualValues(42, l.Front().Value)

	n2 := l.PushFront(3)

	Assert.EqualValues(3, l.Front().Value)
	Assert.EqualValues(42, l.Back().Value)

	n3 := l.PushBack(10)

	Assert.EqualValues(10, l.Back().Value)

	l.Remove(n3)

	Assert.EqualValues(42, l.Back().Value)

	l.Remove(n2)
	Assert.EqualValues(42, l.Front().Value)

	l.Remove(n1)
	Assert.EqualValues(0, l.Len())
}

func TestList(t *testing.T) {
	Assert := assert.New(t)

	l := new(List[string])

	Assert.NotPanics(func() {
		l.PushBack("test")
	})

	n := l.Front()

	Assert.EqualValues("test", n.Value)
	n = l.Back()

	Assert.EqualValues("test", n.Value)

	l.MoveBefore(n, n)

	n = l.Front()

	Assert.EqualValues("test", n.Value)
	n = l.Back()

	Assert.EqualValues("test", n.Value)

	l.move(n, n)

	l.InsertBefore("42", n)

	n = l.Front()
	Assert.EqualValues("42", n.Value)

	l.Remove(l.Back())

	l.InsertAfter("3.14", n)

	n = l.Back()
	Assert.EqualValues("3.14", n.Value)

	f := l.Front()
	b := l.Back()
	l.MoveAfter(l.Front(), l.Back())
	Assert.EqualValues(f, l.Back())
	Assert.EqualValues(b, l.Front())

	l.MoveToBack(l.Front())
	Assert.EqualValues(f, l.Front())

	l2 := NewList[string]()

	l2.PushBack("Hello")
	l2.PushBack("Goodbye")

	l.PushBackList(l2)

	Assert.EqualValues(4, l.Len())
	Assert.EqualValues("42", l.Front().Value)
	Assert.EqualValues("Goodbye", l.Back().Value)

	l3 := NewList[string]()

	l3.PushBack("Test")
	l.PushFrontList(l3)

	Assert.EqualValues(5, l.Len())
	Assert.EqualValues("Test", l.Front().Value)
	Assert.EqualValues("Goodbye", l.Back().Value)
}

func TestList_NonOwnedNodes(t *testing.T) {
	Assert := assert.New(t)

	l := NewList[int]()

	n := l.PushFront(42)

	l2 := NewList[int]()

	Assert.Nil(l2.InsertBefore(1, n))
	Assert.Nil(l2.InsertAfter(1, n))

	Assert.NotPanics(func() { l.MoveToBack(n) })
	Assert.NotPanics(func() { l.MoveAfter(n, n) })

	n2 := l.PushBack(1)

	l.MoveBefore(n2, n)
	Assert.EqualValues(1, l.Front().Value)
}
