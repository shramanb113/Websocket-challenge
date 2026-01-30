package hashing

import (
	"hash/crc32"
	"slices"
	"sort"
	"strconv"
	"sync"
)

type Ring struct {
	nodes    []uint32
	registry map[uint32]string
	replicas int
	mu       sync.RWMutex
}

func hash(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

func (r *Ring) Add(node string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.replicas {
		identity := node + "#" + strconv.Itoa(i)
		hashedIdentity := hash(identity)
		if _, ok := r.registry[hashedIdentity]; !ok {
			r.registry[hashedIdentity] = node
			r.nodes = append(r.nodes, hashedIdentity)
		}
	}
	slices.Sort(r.nodes)
}

func (r *Ring) Get(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.nodes) == 0 {
		return ""
	}

	userHashed := hash(key)

	idx := sort.Search(len(r.nodes), func(i int) bool {
		return r.nodes[i] >= userHashed
	})

	if idx == len(r.nodes) {
		idx = 0
	}

	return r.registry[r.nodes[idx]]
}
