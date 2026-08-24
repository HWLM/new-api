package service

import (
	"errors"

	"github.com/QuantumNous/new-api/constant"
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
