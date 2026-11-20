package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 渠道缓存同步的容错（线上 00:43 那次 panic 的回归）。
//
// 现场：SQLite 一次 SQLITE_BUSY 让 SELECT abilities 返回 0 行，
// InitChannelCache 拿空结果重建缓存，随后按 channel.Group 往不存在的
// 内层 map 里写 → "assignment to entry in nil map" → 整进程 panic。
// 一次临时查询失败就能把服务打挂，MySQL 抖动同理。
//
// 两条要求都要锁住：
//  1. 查询出错直接放弃本次同步，旧缓存原样保留；
//  2. abilities 缺分组（两表短暂不一致）也不能对 nil map 赋值。

func setupChannelCacheTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousType := common.MainDatabaseType()
	previousMem := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.MemoryCacheEnabled = previousMem
	})
	return db
}

func TestInitChannelCacheKeepsOldCacheWhenAbilitiesQueryFails(t *testing.T) {
	db := setupChannelCacheTestDB(t)

	channel := &Channel{
		Id:     901,
		Name:   "cache-sync-test",
		Status: common.ChannelStatusEnabled,
		Models: "claude-3-5-sonnet",
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "claude-3-5-sonnet", ChannelId: 901}).Error)

	InitChannelCache()
	channelSyncLock.RLock()
	before := group2model2channels["default"]["claude-3-5-sonnet"]
	channelSyncLock.RUnlock()
	require.NotEmpty(t, before, "the cache must be populated before the failure is simulated")

	// 模拟瞬时查询失败：表没了，DB.Find 必然返回 error。
	require.NoError(t, db.Migrator().DropTable(&Ability{}))

	require.NotPanics(t, func() { InitChannelCache() }, "a failed sync must never panic the process")

	channelSyncLock.RLock()
	after := group2model2channels["default"]["claude-3-5-sonnet"]
	channelSyncLock.RUnlock()
	require.Equal(t, before, after, "the old cache must stay untouched when the sync query fails")
}

// abilities 与 channels 短暂不一致：abilities 里没有该分组，channels 却已有，
// 这时不许对 nil 内层 map 赋值。
func TestInitChannelCacheToleratesGroupsMissingFromAbilities(t *testing.T) {
	db := setupChannelCacheTestDB(t)

	require.NoError(t, db.Create(&Channel{
		Id:     902,
		Name:   "orphan-group-test",
		Status: common.ChannelStatusEnabled,
		Models: "claude-3-5-sonnet",
		Group:  "doudi",
	}).Error)
	// 故意不写 ability：channels 有 doudi，abilities 没有。

	require.NotPanics(t, func() { InitChannelCache() })

	channelSyncLock.RLock()
	got := group2model2channels["doudi"]["claude-3-5-sonnet"]
	channelSyncLock.RUnlock()
	require.Equal(t, []int{902}, got, "a group missing from abilities must still be indexed")
}
