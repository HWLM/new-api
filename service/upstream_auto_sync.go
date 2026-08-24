package service

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

func TryAutoSyncUpstream(upstreamID int64, score float64) error {
	config := GetUpstreamAutoSyncConfig()
	if !config.Enabled || score < config.MinScore {
		return nil
	}
	candidate, err := model.GetUpstreamCandidate(upstreamID)
	if err != nil {
		return err
	}
	if candidate == nil {
		return errors.New("upstream not found")
	}
	if candidate.ChannelID != nil || candidate.BenchmarkStatus != constant.UpstreamBenchmarkCompleted {
		return nil
	}
	return syncUpstreamCandidateWithTag(upstreamID, config.ChannelTag)
}

func syncUpstreamCandidateWithTag(upstreamID int64, tag string) error {
	_, err := SyncUpstreamCandidateWithTag(upstreamID, tag)
	if err != nil {
		return err
	}
	return nil
}

// SyncEligibleUpstreams applies a newly enabled automatic-sync policy to
// completed benchmark runs that were waiting for a policy decision.
func SyncEligibleUpstreams(config dto.UpstreamAutoSyncConfig) {
	if !config.Enabled {
		return
	}
	candidates, _, err := model.ListUpstreamCandidates(0, 10000, "", "", constant.UpstreamBenchmarkCompleted)
	if err != nil {
		common.SysError("failed to list upstreams for automatic synchronization: " + err.Error())
		return
	}
	for _, candidate := range candidates {
		if candidate == nil || candidate.ChannelID != nil || candidate.LatestRunID == nil {
			continue
		}
		run, err := model.GetUpstreamBenchmarkRun(*candidate.LatestRunID)
		if err != nil || run == nil || run.Status != constant.UpstreamBenchmarkRunSucceeded || run.OverallScore < config.MinScore {
			continue
		}
		if err := syncUpstreamCandidateWithTag(candidate.ID, config.ChannelTag); err != nil {
			common.SysError("automatic upstream synchronization failed: " + err.Error())
			_ = model.DB.Model(run).Updates(map[string]any{
				"auto_sync_status": "failed",
				"auto_sync_error":  common.MaskSensitiveInfo(err.Error()),
			}).Error
			continue
		}
		_ = model.DB.Model(run).Updates(map[string]any{
			"auto_sync_status": "succeeded",
			"auto_synced_time": common.GetTimestamp(),
			"auto_sync_error":  "",
		}).Error
	}
}
