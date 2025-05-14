package http

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/golang/groupcache/internal/app/peermanager"
)

// AdminHandlers 持有管理相关 HTTP 处理程序的依赖项。
// 它主要使用 PeerStore 来根据管理操作管理对等节点信息。
type AdminHandlers struct {
	PeerStore *peermanager.PeerStore
	// SelfGroupcacheAddr string // 用于日志记录，可从 PeerStore.GetSelfGroupcacheAddr() 获取
}

// NewAdminHandlers 创建一个新的 AdminHandlers。
func NewAdminHandlers(ps *peermanager.PeerStore) *AdminHandlers {
	return &AdminHandlers{PeerStore: ps}
}

// AnnounceSelfHandler 处理来自其他节点宣告自身存在的请求。
// 它使用宣告者的信息更新对等节点存储，并返回当前已知对等节点的列表。
func (h *AdminHandlers) AnnounceSelfHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "/admin/announce_self 只允许 POST 请求", http.StatusMethodNotAllowed)
		return
	}
	var payload peermanager.AnnouncePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "announce_self 请求体无效", http.StatusBadRequest)
		return
	}
	log.Printf("[%s 管理] 收到来自 %s (API: %s) 的 /admin/announce_self 请求", h.PeerStore.GetSelfGroupcacheAddr(), payload.GroupcacheAddress, payload.ApiAddress)

	if payload.GroupcacheAddress == "" || payload.ApiAddress == "" {
		http.Error(w, "announce_self 请求体中缺少 groupcache_address 或 api_address", http.StatusBadRequest)
		return
	}

	// 添加或更新对等节点，并检查这是否导致了可能影响 groupcache 池的更改
	h.PeerStore.AddOrUpdatePeer(payload.GroupcacheAddress, payload.ApiAddress, time.Now())
	h.PeerStore.UpdateGroupcachePoolIfNeeded() // 更新 groupcache 对等节点至关重要

	// 返回当前已知的对等节点。这有助于新节点发现网络。
	var currentKnownPeers []peermanager.AnnouncePayload
	knownPeersMap := h.PeerStore.GetAllKnownPeers()

	for _, entry := range knownPeersMap {
		// 可选：不要在列表中将宣告者自身或接收者自身发送回去。
		// 这个简单的实现发送所有已知的对等节点。
		// if gcAddr == payload.GroupcacheAddress { continue } // 不要将宣告者发送回其自身
		// if gcAddr == h.PeerStore.GetSelfGroupcacheAddr() { continue } // 不要将自身发送给宣告者

		currentKnownPeers = append(currentKnownPeers, peermanager.AnnouncePayload{
			GroupcacheAddress: entry.GroupcacheAddress,
			ApiAddress:        entry.ApiAddress,
		})
	}

	respData := peermanager.AnnounceResponse{KnownPeers: currentKnownPeers}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(respData); err != nil {
		log.Printf("[%s 管理] 编码 announce_self 响应时出错: %v", h.PeerStore.GetSelfGroupcacheAddr(), err)
		// 如果此处发生错误，头部可能已经写入，
		// 因此发送 http.Error 可能无效或导致进一步的问题。
	}
}

// HeartbeatHandler 处理来自其他节点的 心跳请求。
// 它更新对等节点的最后可见时间。
func (h *AdminHandlers) HeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "/admin/heartbeat 只允许 POST 请求", http.StatusMethodNotAllowed)
		return
	}
	var payload peermanager.AnnouncePayload // 心跳请求为简单起见使用与宣告相同的载荷结构
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "heartbeat 请求体无效", http.StatusBadRequest)
		return
	}
	// 详细记录每个心跳对于生产环境来说可能过于冗余。
	// log.Printf("[%s 管理] 收到来自 %s (API: %s) 的 /admin/heartbeat 请求", h.PeerStore.GetSelfGroupcacheAddr(), payload.GroupcacheAddress, payload.ApiAddress)

	if payload.GroupcacheAddress == "" { // 如果节点已知，纯心跳请求中的 ApiAddress 可能是可选的
		http.Error(w, "heartbeat 载荷中缺少 groupcache_address", http.StatusBadRequest)
		return
	}
	// 如果心跳也需要 ApiAddress (例如，在其更改时进行更新)，也检查它。
	if payload.ApiAddress == "" {
		http.Error(w, "heartbeat 载荷中缺少 api_address", http.StatusBadRequest)
		return
	}

	h.PeerStore.AddOrUpdatePeer(payload.GroupcacheAddress, payload.ApiAddress, time.Now())
	// UpdateGroupcachePoolIfNeeded 由 AddOrUpdatePeer 或定期修剪器调用，
	// 但在此处调用可确保在对等节点恢复在线时立即反映。
	h.PeerStore.UpdateGroupcachePoolIfNeeded()
	w.WriteHeader(http.StatusOK)
}
