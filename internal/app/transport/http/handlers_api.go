package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/golang/groupcache"
	// Placeholder for actual import paths - will be resolved once module path is known
	pm "github.com/golang/groupcache/internal/app/peermanager"

	cfg "github.com/golang/groupcache/internal/app/config"
)

// ApiHandlers 持有面向客户端的 API 和信息性 HTTP 处理程序的依赖项。
// 它使用 groupcache.Group 进行数据检索，使用 PeerStore 获取对等节点信息。
type ApiHandlers struct {
	Group     *groupcache.Group
	PeerStore *pm.PeerStore
	AppConfig *cfg.AppConfig // 用于访问自身 API/groupcache 地址以进行日志记录/信息获取
}

// NewApiHandlers 创建一个新的 ApiHandlers。
func NewApiHandlers(group *groupcache.Group, ps *pm.PeerStore, appCfg *cfg.AppConfig) *ApiHandlers {
	return &ApiHandlers{
		Group:     group,
		PeerStore: ps,
		AppConfig: appCfg,
	}
}

// GetHandler 处理从 groupcache 检索键的请求。
func (h *ApiHandlers) GetHandler(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "缺少 \"key\" 查询参数", http.StatusBadRequest)
		return
	}

	nodeAddr := "未知节点" // 如果配置或对等节点存储为 nil（实践中不应发生），则为默认值
	if h.AppConfig != nil {
		nodeAddr = h.AppConfig.SelfGroupcacheAddr
	} else if h.PeerStore != nil { // 后备方案，尽管 AppConfig 应该是自身地址的主要来源
		// 如果 PeerStore 本身没有直接的自身地址，则此后备方案可能不够健壮。
		// 对于自身节点的地址，优先使用 AppConfig 更清晰。
		nodeAddr = h.PeerStore.GetSelfGroupcacheAddr()
	}
	log.Printf("[%s API /get] 收到键请求: %q", nodeAddr, key)

	var data []byte
	// 为 Get 操作创建一个带超时的上下文。
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if h.Group == nil {
		log.Printf("[%s API /get] 错误: groupcache 组未初始化", nodeAddr)
		http.Error(w, "内部服务器错误: groupcache 不可用", http.StatusInternalServerError)
		return
	}

	err := h.Group.Get(ctx, key, groupcache.AllocatingByteSliceSink(&data))
	if err != nil {
		log.Printf("[%s API /get] 从 groupcache 获取键 %q 时出错: %v", nodeAddr, key, err)
		http.Error(w, fmt.Sprintf("获取键 %s 时出错: %v", key, err), http.StatusInternalServerError)
		return
	}

	log.Printf("[%s API /get] 成功检索到键 %q。值: %s", nodeAddr, key, string(data))
	w.Header().Set("Content-Type", "text/plain")
	w.Write(data)
}

// PingApiHandler 是 API 服务的简单 ping 端点。
// 它还显示节点的地址和已知的活动 groupcache 对等节点。
func (h *ApiHandlers) PingApiHandler(w http.ResponseWriter, r *http.Request) {
	apiAddr := "[配置不可用]"
	gcAddr := "[配置不可用]"
	var livePeers []string

	if h.AppConfig != nil {
		apiAddr = h.AppConfig.SelfApiAddr
		gcAddr = h.AppConfig.SelfGroupcacheAddr
	}
	if h.PeerStore != nil {
		// GetLivePeerGroupcacheAddrsAndPrune 也会进行修剪，这对于信息端点来说是可以的。
		livePeers = h.PeerStore.GetLivePeerGroupcacheAddrsAndPrune()
	}

	fmt.Fprintf(w, "来自 API 服务器 %s (groupcache 服务位于: %s) 的 pong \n已知的活动 groupcache 对等节点: %v\n",
		apiAddr, gcAddr, livePeers)
}

// KnownPeersHandler 提供一个端点来查看 PeerStore 已知的所有对等节点 (用于调试/信息)。
// 这包括自身和可能已失效的对等节点及其最后可见时间。
func (h *ApiHandlers) KnownPeersHandler(w http.ResponseWriter, r *http.Request) {
	nodeAddr := "[配置不可用]"
	if h.AppConfig != nil {
		nodeAddr = h.AppConfig.SelfGroupcacheAddr
	}

	if h.PeerStore == nil {
		log.Printf("[%s API /admin/known_peers] 错误: PeerStore 未初始化", nodeAddr)
		http.Error(w, "内部服务器错误: PeerStore 不可用", http.StatusInternalServerError)
		return
	}

	allPeers := h.PeerStore.GetAllKnownPeers() // 此方法提供 peerStore 中所有条目的快照
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(allPeers); err != nil {
		log.Printf("[%s API /admin/known_peers] 编码 known_peers 响应时出错: %v", nodeAddr, err)
	}
}
