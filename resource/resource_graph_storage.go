package resource

import (
	"iter"

	"go.viam.com/rdk/utils/rtrie"
)

type byAPIBucket map[API]*graphBucket

type graphBucket struct {
	local  *GraphNode
	remote map[string]*GraphNode
}

type resourcesMap struct {
	trie rtrie.Trie[byAPIBucket]
}

func (m resourcesMap) PutByName(name Name, node *GraphNode) *GraphNode {
	byName, _ := m.trie.ComputeIfAbsent(name.Name, func() byAPIBucket {
		return make(byAPIBucket)
	})
	byAPI, ok := byName[name.API]
	if !ok {
		byAPI = &graphBucket{
			remote: make(map[string]*GraphNode),
		}
		byName[name.API] = byAPI
	}
	if name.Remote == "" {
		prev := byAPI.local
		byAPI.local = node
		return prev
	}

	prev := byAPI.remote[name.Remote]
	byAPI.remote[name.Remote] = node
	return prev
}

func (m resourcesMap) GetByName(name Name) (*GraphNode, bool) {
	byName, ok := m.trie.Get(name.Name)
	if !ok {
		return nil, false
	}
	byNameAndAPI, ok := byName[name.API]
	if !ok {
		return nil, false
	}
	if name.Remote == "" {
		return byNameAndAPI.local, byNameAndAPI.local != nil
	}
	result, ok := byNameAndAPI.remote[name.Remote]
	return result, ok
}

func (m resourcesMap) DeleteByName(name Name) {
	byName, ok := m.trie.Get(name.Name)
	if !ok {
		return
	}
	byNameAndAPI, ok := byName[name.API]
	if !ok {
		return
	}
	if name.Remote == "" {
		byNameAndAPI.local = nil
	} else {
		delete(byNameAndAPI.remote, name.Remote)
	}
	if byNameAndAPI.local == nil && len(byNameAndAPI.remote) < 1 {
		delete(byName, name.API)
		if len(byName) < 1 {
			m.trie.Delete(name.Name)
		}
	}
}

func (m resourcesMap) All() iter.Seq2[Name, *GraphNode] {
	return func(yield func(Name, *GraphNode) bool) {
		for name, byAPI := range m.trie.All() {
			for api, bucket := range byAPI {
				if !yield(newRemoteName("", api, name), bucket.local) {
					return
				}
				for remote, node := range bucket.remote {
					if !yield(newRemoteName(remote, api, name), node) {
						return
					}
				}
			}
		}
	}
}

func (m resourcesMap) Keys() iter.Seq[Name] {
	return func(yield func(Name) bool) {
		for key := range m.All() {
			if !yield(key) {
				return
			}
		}
	}
}
func (m resourcesMap) Values() iter.Seq[*GraphNode] {
	return func(yield func(*GraphNode) bool) {
		for _, node := range m.All() {
			if !yield(node) {
				return
			}
		}
	}
}

func (m resourcesMap) Copy() resourcesMap {
	newMap := resourcesMap{
		trie: rtrie.NewTrie[byAPIBucket](),
	}
	return newMap
}
