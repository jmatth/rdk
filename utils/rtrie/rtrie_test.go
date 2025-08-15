package rtrie_test

import (
	"testing"

	"go.viam.com/test"

	"go.viam.com/rdk/utils/rtrie"
)

func TestSetAndGet(t *testing.T) {
	trie := rtrie.NewTrie[int]()

	ret, exists := trie.Get("a")
	test.That(t, ret, test.ShouldBeZeroValue)
	test.That(t, exists, test.ShouldBeFalse)

	trie.Set("a", 1)
	trie.Set("b", 2)
	trie.Set("ab", 12)

	ret, exists = trie.Get("a")
	test.That(t, ret, test.ShouldEqual, 1)
	test.That(t, exists, test.ShouldBeTrue)
	ret, exists = trie.Get("b")
	test.That(t, ret, test.ShouldEqual, 2)
	test.That(t, exists, test.ShouldBeTrue)
	ret, exists = trie.Get("ab")
	test.That(t, ret, test.ShouldEqual, 12)
	test.That(t, exists, test.ShouldBeTrue)
}

func TestDelete(t *testing.T) {
	trie := rtrie.NewTrie[int]()

	trie.Set("b", 2)
	trie.Set("ab", 12)

	ret, exists := trie.Get("b")
	test.That(t, ret, test.ShouldEqual, 2)
	test.That(t, exists, test.ShouldBeTrue)
	ret, exists = trie.Get("ab")
	test.That(t, ret, test.ShouldEqual, 12)
	test.That(t, exists, test.ShouldBeTrue)

	ret, exists = trie.Delete("b")
	test.That(t, ret, test.ShouldEqual, 2)
	test.That(t, exists, test.ShouldBeTrue)
	ret, exists = trie.Get("b")
	test.That(t, ret, test.ShouldBeZeroValue)
	test.That(t, exists, test.ShouldBeFalse)
	ret, exists = trie.Get("ab")
	test.That(t, ret, test.ShouldEqual, 12)
	test.That(t, exists, test.ShouldBeTrue)

	ret, exists = trie.Delete("ab")
	test.That(t, ret, test.ShouldEqual, 12)
	test.That(t, exists, test.ShouldBeTrue)
	ret, exists = trie.Get("ab")
	test.That(t, ret, test.ShouldBeZeroValue)
	test.That(t, exists, test.ShouldBeFalse)
	ret, exists = trie.Get("b")
	test.That(t, ret, test.ShouldBeZeroValue)
	test.That(t, exists, test.ShouldBeFalse)
}

func TestFindSuffix(t *testing.T) {
	void := struct{}{}
	trie := rtrie.NewTrie[struct{}]()
	trie.Set("abc", void)
	trie.Set("bc", void)
	trie.Set("c", void)
	trie.Set("ab", void)
	trie.Set("a", void)

	test.That(t, trie.FindSuffix("c"), test.ShouldHaveLength, 1)
	test.That(t, trie.FindSuffix("bc"), test.ShouldHaveLength, 2)
	test.That(t, trie.FindSuffix("abc"), test.ShouldHaveLength, 3)
}
