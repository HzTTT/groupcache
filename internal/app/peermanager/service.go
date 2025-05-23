package peermanager

import (
	"log"
	"sync"
	"time"
	// "yourmodule/internal/app/config" // 如果直接需要配置值，可以使用此导入
)

const (
	DefaultHeartbeatInterval = 5 * time.Second
	DefaultAnnounceInterval  = 30 * time.Second
	// peerTimeoutDuration 主要由 PeerStore 管理，但服务可能需要感知
	// DefaultPeerPruneCheckInterval = 10 * time.Second // 服务明确触发剪枝检查的频率
)

// PeerService 管理节点发现、心跳和剔除的生命周期。
// 它会启动后台 goroutine 执行这些任务。
type PeerService struct {
	peerStore         *PeerStore // 依赖 PeerStore
	heartbeatInterval time.Duration
	announceInterval  time.Duration
	// 传出请求的 httpClientTimeout 由 client.go 中的 sendPostRequest 处理

	stopSignal              chan struct{}   // 用于优雅地停止服务 goroutine
	wg                      sync.WaitGroup  // 用于等待 goroutine 完成
	nodeSelfAnnouncePayload AnnouncePayload // 预计算的自身负载
}

// NewPeerService 创建一个新的 PeerService。
func NewPeerService(ps *PeerStore, heartBeat time.Duration, announce time.Duration) *PeerService {
	if heartBeat == 0 {
		heartBeat = DefaultHeartbeatInterval
	}
	if announce == 0 {
		announce = DefaultAnnounceInterval
	}
	return &PeerService{
		peerStore:         ps,
		heartbeatInterval: heartBeat,
		announceInterval:  announce,
		stopSignal:        make(chan struct{}),
		nodeSelfAnnouncePayload: AnnouncePayload{
			GroupcacheAddress: ps.GetSelfGroupcacheAddr(),
			ApiAddress:        ps.GetSelfApiAddr(),
		},
	}
}

// Start 启动 peer 管理相关的后台 goroutine。
func (s *PeerService) Start() {
	s.wg.Add(3) // 用于 announcer、heartbeater 和 pruner/updater
	go s.announcer()
	go s.heartbeater()
	go s.periodicUpdater()
	log.Printf("[PeerService] 节点信息交换间隔: %v, 心跳检测间隔: %v", s.announceInterval, s.heartbeatInterval)
}

// Stop 通知后台 goroutine 终止并等待其结束。
func (s *PeerService) Stop() {
	log.Printf("[%s PeerService] 正在停止...", s.peerStore.GetSelfGroupcacheAddr())
	close(s.stopSignal)
	s.wg.Wait()
	log.Printf("[%s PeerService] 已停止。", s.peerStore.GetSelfGroupcacheAddr())
}

// announcer 定期向初始节点广播自身信息并处理响应。
func (s *PeerService) announcer() {
	defer s.wg.Done()
	//log.Printf("[%s PeerService Announcer] 启动...", s.peerStore.GetSelfGroupcacheAddr())

	ticker := time.NewTicker(s.announceInterval)
	defer ticker.Stop()

	log.Printf("[PeerService Announcer] 初始节点: %v", s.peerStore.GetInitialPeerApiAddrs())
	announcedToInitialOnce := make(map[string]bool) // 跟踪我们是否至少成功向初始对等点广播一次

	for {
		select {
		case <-s.stopSignal:
			log.Printf("[%s PeerService Announcer] 正在关闭。", s.peerStore.GetSelfGroupcacheAddr())
			return
		case <-ticker.C:
			initialPeerApiAddrs := s.peerStore.GetInitialPeerApiAddrs()
			if len(initialPeerApiAddrs) == 0 {
				// log.Printf("[%s PeerService Announcer] 没有初始节点可广播。", s.peerStore.GetSelfGroupcacheAddr())
				continue
			}

			knownPeerCount := 0

			func() { // 匿名函数范围 RLock
				s.peerStore.mu.RLock()
				defer s.peerStore.mu.RUnlock()
				for k := range s.peerStore.peers {
					if k != s.peerStore.selfGroupcacheAddr {
						knownPeerCount++
					}
				}
			}()

			for _, initialPeerAPIAddr := range initialPeerApiAddrs {
				if initialPeerAPIAddr == s.peerStore.GetSelfApiAddr() { // 不向自己广播
					continue
				}

				// 如果我们尚未成功向此初始节点广播，或者节点计数为零，则重新广播。
				if !announcedToInitialOnce[initialPeerAPIAddr] || knownPeerCount == 0 {
					targetURL := initialPeerAPIAddr + "/admin/announce_self" // 假设 Announce 在 admin 路径上
					var resp AnnounceResponse
					err := sendPostRequest(targetURL, s.nodeSelfAnnouncePayload, &resp, 0) // 使用 client.go 的 sendPostRequest

					if err != nil {
						log.Printf("[PeerService Announcer] 广播到 %s 出错: %v", targetURL, err)
						// 如果错误，不标记为已广播，下一个周期将重试
						continue
					}
					announcedToInitialOnce[initialPeerAPIAddr] = true
					log.Printf("[PeerService Announcer] 成功广播到 %s。响应中包含 %d 个已知节点。", targetURL, len(resp.KnownPeers))

					var changedByAnnounce bool
					for _, discoveredPeer := range resp.KnownPeers {
						if discoveredPeer.GroupcacheAddress != s.peerStore.GetSelfGroupcacheAddr() { // 不从广播响应中添加自身
							if s.peerStore.AddOrUpdatePeer(discoveredPeer.GroupcacheAddress, discoveredPeer.ApiAddress, time.Now()) {
								changedByAnnounce = true
							}
						}
					}
					if changedByAnnounce {
						s.peerStore.UpdateGroupcachePoolIfNeeded()
					}
				}
			}
		}
	}
}

// heartbeater 定期向所有已知节点发送心跳。
func (s *PeerService) heartbeater() {
	defer s.wg.Done()
	//log.Printf("[%s PeerService Heartbeater] 启动...", s.peerStore.GetSelfGroupcacheAddr())
	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopSignal:
			log.Printf("[%s PeerService Heartbeater] 正在关闭。", s.peerStore.GetSelfGroupcacheAddr())
			return
		case <-ticker.C:
			var targets []PeerEntry
			// 获取要发送心跳的节点快照
			// 这避免了在发送 HTTP 请求时持有锁
			func() { // 匿名函数范围 RLock
				s.peerStore.mu.RLock()
				defer s.peerStore.mu.RUnlock()
				for gcAddr, entry := range s.peerStore.peers {
					if gcAddr != s.peerStore.selfGroupcacheAddr {
						targets = append(targets, entry)
					}
				}
			}()

			if len(targets) > 0 {
				// log.Printf("[%s PeerService Heartbeater] 正在向 %d 个节点发送心跳...", s.peerStore.GetSelfGroupcacheAddr(), len(targets))
			}
			for _, targetPeer := range targets {
				targetURL := targetPeer.ApiAddress + "/admin/heartbeat"
				err := sendPostRequest(targetURL, s.nodeSelfAnnouncePayload, nil, 0) // 使用 client.go 的 sendPostRequest
				if err != nil {
					// 错误由 sendPostRequest 记录，PeerStore 的剪枝将处理无响应的节点。
					// log.Printf("[%s PeerService Heartbeater] 向 %s (API: %s) 发送心跳时出错: %v", s.peerStore.GetSelfGroupcacheAddr(), targetPeer.GroupcacheAddress, targetPeer.ApiAddress, err)
				}
			}
		}
	}
}

// periodicUpdater 定期触发 PeerStore 剔除失效节点并更新 groupcache pool。
func (s *PeerService) periodicUpdater() {
	defer s.wg.Done()
	//log.Printf("[%s PeerService PeriodicUpdater] 启动...", s.peerStore.GetSelfGroupcacheAddr())
	// PeerStore 的超时是剪枝的真实来源。这个定时器确保它被定期检查。
	// 使用节点超时的一部分，或固定的合理间隔。
	checkInterval := s.peerStore.peerTimeoutDuration / 2
	if checkInterval < 1*time.Second { // 确保最小检查间隔
		checkInterval = 1 * time.Second
	}
	if checkInterval > 10*time.Second { // 限制检查间隔
		checkInterval = 10 * time.Second
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopSignal:
			log.Printf("[%s PeerService PeriodicUpdater] 正在关闭。", s.peerStore.GetSelfGroupcacheAddr())
			return
		case <-ticker.C:
			// log.Printf("[%s PeerService PeriodicUpdater] 周期检查。检查死亡节点并更新 groupcache pool。", s.peerStore.GetSelfGroupcacheAddr())
			s.peerStore.UpdateGroupcachePoolIfNeeded()
		}
	}
}
