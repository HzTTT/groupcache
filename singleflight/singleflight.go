/*
Copyright 2012 Google Inc.

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

// Package singleflight 提供了一个重复函数调用抑制机制。
package singleflight

import (
	"log"
	"sync"
)

// call 是一个正在进行中或已完成的 Do 调用
type call struct {
	wg  sync.WaitGroup
	val interface{}
	err error
}

// Group 表示一类工作，形成一个命名空间，在其中
// 可以执行具有重复抑制的工作单元。
type Group struct {
	mu sync.Mutex       // 保护 m
	m  map[string]*call // 延迟初始化
}

// Do 执行并返回给定函数的结果，确保
// 对于给定的键，一次只有一个执行在进行中。
// 如果有重复到来，重复的调用者等待
// 原始调用完成并接收相同的结果。
func (g *Group) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		log.Printf("Singleflight: 重复请求键 \"%s\", 等待原始请求完成", key)
		c.wg.Wait()
		log.Printf("Singleflight: 键 \"%s\" 的原始请求完成, 返回结果", key)
		return c.val, c.err
	}
	c := new(call)
	c.wg.Add(1)
	g.m[key] = c
	//log.Printf("Singleflight: 新请求键 \"%s\", 执行函数", key)
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()
	//log.Printf("Singleflight: 键 \"%s\" 的函数执行完成", key)

	g.mu.Lock()
	delete(g.m, key)
	//log.Printf("Singleflight: 删除键 \"%s\" 从进行中请求 map", key)
	g.mu.Unlock()

	return c.val, c.err
}
