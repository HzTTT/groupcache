package peermanager

// AnnouncePayload 是节点在自我通告或发送心跳时携带的数据。
// GroupcacheAddress 表示发送节点的 groupcache 地址
// ApiAddress 表示发送节点的 API/admin 地址
type AnnouncePayload struct {
	GroupcacheAddress string `json:"groupcache_address"` // The groupcache address of the sending node
	ApiAddress        string `json:"api_address"`        // The API/admin address of the sending node
}

// AnnounceResponse 是节点向其他节点通告自身后收到的数据。
// 包含接收方已知的节点列表。
type AnnounceResponse struct {
	KnownPeers []AnnouncePayload `json:"known_peers"`
}
