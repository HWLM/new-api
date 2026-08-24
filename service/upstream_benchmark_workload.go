package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

type upstreamProbeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type upstreamProbeRequest struct {
	Model         string                 `json:"model,omitempty"`
	Messages      any                    `json:"messages,omitempty"`
	Query         string                 `json:"query,omitempty"`
	ResponseMode  string                 `json:"response_mode,omitempty"`
	User          string                 `json:"user,omitempty"`
	MaxTokens     int                    `json:"max_tokens,omitempty"`
	Stream        bool                   `json:"stream,omitempty"`
	GenerationCfg *upstreamGenerationCfg `json:"generationConfig,omitempty"`
	Contents      []upstreamProbeContent `json:"contents,omitempty"`
}

type upstreamGenerationCfg struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type upstreamProbeContent struct {
	Role  string              `json:"role"`
	Parts []upstreamProbePart `json:"parts"`
}

type upstreamProbePart struct {
	Text string `json:"text"`
}

type upstreamOpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	Tokens           struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"tokens"`
}

type upstreamGeminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type upstreamProbeResponse struct {
	Usage         upstreamOpenAIUsage `json:"usage"`
	UsageMetadata upstreamGeminiUsage `json:"usageMetadata"`
}

type upstreamProbeSample struct {
	StatusCode       int
	LatencyMS        int64
	PromptTokens     int
	CompletionTokens int
}

var benchmarkContextSizes = []struct {
	name  string
	bytes int
}{
	{constant.UpstreamMetricTTFTP50_8K, 8 * 1024},
	{constant.UpstreamMetricTTFTP50_32K, 32 * 1024},
	{constant.UpstreamMetricTTFTP50_128K, 128 * 1024},
}

func executeUpstreamWorkload(ctx context.Context, candidate *model.UpstreamCandidate, modelName string, concurrency int) (UpstreamBenchmarkResult, error) {
	if candidate == nil {
		return UpstreamBenchmarkResult{}, ErrUpstreamNotFound
	}
	if concurrency < 1 || concurrency > 100 {
		return UpstreamBenchmarkResult{}, errors.New("benchmark concurrency must be between 1 and 100")
	}
	modelName = strings.TrimSpace(modelName)
	models, err := DiscoverUpstreamModels(ctx, candidate)
	if err != nil {
		// Some channel types do not expose a model-list endpoint. A configured
		// model is sufficient to run the workload, so discovery failure should
		// not prevent benchmarking those channels.
		models = configuredUpstreamModels(candidate.Models)
		if len(models) == 0 {
			return UpstreamBenchmarkResult{}, err
		}
	}
	if modelName == "" {
		if len(models) == 0 {
			return UpstreamBenchmarkResult{}, errors.New("upstream returned no models")
		}
		modelName = models[0]
	}

	var allSamples []upstreamProbeSample
	batches := make(map[string][]upstreamProbeSample, len(benchmarkContextSizes))
	for _, stage := range benchmarkContextSizes {
		prompt := benchmarkPrompt(stage.bytes)
		samples := runUpstreamProbeBatch(ctx, candidate, modelName, prompt, concurrency)
		if len(samples) < concurrency {
			return UpstreamBenchmarkResult{}, errors.New("upstream benchmark did not complete all concurrent requests")
		}
		batches[stage.name] = samples
		allSamples = append(allSamples, samples...)
	}
	throughputPrompt := benchmarkPrompt(1024)
	throughputStarted := time.Now()
	throughputSamples := runUpstreamProbeBatch(ctx, candidate, modelName, throughputPrompt, concurrency)
	if len(throughputSamples) < concurrency {
		return UpstreamBenchmarkResult{}, errors.New("upstream throughput benchmark did not complete all concurrent requests")
	}
	throughputSeconds := time.Since(throughputStarted).Seconds()
	if throughputSeconds <= 0 {
		throughputSeconds = 0.001
	}
	allSamples = append(allSamples, throughputSamples...)

	result := aggregateUpstreamProbeSamples(allSamples, batches, throughputSamples, throughputSeconds)
	return result, nil
}

func benchmarkPrompt(targetBytes int) string {
	const unit = "benchmark context token "
	if targetBytes < len(unit) {
		return unit
	}
	var builder strings.Builder
	builder.Grow(targetBytes)
	for builder.Len() < targetBytes {
		builder.WriteString(unit)
	}
	return builder.String()[:targetBytes]
}

func runUpstreamProbeBatch(ctx context.Context, candidate *model.UpstreamCandidate, modelName, prompt string, concurrency int) []upstreamProbeSample {
	samples := make([]upstreamProbeSample, 0, concurrency)
	results := make(chan upstreamProbeSample, concurrency)
	var waitGroup sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if sample, err := sendUpstreamProbeRequest(ctx, candidate, modelName, prompt); err == nil {
				results <- sample
			}
		}()
	}
	waitGroup.Wait()
	close(results)
	for sample := range results {
		samples = append(samples, sample)
	}
	return samples
}

func sendUpstreamProbeRequest(ctx context.Context, candidate *model.UpstreamCandidate, modelName, prompt string) (upstreamProbeSample, error) {
	payload, endpoint, err := buildUpstreamProbeRequest(candidate, modelName, prompt)
	if err != nil {
		return upstreamProbeSample{}, err
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return upstreamProbeSample{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return upstreamProbeSample{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	setUpstreamAuthentication(request, candidate)
	client := GetHttpClient()
	if candidate.Source == constant.UpstreamSourceSelf {
		client = GetSSRFProtectedHTTPClient()
	}
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return upstreamProbeSample{}, err
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if readErr != nil {
		return upstreamProbeSample{}, readErr
	}
	sample := upstreamProbeSample{StatusCode: response.StatusCode, LatencyMS: time.Since(started).Milliseconds()}
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		var parsed upstreamProbeResponse
		if err := common.Unmarshal(responseBody, &parsed); err == nil {
			if candidate.Type == constant.ChannelTypeGemini {
				sample.PromptTokens = parsed.UsageMetadata.PromptTokenCount
				sample.CompletionTokens = parsed.UsageMetadata.CandidatesTokenCount
			} else if candidate.Type == constant.ChannelTypeAnthropic {
				sample.PromptTokens = parsed.Usage.InputTokens
				sample.CompletionTokens = parsed.Usage.OutputTokens
			} else if candidate.Type == constant.ChannelTypeCohere {
				sample.PromptTokens = parsed.Usage.Tokens.InputTokens
				sample.CompletionTokens = parsed.Usage.Tokens.OutputTokens
			} else {
				sample.PromptTokens = parsed.Usage.PromptTokens
				sample.CompletionTokens = parsed.Usage.CompletionTokens
			}
		}
	}
	return sample, nil
}

func buildUpstreamProbeRequest(candidate *model.UpstreamCandidate, modelName, prompt string) (upstreamProbeRequest, string, error) {
	baseURL := strings.TrimRight(candidate.NormalizedBaseURL, "/")
	if candidate.Type == constant.ChannelTypeGemini {
		endpoint := baseURL + "/v1beta/models/" + url.PathEscape(modelName) + ":generateContent"
		if strings.HasSuffix(baseURL, "/v1beta") {
			endpoint = baseURL + "/models/" + url.PathEscape(modelName) + ":generateContent"
		}
		return upstreamProbeRequest{
			Contents:      []upstreamProbeContent{{Role: "user", Parts: []upstreamProbePart{{Text: prompt}}}},
			GenerationCfg: &upstreamGenerationCfg{MaxOutputTokens: 16},
		}, endpoint, nil
	}
	if candidate.Type == constant.ChannelTypeAnthropic {
		endpoint := baseURL + "/v1/messages"
		if strings.HasSuffix(baseURL, "/v1") {
			endpoint = baseURL + "/messages"
		}
		return upstreamProbeRequest{
			Model: modelName,
			Messages: []map[string]any{{
				"role":    "user",
				"content": []map[string]string{{"type": "text", "text": prompt}},
			}},
			MaxTokens: 16,
		}, endpoint, nil
	}
	if candidate.Type == constant.ChannelTypeDify {
		endpoint := baseURL + "/v1/chat-messages"
		if strings.HasSuffix(baseURL, "/v1") {
			endpoint = baseURL + "/chat-messages"
		}
		return upstreamProbeRequest{
			Query: "ping", ResponseMode: "blocking", User: "upstream-benchmark",
		}, endpoint, nil
	}
	if candidate.Type == constant.ChannelTypeCohere {
		endpoint := baseURL + "/v2/chat"
		return upstreamProbeRequest{
			Model: modelName, Messages: []upstreamProbeMessage{{Role: "user", Content: prompt}}, MaxTokens: 16,
		}, endpoint, nil
	}
	if candidate.Type == constant.ChannelTypeCustom || candidate.Type == constant.ChannelTypeAdvancedCustom {
		if strings.Contains(baseURL, "{model}") {
			baseURL = strings.ReplaceAll(baseURL, "{model}", url.PathEscape(modelName))
		}
		if strings.HasSuffix(baseURL, "/chat/completions") {
			return upstreamProbeRequest{
				Model: modelName, Messages: []upstreamProbeMessage{{Role: "user", Content: prompt}}, MaxTokens: 16,
			}, baseURL, nil
		}
	}
	endpoint := baseURL + "/v1/chat/completions"
	if strings.HasSuffix(baseURL, "/v1") {
		endpoint = baseURL + "/chat/completions"
	}
	if candidate.Type == constant.ChannelTypeAzure {
		endpoint = baseURL + "/openai/deployments/" + url.PathEscape(modelName) + "/chat/completions?api-version=2024-02-15-preview"
	}
	return upstreamProbeRequest{
		Model:     modelName,
		Messages:  []upstreamProbeMessage{{Role: "user", Content: prompt}},
		MaxTokens: 16,
	}, endpoint, nil
}

func aggregateUpstreamProbeSamples(allSamples []upstreamProbeSample, batches map[string][]upstreamProbeSample, throughputSamples []upstreamProbeSample, throughputSeconds float64) UpstreamBenchmarkResult {
	result := UpstreamBenchmarkResult{StatusCode: http.StatusOK, Metrics: make(map[string]float64)}
	if len(allSamples) == 0 {
		return result
	}
	latencies := make([]int64, 0, len(allSamples))
	for _, sample := range allSamples {
		latencies = append(latencies, sample.LatencyMS)
		if sample.StatusCode < 200 || sample.StatusCode >= 300 {
			result.StatusCode = sample.StatusCode
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	result.LatencyMS = latencies[(len(latencies)-1)/2]
	result.Metrics[constant.UpstreamMetricTTFTP50_8K] = percentileLatency(batches[constant.UpstreamMetricTTFTP50_8K], 0.50)
	result.Metrics[constant.UpstreamMetricTTFTP90_8K] = percentileLatency(batches[constant.UpstreamMetricTTFTP50_8K], 0.90)
	result.Metrics[constant.UpstreamMetricTTFTP50_32K] = percentileLatency(batches[constant.UpstreamMetricTTFTP50_32K], 0.50)
	result.Metrics[constant.UpstreamMetricTTFTP90_32K] = percentileLatency(batches[constant.UpstreamMetricTTFTP50_32K], 0.90)
	result.Metrics[constant.UpstreamMetricTTFTP50_128K] = percentileLatency(batches[constant.UpstreamMetricTTFTP50_128K], 0.50)
	result.Metrics[constant.UpstreamMetricTTFTP90_128K] = percentileLatency(batches[constant.UpstreamMetricTTFTP50_128K], 0.90)
	var promptTokens, completionTokens int
	for _, sample := range throughputSamples {
		promptTokens += sample.PromptTokens
		completionTokens += sample.CompletionTokens
	}
	result.Metrics[constant.UpstreamMetricRPM] = float64(len(throughputSamples)) / throughputSeconds * 60
	result.Metrics[constant.UpstreamMetricTPM] = float64(promptTokens+completionTokens) / throughputSeconds * 60
	result.Metrics[constant.UpstreamMetricOTPPSP50] = float64(completionTokens) / throughputSeconds
	return result
}

func percentileLatency(samples []upstreamProbeSample, percentile float64) float64 {
	values := make([]int64, 0, len(samples))
	for _, sample := range samples {
		values = append(values, sample.LatencyMS)
	}
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	index := int(float64(len(values)-1) * percentile)
	return float64(values[index])
}
