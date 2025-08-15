package rtrie

import (
	"iter"
)

type trieNode[T any] struct {
	children    map[byte]*trieNode[T]
	hasContents bool
	contents    T
}

type Trie[T any] struct {
	root trieNode[T]
}

func NewTrie[T any]() Trie[T] {
	return Trie[T]{
		root: trieNode[T]{
			children: make(map[byte]*trieNode[T]),
		},
	}
}

func (t *Trie[T]) findNode(key string, create bool) *trieNode[T] {
	if len(key) < 1 {
		return nil
	}

	curr := &t.root
	for i := len(key) - 1; i >= 0; i-- {
		char := key[i]
		child := curr.children[char]
		if child == nil {
			if !create {
				return nil
			}
			child = &trieNode[T]{
				children: make(map[byte]*trieNode[T]),
			}
			curr.children[char] = child
		}
		curr = child
	}

	return curr
}

func (t *Trie[T]) ComputeIfAbsent(key string, compute func() T) (T, bool) {
	existing, ok := t.Get(key)
	if ok {
		return existing, true
	}
	newVal := compute()
	t.Set(key, newVal)
	return newVal, false
}

func (t *Trie[T]) Get(key string) (T, bool) {
	node := t.findNode(key, false)
	if node == nil {
		var zero T
		return zero, false
	}
	return node.contents, node.hasContents
}

func (t *Trie[T]) Set(key string, val T) (T, bool) {
	node := t.findNode(key, true)
	prevVal, hadPrevVal := node.contents, node.hasContents
	node.contents, node.hasContents = val, true
	return prevVal, hadPrevVal
}

func (t *Trie[T]) Delete(key string) (T, bool) {
	var zero T
	node := t.findNode(key, false)
	if node == nil {
		return zero, false
	}
	prevVal, hadPrevVal := node.contents, node.hasContents
	node.contents, node.hasContents = zero, false
	return prevVal, hadPrevVal
}

// FindSuffix returns a list of all values where the key is a suffix of
// `query`. For example, a query of `abc` would return the values for `abc`,
// `bc` and `c`.
func (t *Trie[T]) FindSuffix(query string) []T {
	var result []T
	node := &t.root
	for i := len(query) - 1; i >= 0; i-- {
		node = node.children[query[i]]
		if node == nil {
			break
		}
		if node.hasContents {
			result = append(result, node.contents)
		}
	}

	return result
}

func walk[T any](node *trieNode[T], key string, yield func(string, T) bool) {
	if node.hasContents {
		if !yield(key, node.contents) {
			return
		}
	}

	for prefix, child := range node.children {
		walk(child, string(prefix)+key, yield)
	}
}

func (t *Trie[T]) All() iter.Seq2[string, T] {
	return func(yield func(string, T) bool) {
		walk(&t.root, "", yield)
	}
}
