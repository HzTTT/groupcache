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

package groupcache

import (
	"errors"

	"github.com/golang/protobuf/proto"
)

// Sink 从 Get 调用接收数据。
//
// Getter 的实现必须在成功时调用其中一个 Set 方法。
type Sink interface {
	// SetString 将值设置为 s。
	SetString(s string) error

	// SetBytes 将值设置为 v 的内容。
	// 调用者保留 v 的所有权。
	SetBytes(v []byte) error

	// SetProto 将值设置为 m 的编码版本。
	// 调用者保留 m 的所有权。
	SetProto(m proto.Message) error

	// view 返回用于缓存的字节的冻结视图。
	view() (ByteView, error)
}

func cloneBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func setSinkView(s Sink, v ByteView) error {
	// viewSetter 是一个 Sink，也可以从 ByteView 接收其值。
	// 这是一个快速路径，当项目已经在本地内存中缓存时
	// （在那里它作为 ByteView 缓存）可以最小化副本
	type viewSetter interface {
		setView(v ByteView) error
	}
	if vs, ok := s.(viewSetter); ok {
		return vs.setView(v)
	}
	if v.b != nil {
		return s.SetBytes(v.b)
	}
	return s.SetString(v.s)
}

// StringSink 返回一个填充提供的字符串指针的 Sink。
func StringSink(sp *string) Sink {
	return &stringSink{sp: sp}
}

type stringSink struct {
	sp *string
	v  ByteView
	// TODO(bradfitz): 跟踪是否调用了任何 Sets。
}

func (s *stringSink) view() (ByteView, error) {
	// TODO(bradfitz): 如果没有调用 Set，则返回错误
	return s.v, nil
}

func (s *stringSink) SetString(v string) error {
	s.v.b = nil
	s.v.s = v
	*s.sp = v
	return nil
}

func (s *stringSink) SetBytes(v []byte) error {
	return s.SetString(string(v))
}

func (s *stringSink) SetProto(m proto.Message) error {
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	s.v.b = b
	*s.sp = string(b)
	return nil
}

// ByteViewSink 返回一个填充 ByteView 的 Sink。
func ByteViewSink(dst *ByteView) Sink {
	if dst == nil {
		panic("nil dst")
	}
	return &byteViewSink{dst: dst}
}

type byteViewSink struct {
	dst *ByteView

	// 如果这段代码最终跟踪至少调用了一个 set* 方法，
	// 不要将多次调用 set 方法视为错误。Lorry 的 payload.go
	// 做到了这一点，而且很有道理。本文件顶部关于
	// "恰好调用一个 Set 方法"的注释过于严格。我们
	// 真正关心的是至少一次（在处理程序中），但如果
	// 多个处理程序失败（或程序中使用 Sink 的多个函数），
	// 重用同一个是可以的。
}

func (s *byteViewSink) setView(v ByteView) error {
	*s.dst = v
	return nil
}

func (s *byteViewSink) view() (ByteView, error) {
	return *s.dst, nil
}

func (s *byteViewSink) SetProto(m proto.Message) error {
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	*s.dst = ByteView{b: b}
	return nil
}

func (s *byteViewSink) SetBytes(b []byte) error {
	*s.dst = ByteView{b: cloneBytes(b)}
	return nil
}

func (s *byteViewSink) SetString(v string) error {
	*s.dst = ByteView{s: v}
	return nil
}

// ProtoSink 返回一个 sink，将二进制 proto 值解组到 m 中。
func ProtoSink(m proto.Message) Sink {
	return &protoSink{
		dst: m,
	}
}

type protoSink struct {
	dst proto.Message // 权威值
	typ string

	v ByteView // 已编码
}

func (s *protoSink) view() (ByteView, error) {
	return s.v, nil
}

func (s *protoSink) SetBytes(b []byte) error {
	err := proto.Unmarshal(b, s.dst)
	if err != nil {
		return err
	}
	s.v.b = cloneBytes(b)
	s.v.s = ""
	return nil
}

func (s *protoSink) SetString(v string) error {
	b := []byte(v)
	err := proto.Unmarshal(b, s.dst)
	if err != nil {
		return err
	}
	s.v.b = b
	s.v.s = ""
	return nil
}

func (s *protoSink) SetProto(m proto.Message) error {
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	// TODO(bradfitz): 为同一任务情况优化更多，直接写入？
	// 同时需要记录所有权规则。但然后我们可以在这里
	// 直接分配 *dst = *m。这样现在可以工作：
	err = proto.Unmarshal(b, s.dst)
	if err != nil {
		return err
	}
	s.v.b = b
	s.v.s = ""
	return nil
}

// AllocatingByteSliceSink 返回一个 Sink，它分配
// 一个字节切片来保存接收到的值并将其分配
// 给 *dst。内存不由 groupcache 保留。
func AllocatingByteSliceSink(dst *[]byte) Sink {
	return &allocBytesSink{dst: dst}
}

type allocBytesSink struct {
	dst *[]byte
	v   ByteView
}

func (s *allocBytesSink) view() (ByteView, error) {
	return s.v, nil
}

func (s *allocBytesSink) setView(v ByteView) error {
	if v.b != nil {
		*s.dst = cloneBytes(v.b)
	} else {
		*s.dst = []byte(v.s)
	}
	s.v = v
	return nil
}

func (s *allocBytesSink) SetProto(m proto.Message) error {
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	return s.setBytesOwned(b)
}

func (s *allocBytesSink) SetBytes(b []byte) error {
	return s.setBytesOwned(cloneBytes(b))
}

func (s *allocBytesSink) setBytesOwned(b []byte) error {
	if s.dst == nil {
		return errors.New("nil AllocatingByteSliceSink *[]byte dst")
	}
	*s.dst = cloneBytes(b) // 另一个副本，保护只读的 s.v.b 视图
	s.v.b = b
	s.v.s = ""
	return nil
}

func (s *allocBytesSink) SetString(v string) error {
	if s.dst == nil {
		return errors.New("nil AllocatingByteSliceSink *[]byte dst")
	}
	*s.dst = []byte(v)
	s.v.b = nil
	s.v.s = v
	return nil
}

// TruncatingByteSliceSink 返回一个 Sink，它最多写入 len(*dst)
// 字节到 *dst。如果有更多字节可用，它们会被静默截断。
// 如果可用的字节少于 len(*dst)，*dst 会收缩以适应可用的字节数。
func TruncatingByteSliceSink(dst *[]byte) Sink {
	return &truncBytesSink{dst: dst}
}

type truncBytesSink struct {
	dst *[]byte
	v   ByteView
}

func (s *truncBytesSink) view() (ByteView, error) {
	return s.v, nil
}

func (s *truncBytesSink) SetProto(m proto.Message) error {
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	return s.setBytesOwned(b)
}

func (s *truncBytesSink) SetBytes(b []byte) error {
	return s.setBytesOwned(cloneBytes(b))
}

func (s *truncBytesSink) setBytesOwned(b []byte) error {
	if s.dst == nil {
		return errors.New("nil TruncatingByteSliceSink *[]byte dst")
	}
	n := copy(*s.dst, b)
	if n < len(*s.dst) {
		*s.dst = (*s.dst)[:n]
	}
	s.v.b = b
	s.v.s = ""
	return nil
}

func (s *truncBytesSink) SetString(v string) error {
	if s.dst == nil {
		return errors.New("nil TruncatingByteSliceSink *[]byte dst")
	}
	n := copy(*s.dst, v)
	if n < len(*s.dst) {
		*s.dst = (*s.dst)[:n]
	}
	s.v.b = nil
	s.v.s = v
	return nil
}
