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
	"bytes"
	"errors"
	"io"
	"log"
	"strings"
)

// ByteView 持有字节的不可变视图。
// 内部它包装了一个 []byte 或一个 string，
// 但这个细节对调用者是不可见的。
//
// ByteView 旨在用作值类型，而不是
// 指针（像 time.Time）。
type ByteView struct {
	// 如果 b 非 nil，则使用 b，否则使用 s。
	b []byte
	s string
}

// Len 返回视图的长度。
func (v ByteView) Len() int {
	if v.b != nil {
		return len(v.b)
	}
	return len(v.s)
}

// ByteSlice 返回数据的副本，作为字节切片。
func (v ByteView) ByteSlice() []byte {
	if v.b != nil {
		log.Printf("ByteView: ByteSlice() called, returning copy of b (len %d)", len(v.b))
		return cloneBytes(v.b)
	}
	log.Printf("ByteView: ByteSlice() called, returning new []byte from s (len %d)", len(v.s))
	return []byte(v.s)
}

// String 返回数据作为字符串，如有必要会进行复制。
func (v ByteView) String() string {
	if v.b != nil {
		log.Printf("ByteView: String() called, returning string from b (len %d)", len(v.b))
		return string(v.b)
	}
	log.Printf("ByteView: String() called, returning s (len %d)", len(v.s))
	return v.s
}

// At 返回索引 i 处的字节。
func (v ByteView) At(i int) byte {
	if v.b != nil {
		return v.b[i]
	}
	return v.s[i]
}

// Slice 在提供的 from 和 to 索引之间对视图进行切片。
func (v ByteView) Slice(from, to int) ByteView {
	if v.b != nil {
		return ByteView{b: v.b[from:to]}
	}
	return ByteView{s: v.s[from:to]}
}

// SliceFrom 从提供的索引到末尾对视图进行切片。
func (v ByteView) SliceFrom(from int) ByteView {
	if v.b != nil {
		return ByteView{b: v.b[from:]}
	}
	return ByteView{s: v.s[from:]}
}

// Copy 将 b 复制到 dest 并返回复制的字节数。
func (v ByteView) Copy(dest []byte) int {
	if v.b != nil {
		return copy(dest, v.b)
	}
	return copy(dest, v.s)
}

// Equal 返回 b 中的字节是否与 b2 中的字节相同。
func (v ByteView) Equal(b2 ByteView) bool {
	if b2.b == nil {
		return v.EqualString(b2.s)
	}
	return v.EqualBytes(b2.b)
}

// EqualString 返回 b 中的字节是否与 s 中的字节相同。
func (v ByteView) EqualString(s string) bool {
	if v.b == nil {
		return v.s == s
	}
	l := v.Len()
	if len(s) != l {
		return false
	}
	for i, bi := range v.b {
		if bi != s[i] {
			return false
		}
	}
	return true
}

// EqualBytes 返回 b 中的字节是否与 b2 中的字节相同。
func (v ByteView) EqualBytes(b2 []byte) bool {
	if v.b != nil {
		return bytes.Equal(v.b, b2)
	}
	l := v.Len()
	if len(b2) != l {
		return false
	}
	for i, bi := range b2 {
		if bi != v.s[i] {
			return false
		}
	}
	return true
}

// Reader 为 v 中的字节返回一个 io.ReadSeeker。
func (v ByteView) Reader() io.ReadSeeker {
	if v.b != nil {
		return bytes.NewReader(v.b)
	}
	return strings.NewReader(v.s)
}

// ReadAt 在 v 中的字节上实现 io.ReaderAt。
func (v ByteView) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, errors.New("view: invalid offset")
	}
	if off >= int64(v.Len()) {
		return 0, io.EOF
	}
	n = v.SliceFrom(int(off)).Copy(p)
	if n < len(p) {
		err = io.EOF
	}
	return
}

// WriteTo 在 v 中的字节上实现 io.WriterTo。
func (v ByteView) WriteTo(w io.Writer) (n int64, err error) {
	var m int
	if v.b != nil {
		m, err = w.Write(v.b)
	} else {
		m, err = io.WriteString(w, v.s)
	}
	if err == nil && m < v.Len() {
		err = io.ErrShortWrite
	}
	n = int64(m)
	return
}
