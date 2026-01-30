package hashing

import (
	"hash/crc32"
	"slices"
	"sort"
	"strconv"
	"sync"
)

type Ring struct {
	Nodes    []uint32
	Registry map[uint32]string
	Replicas int
	mu       sync.RWMutex
}

func hash(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

func (r *Ring) Add(node string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	firstIdentity := node + "#0"
	if _, exists := r.Registry[hash(firstIdentity)]; exists {
		return false
	}

	for i := 0; i < r.Replicas; i++ {
		identity := node + "#" + strconv.Itoa(i)
		hashedIdentity := hash(identity)
		r.Registry[hashedIdentity] = node
		r.Nodes = append(r.Nodes, hashedIdentity)
	}

	slices.Sort(r.Nodes)
	return true
}

func (r *Ring) Get(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.Nodes) == 0 {
		return ""
	}

	userHashed := hash(key)

	idx := sort.Search(len(r.Nodes), func(i int) bool {
		return r.Nodes[i] >= userHashed
	})

	if idx == len(r.Nodes) {
		idx = 0
	}

	return r.Registry[r.Nodes[idx]]
}

func (r *Ring) Remove(node string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.Registry[hash(node+"#0")]; !exists {
		return false
	}

	for i := 0; i < r.Replicas; i++ {
		h := hash(node + "#" + strconv.Itoa(i))
		delete(r.Registry, h)

		for j := 0; j < len(r.Nodes); j++ {
			if r.Nodes[j] == h {
				r.Nodes = append(r.Nodes[:j], r.Nodes[j+1:]...)
				break
			}
		}

	}
	return true
}
