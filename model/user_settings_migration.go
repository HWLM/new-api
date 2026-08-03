package model

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const userSettingsRecordIpLogMigrationKey = "migration.user_settings.record_ip_log_enabled.v1"
const userSettingsRecordIpLogMigrationBatchSize = 100
const userSettingsRecordIpLogMigrationLockType = "user_settings_record_ip_log_migration"
const userSettingsRecordIpLogMigrationLockLeaseSeconds int64 = 300

// MigrateUserSettingsRecordIpLogEnabled enables IP logging for legacy users
// that do not have an explicit preference. The marker prevents future opt-outs
// from being changed on later startups.
func MigrateUserSettingsRecordIpLogEnabled() error {
	var marker Option
	err := DB.Where(commonKeyCol+" = ?", userSettingsRecordIpLogMigrationKey).First(&marker).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("check record IP log migration marker: %w", err)
	}
	recordIpLog, err := common.Marshal(true)
	if err != nil {
		return err
	}
	runnerID, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	acquired, _, err := acquireSystemTaskLock(
		userSettingsRecordIpLogMigrationLockType,
		userSettingsRecordIpLogMigrationKey,
		runnerID,
		now,
		now+userSettingsRecordIpLogMigrationLockLeaseSeconds,
	)
	if err != nil {
		return fmt.Errorf("acquire record IP log migration lock: %w", err)
	}
	if !acquired {
		return nil
	}
	defer func() {
		if err := ReleaseSystemTaskLock(userSettingsRecordIpLogMigrationKey, runnerID); err != nil {
			common.SysError("release record IP log migration lock: " + err.Error())
		}
	}()
	if err := DB.Where(commonKeyCol+" = ?", userSettingsRecordIpLogMigrationKey).First(&marker).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("recheck record IP log migration marker: %w", err)
	}

	lastID := 0
	for {
		var users []User
		if err := DB.Select("id", "setting").Where("id > ?", lastID).Order("id").Limit(userSettingsRecordIpLogMigrationBatchSize).Find(&users).Error; err != nil {
			return err
		}
		if len(users) == 0 {
			break
		}

		if err := DB.Transaction(func(tx *gorm.DB) error {
			for i := range users {
				setting := make(map[string]json.RawMessage)
				if users[i].Setting != "" {
					if err := common.Unmarshal([]byte(users[i].Setting), &setting); err != nil {
						common.SysError(fmt.Sprintf("skipping record IP log migration for user %d: invalid settings JSON: %v", users[i].Id, err))
						continue
					}
				}
				if setting == nil {
					setting = make(map[string]json.RawMessage)
				}
				if _, ok := setting["record_ip_log"]; ok {
					continue
				}
				setting["record_ip_log"] = recordIpLog
				settingBytes, err := common.Marshal(setting)
				if err != nil {
					return fmt.Errorf("marshal settings for user %d: %w", users[i].Id, err)
				}
				if err := tx.Model(&User{}).Where("id = ?", users[i].Id).Update("setting", string(settingBytes)).Error; err != nil {
					return fmt.Errorf("enable IP logging for user %d: %w", users[i].Id, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if err := RenewSystemTaskLock(userSettingsRecordIpLogMigrationKey, runnerID, common.GetTimestamp()+userSettingsRecordIpLogMigrationLockLeaseSeconds); err != nil {
			return fmt.Errorf("renew record IP log migration lock: %w", err)
		}

		lastID = users[len(users)-1].Id
	}

	return DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: userSettingsRecordIpLogMigrationKey, Value: "done"}).Error
}
