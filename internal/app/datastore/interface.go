package datastore

// DataStore 定义了数据存储的接口
// 所有实现此接口的存储都应该能够按键检索数据
type DataStore interface {
	// Get 通过键从数据存储中检索值
	Get(key string) ([]byte, error)
}
