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

// Package groupcache 提供了一个数据加载机制，具有缓存和去重功能，
// 可以在一组对等进程中工作。
//
// 每个数据获取首先查询本地缓存，否则委托给请求键的规范所有者，
// 后者再检查其缓存或最终获取数据。在常见情况下，多个对等体对
// 同一个键的并发缓存未命中只会导致一次缓存填充。
package groupcache

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"

	pb "github.com/golang/groupcache/groupcachepb"
	"github.com/golang/groupcache/lru"
	"github.com/golang/groupcache/singleflight"
)

// Getter 为键加载数据。
type Getter interface {
	// Get 返回由键标识的值，并填充 dest。
	//
	// 返回的数据必须是无版本的。也就是说，键必须
	// 唯一描述加载的数据，而不隐含当前时间，
	// 也不依赖缓存过期机制。
	Get(ctx context.Context, key string, dest Sink) error
}

// GetterFunc 使用函数实现 Getter 接口。
type GetterFunc func(ctx context.Context, key string, dest Sink) error

func (f GetterFunc) Get(ctx context.Context, key string, dest Sink) error {
	return f(ctx, key, dest)
}

var (
	mu     sync.RWMutex
	groups = make(map[string]*Group)

	initPeerServerOnce sync.Once
	initPeerServer     func()
)

// GetGroup 返回之前用 NewGroup 创建的命名组，
// 如果没有这样的组，则返回 nil。
func GetGroup(name string) *Group {
	mu.RLock()
	g := groups[name]
	mu.RUnlock()
	return g
}

// NewGroup 从 Getter 创建一个协调的组感知 Getter。
//
// 返回的 Getter 尝试（但不保证）对整个对等进程集中的给定键
// 一次只运行一个 Get 调用。本地进程和其他进程中的并发调用者
// 一旦原始 Get 完成，就会收到答案的副本。
//
// 组名对每个 getter 必须是唯一的。
func NewGroup(name string, cacheBytes int64, getter Getter) *Group {
	return newGroup(name, cacheBytes, getter, nil)
}

// 如果 peers 为 nil，则通过 sync.Once 调用 peerPicker 来初始化它。
func newGroup(name string, cacheBytes int64, getter Getter, peers PeerPicker) *Group {
	if getter == nil {
		panic("nil Getter")
	}
	mu.Lock()
	defer mu.Unlock()
	initPeerServerOnce.Do(callInitPeerServer)
	if _, dup := groups[name]; dup {
		panic("duplicate registration of group " + name)
	}
	g := &Group{
		name:       name,
		getter:     getter,
		peers:      peers,
		cacheBytes: cacheBytes,
		loadGroup:  &singleflight.Group{},
		mainCache:  cache{cacheName: "main"},
		hotCache:   cache{cacheName: "hot"},
	}
	if fn := newGroupHook; fn != nil {
		fn(g)
	}
	groups[name] = g
	return g
}

// newGroupHook，如果非 nil，会在创建新组后立即被调用。
var newGroupHook func(*Group)

// RegisterNewGroupHook 注册一个在每次创建组时运行的钩子。
func RegisterNewGroupHook(fn func(*Group)) {
	if newGroupHook != nil {
		panic("RegisterNewGroupHook called more than once")
	}
	newGroupHook = fn
}

// RegisterServerStart 注册一个在创建第一个组时运行的钩子。
func RegisterServerStart(fn func()) {
	if initPeerServer != nil {
		panic("RegisterServerStart called more than once")
	}
	initPeerServer = fn
}

func callInitPeerServer() {
	if initPeerServer != nil {
		initPeerServer()
	}
}

// Group 是一个缓存命名空间和相关数据，加载并分布在
// 一组 1 个或多个机器上。
type Group struct {
	name       string
	getter     Getter
	peersOnce  sync.Once
	peers      PeerPicker
	cacheBytes int64 // mainCache 和 hotCache 大小总和的限制

	// mainCache 是那些本进程（在其对等体中）
	// 具有权威性的键的缓存。也就是说，该缓存
	// 包含一致性哈希到该进程的对等编号的键。
	mainCache cache

	// hotCache 包含那些本对等体不具有权威性的键/值
	// （否则它们会在 mainCache 中），但是它们
	// 足够受欢迎，值得在这个进程中镜像，以避免
	// 通过网络从对等体获取。拥有 hotCache 可以避免
	// 网络热点问题，其中对等体的网卡可能成为
	// 热门键的瓶颈。此缓存谨慎使用，以最大化
	// 可全局存储的键/值对的总数。
	hotCache cache

	// loadGroup 确保每个键只被获取一次
	// （无论是本地还是远程），无论并发
	// 调用者的数量如何。
	loadGroup flightGroup

	_ int32 // 强制 Stats 在 32 位平台上按 8 字节对齐

	// Stats 是组的统计信息。
	Stats Stats

	// rand 仅在测试时非 nil，
	// 用于在 TestPeers 中获取可预测的结果。
	rand *rand.Rand
}

// flightGroup 被定义为一个接口，flightgroup.Group
// 满足该接口。我们定义这个接口，以便我们可以用替代
// 实现进行测试。
type flightGroup interface {
	// Done 在 Do 完成时被调用。
	Do(key string, fn func() (interface{}, error)) (interface{}, error)
}

// Stats 是每个组的统计信息。
type Stats struct {
	Gets           AtomicInt `json:"gets"`       // 任何 Get 请求，包括来自对等体的
	CacheHits      AtomicInt `json:"cache_hits"` // 任一缓存命中
	PeerLoads      AtomicInt `json:"peer_loads"` // 远程加载或远程缓存命中（非错误）
	PeerErrors     AtomicInt `json:"peer_errors"`
	Loads          AtomicInt `json:"loads"`           // (gets - cacheHits)
	LoadsDeduped   AtomicInt `json:"loads_deduped"`   // 在 singleflight 后
	LocalLoads     AtomicInt `json:"local_loads"`     // 总成功本地加载
	LocalLoadErrs  AtomicInt `json:"local_load_errs"` // 总失败本地加载
	ServerRequests AtomicInt `json:"server_requests"` // 通过网络从对等体来的 gets
}

// Name 返回组的名称。
func (g *Group) Name() string {
	return g.name
}

func (g *Group) initPeers() {
	if g.peers == nil {
		g.peers = getPeers(g.name)
	}
}

func (g *Group) Get(ctx context.Context, key string, dest Sink) error {
	g.peersOnce.Do(g.initPeers)
	g.Stats.Gets.Add(1)
	log.Printf("[Group %s] 请求键 \"%s\"", g.name, key)
	if dest == nil {
		return errors.New("groupcache: nil dest Sink")
	}
	value, cacheHit := g.lookupCache(key)

	if cacheHit {
		g.Stats.CacheHits.Add(1)
		return setSinkView(dest, value)
	}

	// 优化，避免双重解组或复制：跟踪
	// dest 是否已经被填充。一个调用者
	// （如果是本地的）将设置这个；失败者不会。
	// 常见情况可能是一个调用者。
	destPopulated := false
	value, destPopulated, err := g.load(ctx, key, dest)
	if err != nil {
		return err
	}
	if destPopulated {
		log.Printf("[Group %s] 请求处理完成，通过 dest 返回数据给键 \"%s\"", g.name, key)
		return nil
	}
	log.Printf("[Group %s] 请求处理完成，通过 setSinkView 返回数据给键 \"%s\"", g.name, key)
	return setSinkView(dest, value)
}

// load 通过本地调用 getter 或将其发送到另一台机器来加载键。
func (g *Group) load(ctx context.Context, key string, dest Sink) (value ByteView, destPopulated bool, err error) {
	g.Stats.Loads.Add(1)
	log.Printf(" 远程加载(\"%s\")-请求合并", key)
	viewi, err := g.loadGroup.Do(key, func() (interface{}, error) {
		g.Stats.LoadsDeduped.Add(1)
		var value ByteView
		var err error
		if peer, ok := g.peers.PickPeer(key); ok {
			log.Printf("[Group %s] 责任节点为远程", g.name)
			value, err = g.getFromPeer(ctx, peer, key)
			if err == nil {
				g.Stats.PeerLoads.Add(1)
				return value, nil
			}
			g.Stats.PeerErrors.Add(1)
			log.Printf("[Group %s] 从远程节点获取失败: %v", g.name, err)
		} else {
			log.Printf("[Group %s] 责任节点为本地", g.name)
		}

		log.Printf("调用Getter获取源数据")
		value, err = g.getLocally(ctx, key, dest)
		if err != nil {
			g.Stats.LocalLoadErrs.Add(1)
			log.Printf("Getter获取源数据失败: %v", err)
			return nil, err
		}
		g.Stats.LocalLoads.Add(1)
		destPopulated = true // 只有一个 load 的调用者得到这个返回值
		log.Printf("数据源返回数据，键 \"%s\", 大小: %d bytes", key, value.Len())
		g.populateCache(key, value, &g.mainCache)
		return value, nil
	})
	if err == nil {
		value = viewi.(ByteView)
	}
	return
}

func (g *Group) getLocally(ctx context.Context, key string, dest Sink) (ByteView, error) {
	err := g.getter.Get(ctx, key, dest)
	if err != nil {
		return ByteView{}, err
	}
	return dest.view()
}

func (g *Group) getFromPeer(ctx context.Context, peer ProtoGetter, key string) (ByteView, error) {
	req := &pb.GetRequest{
		Group: &g.name,
		Key:   &key,
	}
	res := &pb.GetResponse{}
	err := peer.Get(ctx, req, res)
	if err != nil {
		return ByteView{}, err
	}
	value := ByteView{b: res.Value}
	// TODO(bradfitz): 使用 res.MinuteQps 或其他智能方式
	// 有条件地填充 hotCache。现在只是在一定
	// 百分比的情况下这样做。
	var pop bool
	if g.rand != nil {
		pop = g.rand.Intn(10) == 0
	} else {
		pop = rand.Intn(10) == 0
	}
	if pop {
		g.populateCache(key, value, &g.hotCache)
	}
	return value, nil
}

func (g *Group) lookupCache(key string) (value ByteView, ok bool) {
	if g.cacheBytes <= 0 {
		return
	}
	value, ok = g.mainCache.get(key)
	if ok {
		log.Printf("[Group %s] 本地缓存命中(\"%s\")", g.name, key)
		return
	}
	value, ok = g.hotCache.get(key)
	if ok {
		log.Printf("[Group %s] 本地热点缓存命中(\"%s\")", g.name, key)
		return
	}
	log.Printf("[Group %s] 本地缓存未命中(\"%s\")", g.name, key)
	return
}

func (g *Group) populateCache(key string, value ByteView, cache *cache) {
	if g.cacheBytes <= 0 {
		return
	}
	cache.add(key, value)
	log.Printf("[Group %s] populateCache(\"%s\", %d bytes) - 填充 %s 缓存", g.name, key, value.Len(), cache.name())

	// 如有必要，从缓存中淘汰项目。
	for {
		mainBytes := g.mainCache.bytes()
		hotBytes := g.hotCache.bytes()
		if mainBytes+hotBytes <= g.cacheBytes {
			return
		}

		// TODO(bradfitz): 这是目前足够好的逻辑。
		// 它应该基于测量和/或考虑不同资源的成本。
		victim := &g.mainCache
		if hotBytes > mainBytes/8 {
			victim = &g.hotCache
		}
		victim.removeOldest()
	}
}

// CacheType 表示缓存的类型。
type CacheType int

const (
	// MainCache 是该对等体作为所有者的项目的缓存。
	MainCache CacheType = iota + 1

	// HotCache 是那些看起来足够受欢迎的项目的缓存，
	// 值得复制到这个节点，即使它不是所有者。
	HotCache
)

// CacheStats 返回组内提供的缓存的统计信息。
func (g *Group) CacheStats(which CacheType) CacheStats {
	switch which {
	case MainCache:
		return g.mainCache.stats()
	case HotCache:
		return g.hotCache.stats()
	default:
		return CacheStats{}
	}
}

// cache 是 *lru.Cache 的包装器，它增加了同步功能，
// 使值始终为 ByteView，并计算所有键和值的大小。
type cache struct {
	mu         sync.RWMutex
	nbytes     int64 // 所有键和值的总大小
	lru        *lru.Cache
	nhit, nget int64
	nevict     int64  // 淘汰次数
	cacheName  string // for logging
}

func (c *cache) name() string {
	if c.cacheName == "" {
		return "unknown"
	}
	return c.cacheName
}

func (c *cache) stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CacheStats{
		Bytes:     c.nbytes,
		Items:     c.itemsLocked(),
		Gets:      c.nget,
		Hits:      c.nhit,
		Evictions: c.nevict,
	}
}

func (c *cache) add(key string, value ByteView) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		c.lru = &lru.Cache{
			OnEvicted: func(key lru.Key, value interface{}) {
				val := value.(ByteView)
				c.nbytes -= int64(len(key.(string))) + int64(val.Len())
				c.nevict++
			},
		}
	}
	c.lru.Add(key, value)
	c.nbytes += int64(len(key)) + int64(value.Len())
}

func (c *cache) get(key string) (value ByteView, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nget++
	if c.lru == nil {
		return
	}
	vi, ok := c.lru.Get(key)
	if !ok {
		return
	}
	c.nhit++
	return vi.(ByteView), true
}

func (c *cache) removeOldest() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru != nil {
		c.lru.RemoveOldest()
	}
}

func (c *cache) bytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nbytes
}

func (c *cache) items() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.itemsLocked()
}

func (c *cache) itemsLocked() int64 {
	if c.lru == nil {
		return 0
	}
	return int64(c.lru.Len())
}

// AtomicInt 是一个要以原子方式访问的 int64。
type AtomicInt int64

// Add 以原子方式将 n 添加到 i。
func (i *AtomicInt) Add(n int64) {
	atomic.AddInt64((*int64)(i), n)
}

// Get 以原子方式获取 i 的值。
func (i *AtomicInt) Get() int64 {
	return atomic.LoadInt64((*int64)(i))
}

func (i *AtomicInt) String() string {
	return strconv.FormatInt(i.Get(), 10)
}

// CacheStats 由 Group 上的 stats 访问器返回。
type CacheStats struct {
	Bytes     int64
	Items     int64
	Gets      int64
	Hits      int64
	Evictions int64
}
