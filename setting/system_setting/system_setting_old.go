package system_setting

import "sync"

var ServerAddress = "http://localhost:3000"
var WorkerUrl = ""
var WorkerValidKey = ""
var WorkerAllowHttpImageRequestEnabled = false

// OpenAPIToken 供 /openapi 路由组使用的静态访问令牌；为空时拒绝所有 /openapi/* 请求。
// 通过系统设置后台编辑，存储到 options 表（key = "OpenAPIToken"）。
var (
	openAPIToken      string
	openAPITokenMutex sync.RWMutex
)

func SetOpenAPIToken(v string) {
	openAPITokenMutex.Lock()
	openAPIToken = v
	openAPITokenMutex.Unlock()
}

func GetOpenAPIToken() string {
	openAPITokenMutex.RLock()
	defer openAPITokenMutex.RUnlock()
	return openAPIToken
}

func EnableWorker() bool {
	return WorkerUrl != ""
}
