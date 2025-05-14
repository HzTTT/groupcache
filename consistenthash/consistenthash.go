/*
Copyright 2013 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package consistenthash 提供了一个环形哈希的实现。
package consistenthash

import (
	"hash/crc32"
	"log"
	"sort"
	"strconv"
)

type Hash func(data []byte) uint32

type Map struct {
	hash     Hash
	replicas int
	keys     []int // 已排序
	hashMap  map[int]string
}

func New(replicas int, fn Hash) *Map {
	m := &Map{
		replicas: replicas,
		hash:     fn,
		hashMap:  make(map[int]string),
	}
	if m.hash == nil {
		m.hash = crc32.ChecksumIEEE
	}
	return m
}

// IsEmpty 如果没有可用项，则返回 true。
func (m *Map) IsEmpty() bool {
	return len(m.keys) == 0
}

// Add 向哈希中添加一些键。
func (m *Map) Add(keys ...string) {
	if len(keys) == 0 {
		return
	}
	//log.Printf("ConsistentHash: 开始添加节点: %v", keys)
	addedHashes := 0
	for _, key := range keys {
		for i := 0; i < m.replicas; i++ {
			hash := int(m.hash([]byte(strconv.Itoa(i) + key)))
			m.keys = append(m.keys, hash)
			m.hashMap[hash] = key
			// 避免过多日志，可以考虑只在 DEBUG 级别记录每个哈希，或只记录总数
			// log.Printf("ConsistentHash: 添加虚拟节点 %s (replica %d), hash %d", key, i, hash)
			addedHashes++
		}
	}
	sort.Ints(m.keys)
	log.Printf("ConsistentHash: 添加完成, 共生成 %d 个虚拟节点并排序", addedHashes)
}

// Get 获取哈希中与提供的键最接近的项。
func (m *Map) Get(key string) string {
	if m.IsEmpty() {
		log.Printf("ConsistentHash: Get(\"%s\") - 哈希环为空", key)
		return ""
	}

	hash := int(m.hash([]byte(key)))
	log.Printf("ConsistentHash: Get(\"%s\") - 计算哈希值: %d", key, hash)

	// 对适当的副本进行二分查找。
	idx := sort.Search(len(m.keys), func(i int) bool { return m.keys[i] >= hash })

	// 表示我们已循环回到第一个副本。
	if idx == len(m.keys) {
		idx = 0
	}

	node := m.hashMap[m.keys[idx]]
	log.Printf("ConsistentHash: Get(\"%s\") - 找到节点: %s (通过虚拟节点哈希 %d)", key, node, m.keys[idx])
	return node
}
