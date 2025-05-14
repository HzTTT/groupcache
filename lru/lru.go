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

// Package lru 实现了一个 LRU 缓存。
package lru

import (
	"container/list"
	"log"
)

// Cache 是一个 LRU 缓存。它不是并发安全的。
type Cache struct {
	// MaxEntries 是在项目被淘汰前的最大缓存条目数。
	// 零表示没有限制。
	MaxEntries int

	// OnEvicted 可选地指定一个回调函数，在条目
	// 从缓存中清除时执行。
	OnEvicted func(key Key, value interface{})

	ll    *list.List
	cache map[interface{}]*list.Element
}

// Key 可以是任何可比较的值。参见 http://golang.org/ref/spec#Comparison_operators
type Key interface{}

type entry struct {
	key   Key
	value interface{}
}

// New 创建一个新的 Cache。
// 如果 maxEntries 为零，则缓存没有限制，假定
// 淘汰由调用者完成。
func New(maxEntries int) *Cache {
	c := &Cache{
		MaxEntries: maxEntries,
		ll:         list.New(),
		cache:      make(map[interface{}]*list.Element),
	}
	log.Printf("LRU: 新建缓存, MaxEntries: %d", maxEntries)
	return c
}

// Add 向缓存添加一个值。
func (c *Cache) Add(key Key, value interface{}) {
	if c.cache == nil {
		c.cache = make(map[interface{}]*list.Element)
		c.ll = list.New()
		log.Printf("LRU: Add - 缓存未初始化, 重新初始化")
	}
	if ee, ok := c.cache[key]; ok {
		c.ll.MoveToFront(ee)
		ee.Value.(*entry).value = value
		log.Printf("LRU: Add - 更新键 '%v'", key)
		return
	}
	ele := c.ll.PushFront(&entry{key, value})
	c.cache[key] = ele
	log.Printf("LRU: Add - 添加新键 '%v'", key)
	if c.MaxEntries != 0 && c.ll.Len() > c.MaxEntries {
		log.Printf("LRU: Add - 缓存已满 (Len: %d, Max: %d), 淘汰最旧元素", c.ll.Len(), c.MaxEntries)
		c.RemoveOldest()
	}
}

// Get 从缓存中查找键的值。
func (c *Cache) Get(key Key) (value interface{}, ok bool) {
	if c.cache == nil {
		log.Printf("LRU: Get - 缓存未初始化, 键 '%v' 未找到", key)
		return
	}
	if ele, hit := c.cache[key]; hit {
		c.ll.MoveToFront(ele)
		log.Printf("LRU: Get - 键 '%v' 命中", key)
		return ele.Value.(*entry).value, true
	}
	log.Printf("LRU: Get - 键 '%v' 未命中", key)
	return
}

// Remove 从缓存中移除提供的键。
func (c *Cache) Remove(key Key) {
	if c.cache == nil {
		log.Printf("LRU: Remove - 缓存未初始化, 无法移除键 '%v'", key)
		return
	}
	if ele, hit := c.cache[key]; hit {
		log.Printf("LRU: Remove - 开始移除键 '%v'", key)
		c.removeElement(ele)
	} else {
		log.Printf("LRU: Remove - 键 '%v' 未在缓存中找到, 无需移除", key)
	}
}

// RemoveOldest 从缓存中移除最旧的项。
func (c *Cache) RemoveOldest() {
	if c.cache == nil {
		log.Printf("LRU: RemoveOldest - 缓存未初始化, 无法淘汰")
		return
	}
	ele := c.ll.Back()
	if ele != nil {
		log.Printf("LRU: RemoveOldest - 开始淘汰最旧元素")
		c.removeElement(ele)
	} else {
		log.Printf("LRU: RemoveOldest - 缓存为空, 无元素可淘汰")
	}
}

func (c *Cache) removeElement(e *list.Element) {
	c.ll.Remove(e)
	kv := e.Value.(*entry)
	delete(c.cache, kv.key)
	log.Printf("LRU: removeElement - 已移除键 '%v'", kv.key)
	if c.OnEvicted != nil {
		log.Printf("LRU: removeElement - 调用 OnEvicted 回调函数处理键 '%v'", kv.key)
		c.OnEvicted(kv.key, kv.value)
	}
}

// Len 返回缓存中的项目数。
func (c *Cache) Len() int {
	if c.cache == nil {
		log.Printf("LRU: Len - 缓存未初始化, 大小为 0")
		return 0
	}
	length := c.ll.Len()
	// log.Printf("LRU: Len - 当前缓存大小: %d", length) // Get 操作会频繁调用，此日志可能过多
	return length
}

// Clear 清除缓存中所有存储的项目。
func (c *Cache) Clear() {
	log.Printf("LRU: Clear - 开始清空缓存")
	if c.OnEvicted != nil && c.cache != nil {
		log.Printf("LRU: Clear - 缓存中有 %d 个元素, 将对每个元素调用 OnEvicted", len(c.cache))
		for _, e := range c.cache {
			kv := e.Value.(*entry)
			c.OnEvicted(kv.key, kv.value)
		}
	}
	c.ll = nil
	c.cache = nil
	log.Printf("LRU: Clear - 缓存已清空")
}
