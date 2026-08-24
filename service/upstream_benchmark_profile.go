package service

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type UpstreamBenchmarkMetricConfig struct {
	Admission float64 `json:"admission"`
	Full      float64 `json:"full"`
	Weight    float64 `json:"weight"`
	Enabled   bool    `json:"enabled"`
	Direction string  `json:"direction"`
}

type UpstreamBenchmarkProfileConfig struct {
	Name    string                                   `json:"name"`
	Version int                                      `json:"version"`
	Metrics map[string]UpstreamBenchmarkMetricConfig `json:"metrics"`
}

var benchmarkMetricDirections = map[string]string{
	constant.UpstreamMetricRPM:          "higher",
	constant.UpstreamMetricTPM:          "higher",
	constant.UpstreamMetricTTFTP50_8K:   "lower",
	constant.UpstreamMetricTTFTP90_8K:   "lower",
	constant.UpstreamMetricTTFTP50_32K:  "lower",
	constant.UpstreamMetricTTFTP90_32K:  "lower",
	constant.UpstreamMetricTTFTP50_128K: "lower",
	constant.UpstreamMetricTTFTP90_128K: "lower",
	constant.UpstreamMetricOTPPSP50:     "higher",
}

func defaultUpstreamBenchmarkProfileConfig() UpstreamBenchmarkProfileConfig {
	metrics := map[string]UpstreamBenchmarkMetricConfig{
		constant.UpstreamMetricRPM: {
			Admission: 75, Full: 600, Weight: 1, Enabled: true, Direction: "higher",
		},
		constant.UpstreamMetricTPM: {
			Admission: 1_000, Full: 6_000_000, Weight: 1, Enabled: true, Direction: "higher",
		},
		constant.UpstreamMetricTTFTP50_8K: {
			Admission: 2_500, Full: 1_000, Weight: 1, Enabled: true, Direction: "lower",
		},
		constant.UpstreamMetricTTFTP90_8K: {
			Admission: 6_000, Full: 2_500, Weight: 1, Enabled: true, Direction: "lower",
		},
		constant.UpstreamMetricTTFTP50_32K: {
			Admission: 6_500, Full: 3_000, Weight: 1, Enabled: true, Direction: "lower",
		},
		constant.UpstreamMetricTTFTP90_32K: {
			Admission: 14_000, Full: 6_000, Weight: 1, Enabled: true, Direction: "lower",
		},
		constant.UpstreamMetricTTFTP50_128K: {
			Admission: 30_000, Full: 10_000, Weight: 1, Enabled: true, Direction: "lower",
		},
		constant.UpstreamMetricTTFTP90_128K: {
			Admission: 60_000, Full: 20_000, Weight: 1, Enabled: true, Direction: "lower",
		},
		constant.UpstreamMetricOTPPSP50: {
			Admission: 8, Full: 22, Weight: 1, Enabled: true, Direction: "higher",
		},
	}
	return UpstreamBenchmarkProfileConfig{Name: constant.UpstreamBenchmarkProfileGeneral, Version: 1, Metrics: metrics}
}

func normalizeUpstreamBenchmarkProfileConfig(config UpstreamBenchmarkProfileConfig) UpstreamBenchmarkProfileConfig {
	defaults := defaultUpstreamBenchmarkProfileConfig()
	if config.Metrics == nil {
		config.Metrics = make(map[string]UpstreamBenchmarkMetricConfig, len(defaults.Metrics))
	}
	for name, defaultMetric := range defaults.Metrics {
		metric, ok := config.Metrics[name]
		invalidThresholds := !isFinitePositive(metric.Admission) || !isFinitePositive(metric.Full) || !isFinitePositive(metric.Weight)
		invalidOrder := (defaultMetric.Direction == "higher" && metric.Full < metric.Admission) ||
			(defaultMetric.Direction == "lower" && metric.Full > metric.Admission)
		if !ok || invalidThresholds || invalidOrder {
			metric = defaultMetric
		} else {
			metric.Enabled = true
			metric.Direction = defaultMetric.Direction
		}
		config.Metrics[name] = metric
	}
	return config
}

func decodeUpstreamBenchmarkProfile(raw string) (UpstreamBenchmarkProfileConfig, error) {
	config := defaultUpstreamBenchmarkProfileConfig()
	if strings.TrimSpace(raw) == "" {
		return config, nil
	}
	if err := common.UnmarshalJsonStr(raw, &config); err != nil {
		return UpstreamBenchmarkProfileConfig{}, err
	}
	if config.Name == "" {
		config.Name = constant.UpstreamBenchmarkProfileGeneral
	}
	return normalizeUpstreamBenchmarkProfileConfig(config), nil
}

func validateUpstreamBenchmarkProfile(config UpstreamBenchmarkProfileConfig) error {
	if config.Name != constant.UpstreamBenchmarkProfileGeneral {
		return errors.New("only the general benchmark profile is supported")
	}
	if len(config.Metrics) == 0 {
		return errors.New("at least one benchmark metric is required")
	}
	enabled := 0
	for name, metric := range config.Metrics {
		direction, ok := benchmarkMetricDirections[name]
		if !ok {
			return errors.New("unsupported benchmark metric: " + name)
		}
		if metric.Direction == "" {
			metric.Direction = direction
		}
		if metric.Direction != direction {
			return errors.New("benchmark metric direction cannot be changed: " + name)
		}
		if !metric.Enabled {
			config.Metrics[name] = metric
			continue
		}
		enabled++
		if !isFinitePositive(metric.Admission) || !isFinitePositive(metric.Full) || !isFinitePositive(metric.Weight) {
			return errors.New("enabled benchmark metric thresholds and weight must be finite and positive: " + name)
		}
		if direction == "higher" && metric.Full < metric.Admission {
			return errors.New("higher-is-better metric full score must be >= admission: " + name)
		}
		if direction == "lower" && metric.Full > metric.Admission {
			return errors.New("lower-is-better metric full score must be <= admission: " + name)
		}
		config.Metrics[name] = metric
	}
	if enabled == 0 {
		return errors.New("at least one benchmark metric must be enabled")
	}
	return nil
}

func isFinitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func UpstreamBenchmarkProfileResponse(profile *model.UpstreamBenchmarkProfile) (dto.UpstreamBenchmarkProfileResponse, error) {
	config, err := decodeUpstreamBenchmarkProfile(profile.Config)
	if err != nil {
		return dto.UpstreamBenchmarkProfileResponse{}, err
	}
	metrics := make(map[string]dto.UpstreamBenchmarkMetric, len(config.Metrics))
	for name, metric := range config.Metrics {
		metrics[name] = dto.UpstreamBenchmarkMetric{Admission: metric.Admission, Full: metric.Full, Weight: metric.Weight, Enabled: metric.Enabled, Direction: metric.Direction}
	}
	return dto.UpstreamBenchmarkProfileResponse{Name: profile.Name, Version: profile.Version, Metrics: metrics, UpdatedTime: profile.UpdatedTime}, nil
}

func GetOrCreateUpstreamBenchmarkProfile(tx *gorm.DB) (*model.UpstreamBenchmarkProfile, UpstreamBenchmarkProfileConfig, error) {
	profile, err := model.GetUpstreamBenchmarkProfile(tx, constant.UpstreamBenchmarkProfileGeneral)
	if err != nil {
		return nil, UpstreamBenchmarkProfileConfig{}, err
	}
	if profile == nil {
		config := defaultUpstreamBenchmarkProfileConfig()
		data, err := common.Marshal(config)
		if err != nil {
			return nil, UpstreamBenchmarkProfileConfig{}, err
		}
		profile = &model.UpstreamBenchmarkProfile{Name: config.Name, Version: config.Version, Config: string(data)}
		if err := tx.Create(profile).Error; err != nil {
			return nil, UpstreamBenchmarkProfileConfig{}, err
		}
		return profile, config, nil
	}
	config, err := decodeUpstreamBenchmarkProfile(profile.Config)
	if err != nil {
		return nil, UpstreamBenchmarkProfileConfig{}, err
	}
	config.Name = profile.Name
	config.Version = profile.Version
	return profile, config, nil
}

func SaveUpstreamBenchmarkProfile(request dto.UpstreamBenchmarkProfileRequest) (dto.UpstreamBenchmarkProfileResponse, error) {
	config := UpstreamBenchmarkProfileConfig{Name: constant.UpstreamBenchmarkProfileGeneral, Metrics: make(map[string]UpstreamBenchmarkMetricConfig, len(request.Metrics))}
	for name, metric := range request.Metrics {
		config.Metrics[name] = UpstreamBenchmarkMetricConfig{Admission: metric.Admission, Full: metric.Full, Weight: metric.Weight, Enabled: metric.Enabled, Direction: metric.Direction}
	}
	if err := validateUpstreamBenchmarkProfile(config); err != nil {
		return dto.UpstreamBenchmarkProfileResponse{}, err
	}
	var response dto.UpstreamBenchmarkProfileResponse
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		profile, current, err := GetOrCreateUpstreamBenchmarkProfile(tx)
		if err != nil {
			return err
		}
		config.Version = current.Version + 1
		data, err := common.Marshal(config)
		if err != nil {
			return err
		}
		if err := tx.Model(profile).Updates(map[string]any{"version": config.Version, "config": string(data), "updated_time": common.GetTimestamp()}).Error; err != nil {
			return err
		}
		profile.Version = config.Version
		profile.Config = string(data)
		profile.UpdatedTime = common.GetTimestamp()
		response, err = UpstreamBenchmarkProfileResponse(profile)
		return err
	})
	return response, err
}

func GetUpstreamBenchmarkProfileResponse() (dto.UpstreamBenchmarkProfileResponse, error) {
	var response dto.UpstreamBenchmarkProfileResponse
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		profile, _, err := GetOrCreateUpstreamBenchmarkProfile(tx)
		if err != nil {
			return err
		}
		response, err = UpstreamBenchmarkProfileResponse(profile)
		return err
	})
	return response, err
}

func ScoreUpstreamBenchmarkMetric(actual float64, metric UpstreamBenchmarkMetricConfig) (float64, bool) {
	if !metric.Enabled || !isFinitePositive(metric.Admission) || !isFinitePositive(metric.Full) || !isFinitePositive(metric.Weight) || actual < 0 || math.IsNaN(actual) || math.IsInf(actual, 0) {
		return 0, false
	}
	if metric.Direction == "higher" {
		if actual < metric.Admission {
			return clampBenchmarkScore(60 * actual / metric.Admission), false
		}
		if metric.Full == metric.Admission || actual >= metric.Full {
			return 100, true
		}
		return clampBenchmarkScore(60 + 40*(actual-metric.Admission)/(metric.Full-metric.Admission)), true
	}
	if actual > metric.Admission {
		return clampBenchmarkScore(60 * metric.Admission / actual), false
	}
	if metric.Full == metric.Admission || actual <= metric.Full {
		return 100, true
	}
	return clampBenchmarkScore(60 + 40*(metric.Admission-actual)/(metric.Admission-metric.Full)), true
}

func clampBenchmarkScore(value float64) float64 {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func CalculateUpstreamBenchmarkScore(metrics map[string]float64, config UpstreamBenchmarkProfileConfig) (float64, map[string]float64, int) {
	var weighted, totalWeight float64
	scores := make(map[string]float64, len(config.Metrics))
	unmet := 0
	for name, metric := range config.Metrics {
		if !metric.Enabled {
			continue
		}
		actual, ok := metrics[name]
		if !ok {
			scores[name] = 0
			weighted += 0 * metric.Weight
			totalWeight += metric.Weight
			unmet++
			continue
		}
		score, met := ScoreUpstreamBenchmarkMetric(actual, metric)
		scores[name] = score
		weighted += score * metric.Weight
		totalWeight += metric.Weight
		if !met {
			unmet++
		}
	}
	if totalWeight == 0 {
		return 0, scores, unmet
	}
	return math.Round(weighted/totalWeight*10) / 10, scores, unmet
}

func GetUpstreamAutoSyncConfig() dto.UpstreamAutoSyncConfig {
	minScore, err := strconv.ParseFloat(model.GetOptionString(constant.UpstreamAutoSyncMinScoreOption), 64)
	if err != nil || minScore < 0 || minScore > 100 {
		minScore = 0
	}
	return dto.UpstreamAutoSyncConfig{
		Enabled:    model.GetOptionString(constant.UpstreamAutoSyncEnabledOption) == "true",
		ChannelTag: strings.TrimSpace(model.GetOptionString(constant.UpstreamAutoSyncChannelTagOption)),
		MinScore:   minScore,
	}
}

func UpdateUpstreamAutoSyncConfig(config dto.UpstreamAutoSyncConfig) (dto.UpstreamAutoSyncConfig, error) {
	config.ChannelTag = strings.TrimSpace(config.ChannelTag)
	if config.Enabled && config.ChannelTag == "" {
		return dto.UpstreamAutoSyncConfig{}, errors.New("channel_tag is required when automatic synchronization is enabled")
	}
	if math.IsNaN(config.MinScore) || math.IsInf(config.MinScore, 0) || config.MinScore < 0 || config.MinScore > 100 {
		return dto.UpstreamAutoSyncConfig{}, errors.New("min_score must be between 0 and 100")
	}
	err := model.UpdateOptionsBulk(map[string]string{
		constant.UpstreamAutoSyncEnabledOption:    strconv.FormatBool(config.Enabled),
		constant.UpstreamAutoSyncChannelTagOption: config.ChannelTag,
		constant.UpstreamAutoSyncMinScoreOption:   strconv.FormatFloat(config.MinScore, 'f', -1, 64),
	})
	return config, err
}
