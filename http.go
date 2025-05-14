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

package groupcache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/golang/groupcache/consistenthash"
	pb "github.com/golang/groupcache/groupcachepb"
	"github.com/golang/protobuf/proto"
)

const defaultBasePath = "/_groupcache/"

const defaultReplicas = 50

// HTTPPool 为一组 HTTP 对等体实现 PeerPicker。
type HTTPPool struct {
	// Context 可选地指定服务器在收到请求时使用的上下文。
	// 如果为 nil，服务器使用请求的上下文
	Context func(*http.Request) context.Context

	// Transport 可选地指定客户端在发出请求时
	// 使用的 http.RoundTripper。
	// 如果为 nil，客户端使用 http.DefaultTransport。
	Transport func(context.Context) http.RoundTripper

	// 这个对等体的基本 URL，例如 "https://example.net:8000"
	self string

	// opts 指定选项。
	opts HTTPPoolOptions

	mu          sync.Mutex // 保护 peers 和 httpGetters
	peers       *consistenthash.Map
	httpGetters map[string]*httpGetter // 键例如 "http://10.0.0.2:8008"
}

// HTTPPoolOptions 是 HTTPPool 的配置。
type HTTPPoolOptions struct {
	// BasePath 指定将处理 groupcache 请求的 HTTP 路径。
	// 如果为空，默认为 "/_groupcache/"。
	BasePath string

	// Replicas 指定一致性哈希上的键副本数量。
	// 如果为空，默认为 50。
	Replicas int

	// HashFn 指定一致性哈希的哈希函数。
	// 如果为空，默认为 crc32.ChecksumIEEE。
	HashFn consistenthash.Hash
}

// NewHTTPPool 初始化对等体的 HTTP 池，并将自己注册为 PeerPicker。
// 为方便起见，它还将自己注册为 http.DefaultServeMux 的 http.Handler。
// self 参数应该是指向当前服务器的有效基本 URL，
// 例如 "http://example.net:8000"。
func NewHTTPPool(self string) *HTTPPool {
	p := NewHTTPPoolOpts(self, nil)
	http.Handle(p.opts.BasePath, p)
	return p
}

var httpPoolMade bool

// NewHTTPPoolOpts 使用给定选项初始化对等体的 HTTP 池。
// 与 NewHTTPPool 不同，此函数不将创建的池注册为 HTTP 处理程序。
// 返回的 *HTTPPool 实现 http.Handler，必须使用 http.Handle 注册。
func NewHTTPPoolOpts(self string, o *HTTPPoolOptions) *HTTPPool {
	if httpPoolMade {
		panic("groupcache: NewHTTPPool must be called only once")
	}
	httpPoolMade = true

	p := &HTTPPool{
		self:        self,
		httpGetters: make(map[string]*httpGetter),
	}
	if o != nil {
		p.opts = *o
	}
	if p.opts.BasePath == "" {
		p.opts.BasePath = defaultBasePath
	}
	if p.opts.Replicas == 0 {
		p.opts.Replicas = defaultReplicas
	}
	p.peers = consistenthash.New(p.opts.Replicas, p.opts.HashFn)

	RegisterPeerPicker(func() PeerPicker { return p })
	return p
}

// Set 更新池的对等体列表。
// 每个对等体值应该是有效的基本 URL，
// 例如 "http://example.net:8000"。
func (p *HTTPPool) Set(peers ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.peers = consistenthash.New(p.opts.Replicas, p.opts.HashFn)
	p.peers.Add(peers...)
	p.httpGetters = make(map[string]*httpGetter, len(peers))
	for _, peer := range peers {
		p.httpGetters[peer] = &httpGetter{transport: p.Transport, baseURL: peer + p.opts.BasePath}
	}
}

func (p *HTTPPool) PickPeer(key string) (ProtoGetter, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.peers.IsEmpty() {
		return nil, false
	}
	if peer := p.peers.Get(key); peer != p.self {
		return p.httpGetters[peer], true
	}
	return nil, false
}

func (p *HTTPPool) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 解析请求。
	if !strings.HasPrefix(r.URL.Path, p.opts.BasePath) {
		panic("HTTPPool serving unexpected path: " + r.URL.Path)
	}
	parts := strings.SplitN(r.URL.Path[len(p.opts.BasePath):], "/", 2)
	if len(parts) != 2 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	groupName := parts[0]
	key := parts[1]

	// 获取此组/键的值。
	group := GetGroup(groupName)
	if group == nil {
		http.Error(w, "no such group: "+groupName, http.StatusNotFound)
		return
	}
	var ctx context.Context
	if p.Context != nil {
		ctx = p.Context(r)
	} else {
		ctx = r.Context()
	}

	group.Stats.ServerRequests.Add(1)
	var value []byte
	err := group.Get(ctx, key, AllocatingByteSliceSink(&value))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 将值作为 proto 消息写入响应体。
	body, err := proto.Marshal(&pb.GetResponse{Value: value})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.Write(body)
}

type httpGetter struct {
	transport func(context.Context) http.RoundTripper
	baseURL   string
}

var bufferPool = sync.Pool{
	New: func() interface{} { return new(bytes.Buffer) },
}

func (h *httpGetter) Get(ctx context.Context, in *pb.GetRequest, out *pb.GetResponse) error {
	u := fmt.Sprintf(
		"%v%v/%v",
		h.baseURL,
		url.QueryEscape(in.GetGroup()),
		url.QueryEscape(in.GetKey()),
	)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	tr := http.DefaultTransport
	if h.transport != nil {
		tr = h.transport(ctx)
	}
	res, err := tr.RoundTrip(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned: %v", res.Status)
	}
	b := bufferPool.Get().(*bytes.Buffer)
	b.Reset()
	defer bufferPool.Put(b)
	_, err = io.Copy(b, res.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %v", err)
	}
	err = proto.Unmarshal(b.Bytes(), out)
	if err != nil {
		return fmt.Errorf("decoding response body: %v", err)
	}
	return nil
}
