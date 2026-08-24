package service

import (
	"errors"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func SyncUpstreamCandidate(upstreamID int64) (*model.Channel, error) {
	return syncUpstreamCandidate(upstreamID, "")
}

func SyncUpstreamCandidateWithTag(upstreamID int64, tag string) (*model.Channel, error) {
	normalizedTag, err := normalizeUpstreamSyncTag(tag)
	if err != nil {
		return nil, err
	}
	return syncUpstreamCandidate(upstreamID, normalizedTag)
}

func normalizeUpstreamSyncTag(tag string) (string, error) {
	tag = strings.TrimSpace(tag)
	if len(tag) > 128 {
		return "", errors.New("channel tag must be at most 128 characters")
	}
	return tag, nil
}

func syncUpstreamCandidate(upstreamID int64, tag string) (*model.Channel, error) {
	tx := model.DB.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	candidate, err := model.LockUpstreamCandidate(tx, upstreamID)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if candidate.ChannelID != nil {
		tx.Rollback()
		return model.GetChannelById(*candidate.ChannelID, false)
	}
	channel, err := syncUpstreamCandidateTx(tx, candidate, tag)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	model.InitChannelCache()
	channel.Key = ""
	return channel, nil
}

func SyncUpstreamCandidates(upstreamIDs []int64, tag string) ([]*model.Channel, error) {
	normalizedTag, err := normalizeUpstreamSyncTag(tag)
	if err != nil {
		return nil, err
	}
	ids := append([]int64(nil), upstreamIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	unique := ids[:0]
	for _, id := range ids {
		if id > 0 && (len(unique) == 0 || unique[len(unique)-1] != id) {
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return nil, errors.New("at least one upstream is required")
	}
	tx := model.DB.Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	channels := make([]*model.Channel, 0, len(unique))
	for _, id := range unique {
		candidate, err := model.LockUpstreamCandidate(tx, id)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		if candidate.ChannelID != nil {
			tx.Rollback()
			return nil, errors.New("one or more upstreams are already synchronized")
		}
		channel, err := syncUpstreamCandidateTx(tx, candidate, normalizedTag)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		channels = append(channels, channel)
	}
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	model.InitChannelCache()
	for _, channel := range channels {
		channel.Key = ""
	}
	return channels, nil
}

func syncUpstreamCandidateTx(tx *gorm.DB, candidate *model.UpstreamCandidate, tag string) (*model.Channel, error) {
	if candidate.BenchmarkStatus != constant.UpstreamBenchmarkCompleted {
		return nil, errors.New("upstream benchmark is not completed")
	}
	if candidate.Source == constant.UpstreamSourceSelf && candidate.ApplicationID != nil {
		application, err := model.LockBusinessCooperationByID(tx, *candidate.ApplicationID)
		if err != nil {
			return nil, err
		}
		if application.Status != constant.BusinessCooperationPendingDecision {
			return nil, errors.New("upstream application is not awaiting approval")
		}
	}
	models := strings.TrimSpace(candidate.Models)
	if models == "" {
		models = "gpt-4o-mini"
		if candidate.Type == constant.ChannelTypeGemini {
			models = "gemini-1.5-flash"
		}
	}
	testModel := strings.Split(models, ",")[0]
	if candidate.LatestRunID != nil {
		var run model.UpstreamBenchmarkRun
		err := tx.Where("id = ? AND upstream_id = ?", *candidate.LatestRunID, candidate.ID).First(&run).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil && strings.TrimSpace(run.Model) != "" {
			benchmarkModel := strings.TrimSpace(run.Model)
			testModel = benchmarkModel
			if strings.TrimSpace(candidate.Models) == "" {
				models = benchmarkModel
			}
		}
	}
	channelBaseURL := candidate.NormalizedBaseURL
	var channelTag *string
	if tag != "" {
		channelTag = &tag
	}
	channel := &model.Channel{Type: candidate.Type, Key: candidate.APIKey, Name: candidate.Name, Status: common.ChannelStatusEnabled, Models: models, Group: "default", CreatedTime: common.GetTimestamp(), BaseURL: &channelBaseURL, TestModel: &testModel, Tag: channelTag}
	if err := tx.Create(channel).Error; err != nil {
		return nil, err
	}
	if err := channel.AddAbilities(tx); err != nil {
		return nil, err
	}
	if err := tx.Model(candidate).Updates(map[string]any{"channel_id": channel.Id, "synced_time": common.GetTimestamp()}).Error; err != nil {
		return nil, err
	}
	if candidate.ApplicationID != nil {
		if err := tx.Model(&model.BusinessCooperation{}).Where("id = ?", *candidate.ApplicationID).Updates(map[string]any{"status": constant.BusinessCooperationApproved, "reviewed_time": common.GetTimestamp()}).Error; err != nil {
			return nil, err
		}
	}
	return channel, nil
}

func RejectUpstreamCandidate(upstreamID int64, reason string) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		candidate, err := model.LockUpstreamCandidate(tx, upstreamID)
		if err != nil {
			return err
		}
		if candidate.Source != constant.UpstreamSourceSelf || candidate.ApplicationID == nil {
			return errors.New("only self-submitted upstreams can be rejected")
		}
		if strings.TrimSpace(reason) == "" {
			return errors.New("reject reason is required")
		}
		application, err := model.LockBusinessCooperationByID(tx, *candidate.ApplicationID)
		if err != nil {
			return err
		}
		if application.Status != constant.BusinessCooperationPendingDecision || candidate.BenchmarkStatus != constant.UpstreamBenchmarkCompleted || candidate.ChannelID != nil {
			return errors.New("only completed unsynchronized applications awaiting approval can be rejected")
		}
		return tx.Model(application).Updates(map[string]any{"status": constant.BusinessCooperationRejected, "reject_reason": strings.TrimSpace(reason), "reviewed_time": common.GetTimestamp()}).Error
	})
}
