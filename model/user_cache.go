package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

// userCacheSchemaVersion 用户缓存哈希的结构版本。
//
// ⛔ 加法都要升版：旧版本哈希里没有新字段，读出来是零值，而零值在这里有语义
// （例如 DistillDetectOverride=0 = 跟随全局）。不升版会让新字段在缓存 TTL
// 内静默读成 0，用户编辑完三态后看起来「没有生效」。
//
//	3 → 4  加入 MaxConcurrency / RefusalDemoteOverride / DistillDetectOverride
//	4 → 5  加入 GroupRateLimits（分组速率，规格 §21）
//	5 → 6  加入 DistillNotifyOverride（蒸馏告警是否通知用户的三态）
const userCacheSchemaVersion = 6

type UserBase struct {
	Id                    int    `json:"id"`
	Role                  int    `json:"role"`
	Group                 string `json:"group"`
	Email                 string `json:"email"`
	Quota                 int    `json:"quota"`
	Status                int    `json:"status"`
	Username              string `json:"username"`
	Setting               string `json:"setting"`
	MaxConcurrency        int    `json:"max_concurrency"`
	GroupRateLimits       string `json:"group_rate_limits"`
	RefusalDemoteOverride int    `json:"refusal_demote_override"`
	DistillDetectOverride int    `json:"distill_detect_override"`
	DistillNotifyOverride int    `json:"distill_notify_override"`
	AuthVersion           int64  `json:"-"`
	CacheSchema           int    `json:"-"`
}

func (user *UserBase) WriteContext(c *gin.Context) {
	c.Set("role", user.Role)
	common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
	common.SetContextKey(c, constant.ContextKeyUserQuota, user.Quota)
	common.SetContextKey(c, constant.ContextKeyUserStatus, user.Status)
	common.SetContextKey(c, constant.ContextKeyUserEmail, user.Email)
	common.SetContextKey(c, constant.ContextKeyUserName, user.Username)
	common.SetContextKey(c, constant.ContextKeyUserSetting, user.GetSetting())
}

func (user *UserBase) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{RecordIpLog: true}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

// getUserCacheKey returns the key for user cache
func getUserCacheKey(userId int) string {
	return fmt.Sprintf("user:%d", userId)
}

func getUserCacheHashTag(userId int) string {
	return fmt.Sprintf("{%s}", getUserCacheKey(userId))
}

func userCacheTTLSeconds() int {
	ttl := common.RedisKeyCacheSeconds()
	if ttl <= 0 {
		return 60
	}
	return ttl
}

// invalidateUserCache clears user cache
func invalidateUserCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisDelKey(getUserCacheKey(userId))
}

// InvalidateUserCache is the exported version of invalidateUserCache.
// 供 controller 等上层包在用户状态变更（如禁用、删除、角色变更）后主动清理缓存。
func InvalidateUserCache(userId int) error {
	return invalidateUserCache(userId)
}

func populateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}
	return writeUserCache(user.ToBaseUser(), true)
}

// updateUserCache refreshes non-quota user cache fields.
// Quota is maintained by atomic quota delta paths and must not be overwritten
// by stale user snapshots from profile/settings updates.
func updateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}
	if err := updateUserGroupCache(user.Id, user.Group); err != nil {
		return err
	}
	if err := updateUserRoleCache(user.Id, user.Role); err != nil {
		return err
	}
	if err := updateUserEmailCache(user.Id, user.Email); err != nil {
		return err
	}
	if err := updateUserStatusCache(user.Id, user.Status == common.UserStatusEnabled); err != nil {
		return err
	}
	if err := updateUserNameCache(user.Id, user.Username); err != nil {
		return err
	}
	// 下面三个是「改了必须立刻生效」的策略字段：运营就是为了止损才会去改，
	// 等 60 秒 TTL 等于这一分钟继续按旧策略跑。
	if err := updateUserMaxConcurrencyCache(user.Id, user.MaxConcurrency); err != nil {
		return err
	}
	if err := updateUserGroupRateLimitsCache(user.Id, user.GroupRateLimits); err != nil {
		return err
	}
	if err := updateUserRefusalDemoteOverrideCache(user.Id, user.RefusalDemoteOverride); err != nil {
		return err
	}
	if err := updateUserDistillDetectOverrideCache(user.Id, user.DistillDetectOverride); err != nil {
		return err
	}
	if err := updateUserDistillNotifyOverrideCache(user.Id, user.DistillNotifyOverride); err != nil {
		return err
	}
	return updateUserSettingCache(user.Id, user.Setting)
}

// GetUserCache gets complete user cache from hash
func GetUserCache(userId int) (*UserBase, error) {
	// Try getting from Redis first
	userCache, err := cacheGetUserBase(userId)
	if err == nil {
		return userCache, nil
	}

	// Redis misses and read failures both fall back to the shared database. A
	// version fence newer than the database is the one exception: allowing that
	// snapshot would re-authorize a user while a restrictive update is pending.
	user, err := GetUserById(userId, false)
	if err != nil {
		return nil, err
	}

	if common.RedisEnabled {
		floor, floorErr := getUserAuthVersionFloor(userId)
		if floorErr == nil && floor > user.AuthVersion {
			return nil, ErrUserAuthCachePending
		}
		if err := populateUserCache(*user); err != nil {
			if errors.Is(err, ErrUserAuthCachePending) {
				return nil, err
			}
			common.SysLog("failed to synchronously populate user cache: " + err.Error())
		}
	}
	return user.ToBaseUser(), nil
}

func cacheGetUserBase(userId int) (*UserBase, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var userCache UserBase
	// Try getting from Redis first
	err := common.RedisHGetObj(getUserCacheKey(userId), &userCache)
	if err != nil {
		return nil, err
	}
	if userCache.Id != userId || userCache.CacheSchema != userCacheSchemaVersion || userCache.AuthVersion <= 0 {
		return nil, fmt.Errorf("user cache schema is stale")
	}
	floor, err := getUserAuthVersionFloor(userId)
	if err != nil {
		return nil, err
	}
	if floor > userCache.AuthVersion {
		return nil, ErrUserAuthCachePending
	}
	return &userCache, nil
}

// Add atomic quota operations using hash fields
func cacheIncrUserQuota(userId int, delta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHIncrBy(getUserCacheKey(userId), "Quota", delta)
}

func cacheDecrUserQuota(userId int, delta int64) error {
	return cacheIncrUserQuota(userId, -delta)
}

// Helper functions to get individual fields if needed
func getUserGroupCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Group, nil
}

func getUserQuotaCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Quota, nil
}

func getUserStatusCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Status, nil
}

func getUserNameCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Username, nil
}

func getUserSettingCache(userId int) (dto.UserSetting, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return dto.UserSetting{}, err
	}
	return cache.GetSetting(), nil
}

// New functions for individual field updates
func updateUserStatusCache(userId int, status bool) error {
	statusInt := common.UserStatusEnabled
	if !status {
		statusInt = common.UserStatusDisabled
	}
	return updateUserCacheField(userId, "Status", statusInt)
}

func updateUserQuotaCache(userId int, quota int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Quota", fmt.Sprintf("%d", quota))
}

func updateUserGroupCache(userId int, group string) error {
	return updateUserCacheField(userId, "Group", group)
}

// RefreshUserGroupCache writes the database-authoritative group into an
// existing user hash without changing the user's authentication version.
func RefreshUserGroupCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	if userId <= 0 {
		return fmt.Errorf("invalid user id")
	}
	var authoritative User
	if err := DB.Select("id", "auth_version", commonGroupCol).Where("id = ?", userId).First(&authoritative).Error; err != nil {
		return err
	}
	// Group transitions intentionally keep the same authentication version. A
	// refresh that read the previous group can therefore arrive after a newer
	// refresh and still pass the auth-version fence. Re-read after every write
	// and repair the cache when the authoritative group changed in between.
	for range 3 {
		if err := updateUserCacheFieldAtVersion(userId, "Group", authoritative.Group, authoritative.AuthVersion); err != nil {
			return err
		}

		var verified User
		if err := DB.Select("id", "auth_version", commonGroupCol).Where("id = ?", userId).First(&verified).Error; err != nil {
			return err
		}
		if verified.AuthVersion == authoritative.AuthVersion && verified.Group == authoritative.Group {
			return nil
		}
		authoritative = verified
	}

	if err := updateUserCacheFieldAtVersion(userId, "Group", authoritative.Group, authoritative.AuthVersion); err != nil {
		return err
	}
	return fmt.Errorf("user group changed repeatedly during cache refresh")
}

func updateUserRoleCache(userId int, role int) error {
	return updateUserCacheField(userId, "Role", role)
}

func UpdateUserGroupCache(userId int, group string) error {
	return updateUserGroupCache(userId, group)
}

func updateUserEmailCache(userId int, email string) error {
	return updateUserCacheField(userId, "Email", email)
}

func updateUserNameCache(userId int, username string) error {
	return updateUserCacheField(userId, "Username", username)
}

func updateUserSettingCache(userId int, setting string) error {
	return updateUserCacheField(userId, "Setting", setting)
}

// updateUserDistillDetectOverrideCache 立即刷新蒸馏检测三态。
//
// ⛔ 不能只等缓存 TTL（默认 60s）：运营在用户编辑里把某人豁免掉，是因为他
// 正在被反复判中；等一分钟才生效，等于这段时间的命中照常记、告警照常发。
func updateUserDistillDetectOverrideCache(userId int, override int) error {
	return updateUserCacheField(userId, "DistillDetectOverride", override)
}

// updateUserDistillNotifyOverrideCache 立即刷新「蒸馏告警是否通知用户」三态。
func updateUserDistillNotifyOverrideCache(userId int, override int) error {
	return updateUserCacheField(userId, "DistillNotifyOverride", override)
}

// updateUserRefusalDemoteOverrideCache 同上，拒答降级三态。
func updateUserRefusalDemoteOverrideCache(userId int, override int) error {
	return updateUserCacheField(userId, "RefusalDemoteOverride", override)
}

// updateUserMaxConcurrencyCache 并发上限，同属「改了要立刻生效」的一类。
func updateUserMaxConcurrencyCache(userId int, value int) error {
	return updateUserCacheField(userId, "MaxConcurrency", value)
}

// updateUserGroupRateLimitsCache 分组速率，同样「改了要立刻生效」。
func updateUserGroupRateLimitsCache(userId int, value string) error {
	return updateUserCacheField(userId, "GroupRateLimits", value)
}

// updateUserCacheField prevents individual cache refreshes from bypassing the
// auth-version fence. It intentionally does nothing when the complete hash is
// absent; the next GetUserCache call will repopulate it from the database.
func updateUserCacheField(userId int, field string, value interface{}) error {
	if !common.RedisEnabled {
		return nil
	}
	var user User
	if err := DB.Select("id", "auth_version").Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if user.AuthVersion <= 0 {
		return fmt.Errorf("invalid user auth version")
	}
	return updateUserCacheFieldAtVersion(userId, field, value, user.AuthVersion)
}

// GetUserLanguage returns the user's language preference from cache
// Uses the existing GetUserCache mechanism for efficiency
func GetUserLanguage(userId int) string {
	userCache, err := GetUserCache(userId)
	if err != nil {
		return ""
	}
	return userCache.GetSetting().Language
}
