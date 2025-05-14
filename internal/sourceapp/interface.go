package sourceapp

// DataSource 定义了数据源的接口
// 所有实现此接口的服务都能够通过键存储和检索数据
type DataSource interface {
	// Get 通过键从数据存储中检索值
	Get(key string) ([]byte, error)

	// Set 存储一个键值对
	Set(key string, value []byte) error

	// Delete 删除指定的键
	Delete(key string) error

	// Start 启动数据服务
	Start() error

	// Stop 停止数据服务
	Stop() error
}

// 确保 SQLiteService 实现了 DataSource 接口
var _ DataSource = (*SQLiteService)(nil)
