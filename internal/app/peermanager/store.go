package peermanager

import (
	"log"
	"sort"
	"sync"
	"time"

	"github.com/golang/groupcache"
)

// 节点管理相关常量 - 之后可以做成可配置
const (
	DefaultPeerTimeoutDuration = 15 * time.Second
	// heartbeatInterval 和 announceInterval 由 service 层管理
)

// PeerEntry 用于存储已知节点的信息
// GroupcacheAddress 例如：http://localhost:8081
// ApiAddress 例如：http://localhost:9081（用于管理/API 通信）
// LastSeen 记录最后一次看到该节点的时间
type PeerEntry struct {
	GroupcacheAddress string // e.g., http://localhost:8081
	ApiAddress        string // e.g., http://localhost:9081 (for admin/API communication)
	LastSeen          time.Time
}

// PeerStore 管理已知节点列表并更新 groupcache 的 HTTPPool。
// 它是节点发现和健康检查机制的核心。
type PeerStore struct {
	mu                     sync.RWMutex
	peers                  map[string]PeerEntry // Key: GroupcacheAddress of the peer
	selfApiAddr            string
	selfGroupcacheAddr     string
	initialPeerApiAddrs    []string             // API addresses of initial contact points from config
	groupcachePool         *groupcache.HTTPPool // The groupcache pool to update
	lastSetGroupcachePeers []string             // To avoid unnecessary Set() calls to groupcachePool
	peerTimeoutDuration    time.Duration        // How long before a peer is considered dead
}

// NewPeerStore 创建并初始化一个 PeerStore。
func NewPeerStore(
	selfApiAddr string,
	selfGroupcacheAddr string,
	initialPeerApiAddrs []string,
	pool *groupcache.HTTPPool,
	peerTimeout time.Duration,
) *PeerStore {
	if peerTimeout == 0 {
		peerTimeout = DefaultPeerTimeoutDuration
	}
	ps := &PeerStore{
		peers:                  make(map[string]PeerEntry),
		selfApiAddr:            selfApiAddr,
		selfGroupcacheAddr:     selfGroupcacheAddr,
		initialPeerApiAddrs:    initialPeerApiAddrs,
		groupcachePool:         pool,
		lastSetGroupcachePeers: []string{},
		peerTimeoutDuration:    peerTimeout,
	}
	// 将自身加入 map，主要用于一致性信息查询。
	// 自身不会被加入 groupcachePool 的节点列表。
	ps.peers[selfGroupcacheAddr] = PeerEntry{
		GroupcacheAddress: selfGroupcacheAddr,
		ApiAddress:        selfApiAddr,
		LastSeen:          time.Now(), // 标记自身为最近可见
	}
	log.Printf("[%s PeerStore] 初始化完成。自身: %s (API: %s)。超时时间: %v", selfGroupcacheAddr, selfGroupcacheAddr, selfApiAddr, peerTimeout)
	return ps
}

// AddOrUpdatePeer 添加新节点或更新已存在节点的 LastSeen 时间。
// 如果是新节点或 API 地址发生变化则返回 true。
func (ps *PeerStore) AddOrUpdatePeer(groupcacheAddr, apiAddr string, lastSeenTime time.Time) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	existingEntry, exists := ps.peers[groupcacheAddr]
	ps.peers[groupcacheAddr] = PeerEntry{
		GroupcacheAddress: groupcacheAddr,
		ApiAddress:        apiAddr,
		LastSeen:          lastSeenTime,
	}

	if !exists {
		log.Printf("[%s PeerStore] 发现新节点: %s (API: %s)", ps.selfGroupcacheAddr, groupcacheAddr, apiAddr)
		return true
	}
	if existingEntry.ApiAddress != apiAddr {
		log.Printf("[%s PeerStore] 节点 %s 的 API 地址发生变化: 旧 %s, 新 %s", ps.selfGroupcacheAddr, groupcacheAddr, existingEntry.ApiAddress, apiAddr)
		return true // Consider API address change as a notable update
	}
	// log.Printf("[%s PeerStore] Updated lastSeen for peer: %s", ps.selfGroupcacheAddr, groupcacheAddr) // Too verbose for heartbeats
	return false
}

// GetLivePeerGroupcacheAddrsAndPrune 剔除失效节点并返回存活节点的 groupcache 地址（已排序）。
func (ps *PeerStore) GetLivePeerGroupcacheAddrsAndPrune() []string {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	var livePeers []string
	updatedInternalPeersMap := make(map[string]PeerEntry)
	removedCount := 0

	for addr, entry := range ps.peers {
		// Always keep self in the internal map, but don't add to groupcache peers list for groupcache itself.
		if addr == ps.selfGroupcacheAddr {
			updatedInternalPeersMap[addr] = entry
			continue
		}

		if time.Since(entry.LastSeen) < ps.peerTimeoutDuration {
			livePeers = append(livePeers, addr)
			updatedInternalPeersMap[addr] = entry
		} else {
			log.Printf("[%s PeerStore] 剔除失效节点: %s (API: %s, 最后活跃: %v)", ps.selfGroupcacheAddr, addr, entry.ApiAddress, entry.LastSeen)
			removedCount++
		}
	}
	ps.peers = updatedInternalPeersMap
	if removedCount > 0 {
		log.Printf("[%s PeerStore] 已剔除 %d 个失效节点。当前已知节点（含自身）: %d", ps.selfGroupcacheAddr, removedCount, len(ps.peers))
	}

	sort.Strings(livePeers) // Sort for consistent comparison and Set calls
	return livePeers
}

// UpdateGroupcachePoolIfNeeded 如果 groupcache 节点列表（不含自身）发生变化，则更新 groupcache HTTPPool。
func (ps *PeerStore) UpdateGroupcachePoolIfNeeded() (changed bool) {
	liveGroupcacheAddrs := ps.GetLivePeerGroupcacheAddrsAndPrune()

	ps.mu.RLock()
	isDifferent := !equalSorted(liveGroupcacheAddrs, ps.lastSetGroupcachePeers)
	ps.mu.RUnlock()

	if isDifferent {
		log.Printf("[%s PeerStore] groupcache 活跃节点列表发生变化，正在更新 groupcache pool。旧: %v, 新: %v", ps.selfGroupcacheAddr, ps.lastSetGroupcachePeers, liveGroupcacheAddrs)
		ps.groupcachePool.Set(liveGroupcacheAddrs...) // This is the crucial call to update groupcache

		ps.mu.Lock()
		ps.lastSetGroupcachePeers = make([]string, len(liveGroupcacheAddrs))
		copy(ps.lastSetGroupcachePeers, liveGroupcacheAddrs)
		ps.mu.Unlock()
		return true
	}
	// log.Printf("[%s PeerStore] Active peer list for groupcache unchanged. No update needed. Current: %v", ps.selfGroupcacheAddr, liveGroupcacheAddrs)
	return false
}

// GetPeerApiAddress 根据 groupcache 地址获取对应的 API 地址。
func (ps *PeerStore) GetPeerApiAddress(groupcacheAddr string) (string, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	entry, ok := ps.peers[groupcacheAddr]
	if !ok {
		return "", false
	}
	return entry.ApiAddress, true
}

// GetAllKnownPeers 返回所有已知节点（含自身）的副本，仅供信息展示。
func (ps *PeerStore) GetAllKnownPeers() map[string]PeerEntry {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	copiedPeers := make(map[string]PeerEntry, len(ps.peers))
	for k, v := range ps.peers {
		copiedPeers[k] = v
	}
	return copiedPeers
}

// GetInitialPeerApiAddrs 返回初始节点 API 地址列表。
func (ps *PeerStore) GetInitialPeerApiAddrs() []string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	addrs := make([]string, len(ps.initialPeerApiAddrs))
	copy(addrs, ps.initialPeerApiAddrs)
	return addrs
}

// GetSelfApiAddr 返回当前节点的 API 地址。
func (ps *PeerStore) GetSelfApiAddr() string {
	return ps.selfApiAddr
}

// GetSelfGroupcacheAddr 返回当前节点的 groupcache 地址。
func (ps *PeerStore) GetSelfGroupcacheAddr() string {
	return ps.selfGroupcacheAddr
}

// equalSorted 判断两个已排序字符串切片是否相等。
// 该工具函数如有需要可放到 util 包。
func equalSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
