// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import "testing"

func TestNewList(t *testing.T) {
	l := NewList[int]()
	n1 := l.PushFront(42)

	if l.Front().Value != 42 {
		t.Fatalf("Expected 42 but got %d", l.Front().Value)
	}

	n2 := l.PushFront(3)

	if l.Front().Value != 3 {
		t.Fatalf("Expected 3 but got %d", l.Front().Value)
	}

	if l.Back().Value != 42 {
		t.Fatalf("Expected 42 but got %d", l.Back().Value)
	}

	n3 := l.PushBack(10)

	if l.Back().Value != 10 {
		t.Fatalf("Expected 10 but got %d", l.Back().Value)
	}

	l.Remove(n3)

	if l.Back().Value != 42 {
		t.Fatalf("Expected 42 but got %d", l.Back().Value)
	}

	l.Remove(n2)
	if l.Front().Value != 42 {
		t.Fatalf("Expected 42 but got %d", l.Back().Value)
	}

	l.Remove(n1)
	if l.Len() != 0 {
		t.Fatalf("Expected length to be 0, but was %d", l.Len())
	}
}
