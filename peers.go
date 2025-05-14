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

// peers.go 定义了进程如何查找和与对等体通信。

package groupcache

import (
	"context"

	pb "github.com/golang/groupcache/groupcachepb"
)

// Context 是 context.Context 的别名，用于向后兼容。
type Context = context.Context

// ProtoGetter 是必须由对等体实现的接口。
type ProtoGetter interface {
	Get(ctx context.Context, in *pb.GetRequest, out *pb.GetResponse) error
}

// PeerPicker 是必须实现的接口，用于定位
// 拥有特定键的对等体。
type PeerPicker interface {
	// PickPeer 返回拥有特定键的对等体
	// 和 true 表示提名了远程对等体。
	// 如果键所有者是当前对等体，则返回 nil, false。
	PickPeer(key string) (peer ProtoGetter, ok bool)
}

// NoPeers 是 PeerPicker 的一个实现，它永远不会找到对等体。
type NoPeers struct{}

func (NoPeers) PickPeer(key string) (peer ProtoGetter, ok bool) { return }

var (
	portPicker func(groupName string) PeerPicker
)

// RegisterPeerPicker 注册对等体初始化函数。
// 它在创建第一个组时被调用一次。
// RegisterPeerPicker 或 RegisterPerGroupPeerPicker 应该
// 正好被调用一次，但不能同时调用两者。
func RegisterPeerPicker(fn func() PeerPicker) {
	if portPicker != nil {
		panic("RegisterPeerPicker called more than once")
	}
	portPicker = func(_ string) PeerPicker { return fn() }
}

// RegisterPerGroupPeerPicker 注册对等体初始化函数，
// 该函数接受 groupName 参数，用于选择 PeerPicker。
// 它在创建第一个组时被调用一次。
// RegisterPeerPicker 或 RegisterPerGroupPeerPicker 应该
// 正好被调用一次，但不能同时调用两者。
func RegisterPerGroupPeerPicker(fn func(groupName string) PeerPicker) {
	if portPicker != nil {
		panic("RegisterPeerPicker called more than once")
	}
	portPicker = fn
}

func getPeers(groupName string) PeerPicker {
	if portPicker == nil {
		return NoPeers{}
	}
	pk := portPicker(groupName)
	if pk == nil {
		pk = NoPeers{}
	}
	return pk
}
