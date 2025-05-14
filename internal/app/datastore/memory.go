package datastore

import (
	"fmt"
	"log"
	"sync"
)

var (
	db = map[string][]byte{
		"apple":  []byte("red"),
		"banana": []byte("yellow"),
		"orange": []byte("orange"),
		"grape":  []byte("purple"),
		"kiwi":   []byte("green"),
		"cat":    []byte("meow"),
		"dog":    []byte("woof"),
		"bird":   []byte("tweet"),
		"fish":   []byte("blub"),
		"lion":   []byte("roar"),
	}
	dbMu              sync.RWMutex
	cacheFillsCounter int // 计数器，用于记录源自此数据存储的缓存填充次数
)

// InMemoryStore 是一个简单的内存键值存储。
// 它用于模拟后端数据源。
type InMemoryStore struct {
	// nodeAddress 用于日志目的，以标识正在访问哪个节点的数据存储。
	nodeAddress string
}

// NewInMemoryStore 创建一个新的 InMemoryStore。
func NewInMemoryStore(nodeAddress string) *InMemoryStore {
	return &InMemoryStore{nodeAddress: nodeAddress}
}

// Get 通过键从数据存储中检索值。
// 它还记录访问并递增缓存填充的计数器。
func (s *InMemoryStore) Get(key string) ([]byte, error) {
	dbMu.Lock()
	val, ok := db[key]
	cacheFillsCounter++
	currentFills := cacheFillsCounter
	dbMu.Unlock()

	log.Printf("[数据存储获取器] 节点 %s: 被调用获取键: %q。这是此节点的第 %d 次数据库访问。在数据库中找到: %v", s.nodeAddress, key, currentFills, ok)

	if !ok {
		log.Printf("[数据存储获取器] 节点 %s: 数据库中未找到键 %q", s.nodeAddress, key)
		return nil, fmt.Errorf("数据存储中未找到键: %s", key)
	}

	// 返回副本以防止调用者修改原始映射值。
	dataCopy := make([]byte, len(val))
	copy(dataCopy, val)
	return dataCopy, nil
}
