package taskcommon

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const seedanceTaskIDPlaceholder = "{task_id}"

const SeedanceTaskGetRouteBodyKey = "seedance_v3_task_get_route"

// NormalizeSeedanceV3Route resolves a configured relative target against the
// channel base URL while preserving placeholders for an asynchronous task.
func NormalizeSeedanceV3Route(baseURL string, route *dto.SeedanceV3Route, defaultMethod string) (*dto.SeedanceV3Route, error) {
	if !route.IsConfigured() {
		return nil, nil
	}
	method := strings.ToUpper(strings.TrimSpace(route.Method))
	if method == "" {
		method = defaultMethod
	}
	if !dto.IsSeedanceV3RouteMethodAllowed(method) {
		return nil, fmt.Errorf("invalid Seedance upstream method: %s", method)
	}
	target := strings.TrimSpace(route.Target)
	if strings.HasPrefix(target, "/") {
		baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
		if baseURL == "" {
			return nil, fmt.Errorf("channel base URL is empty")
		}
		target = baseURL + target
	}
	parsed, err := url.Parse(strings.ReplaceAll(target, seedanceTaskIDPlaceholder, "task-id"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid Seedance upstream target: %s", target)
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return nil, fmt.Errorf("Seedance upstream target must use http or https")
	}
	return &dto.SeedanceV3Route{
		Method:          method,
		Target:          target,
		Parameters:      route.Parameters,
		ResponseMapping: route.ResponseMapping,
	}, nil
}

func ResolveSeedanceV3TaskGetRoute(baseURL string, route *dto.SeedanceV3Route, taskID string) (*dto.SeedanceV3Route, error) {
	normalized, err := NormalizeSeedanceV3Route(baseURL, route, http.MethodGet)
	if err != nil || normalized == nil {
		return normalized, err
	}
	normalized.Target = strings.ReplaceAll(normalized.Target, seedanceTaskIDPlaceholder, url.PathEscape(taskID))
	return normalized, nil
}

// BuildSeedanceV3TaskGetRequest supports path-style GET endpoints and
// body-style non-GET endpoints without changing any adaptor's default path.
func BuildSeedanceV3TaskGetRequest(baseURL string, route *dto.SeedanceV3Route, taskID, key string) (*http.Request, error) {
	resolved, err := ResolveSeedanceV3TaskGetRoute(baseURL, route, taskID)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return nil, fmt.Errorf("Seedance task query route is not configured")
	}
	parameters := replaceSeedanceV3TaskID(route.Parameters, taskID)
	if resolved.Method == http.MethodGet {
		resolved.Target, err = applySeedanceV3QueryParameters(resolved.Target, parameters)
		if err != nil {
			return nil, err
		}
	}

	bodyBytes := []byte(nil)
	if resolved.Method != http.MethodGet {
		baseParameters := map[string]any{}
		if !strings.Contains(route.Target, seedanceTaskIDPlaceholder) {
			baseParameters["task_id"] = taskID
		}
		mergeSeedanceV3Parameters(baseParameters, parameters)
		bodyBytes, err = common.Marshal(baseParameters)
		if err != nil {
			return nil, err
		}
	}
	request, err := http.NewRequest(resolved.Method, resolved.Target, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	if resolved.Method != http.MethodGet {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, nil
}

// ApplySeedanceV3RouteParameters applies a JSON merge patch to the adaptor's
// existing request body. An empty parameters object keeps the current payload
// unchanged, while null removes a field.
func ApplySeedanceV3RouteParameters(requestBody io.Reader, route *dto.SeedanceV3Route) (io.Reader, error) {
	if route == nil || len(route.Parameters) == 0 {
		return requestBody, nil
	}
	bodyBytes, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}
	var current map[string]any
	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		current = map[string]any{}
	} else if err := common.Unmarshal(bodyBytes, &current); err != nil {
		return nil, fmt.Errorf("invalid Seedance request parameters: %w", err)
	}
	parameters, err := resolveSeedanceV3ParameterMappings(route.Parameters, current)
	if err != nil {
		return nil, err
	}
	mergeSeedanceV3Parameters(current, parameters)
	merged, err := common.Marshal(current)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(merged), nil
}

// ApplySeedanceV3RouteResponseMapping transforms an upstream JSON response
// before the provider adaptor parses it. An empty mapping preserves the exact
// response bytes, while null removes a field.
func ApplySeedanceV3RouteResponseMapping(responseBody []byte, route *dto.SeedanceV3Route) ([]byte, error) {
	if route == nil || len(route.ResponseMapping) == 0 {
		return responseBody, nil
	}
	var current map[string]any
	if err := common.Unmarshal(responseBody, &current); err != nil {
		return nil, fmt.Errorf("invalid Seedance upstream response: %w", err)
	}
	if current == nil {
		return nil, fmt.Errorf("invalid Seedance upstream response: expected a JSON object")
	}
	mapping, err := resolveSeedanceV3ParameterMappings(route.ResponseMapping, current)
	if err != nil {
		return nil, err
	}
	mergeSeedanceV3Parameters(current, mapping)
	return common.Marshal(current)
}

// ApplySeedanceV3RouteHTTPResponseMapping preserves the upstream response
// status and headers while replacing its body with the configured mapping.
func ApplySeedanceV3RouteHTTPResponseMapping(response *http.Response, route *dto.SeedanceV3Route) error {
	if response == nil || route == nil || len(route.ResponseMapping) == 0 {
		return nil
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	_ = response.Body.Close()
	mappedBody, err := ApplySeedanceV3RouteResponseMapping(responseBody, route)
	if err != nil {
		return err
	}
	response.Body = io.NopCloser(bytes.NewReader(mappedBody))
	response.ContentLength = int64(len(mappedBody))
	response.Header.Del("Content-Length")
	return nil
}

func resolveSeedanceV3ParameterMappings(parameters, source map[string]any) (map[string]any, error) {
	resolved := make(map[string]any, len(parameters))
	for key, value := range parameters {
		mapped, err := resolveSeedanceV3ParameterMappingValue(value, source)
		if err != nil {
			return nil, err
		}
		resolved[key] = mapped
	}
	return resolved, nil
}

func resolveSeedanceV3ParameterMappingValue(value any, source map[string]any) (any, error) {
	switch typed := value.(type) {
	case string:
		path, mapped := seedanceV3ParameterMappingPath(typed)
		if !mapped {
			return typed, nil
		}
		mappedValue, ok := seedanceV3ParameterSourceValue(source, path)
		if !ok {
			return nil, fmt.Errorf("Seedance mapping source not found: %s", path)
		}
		return mappedValue, nil
	case map[string]any:
		return resolveSeedanceV3ParameterMappings(typed, source)
	case []any:
		resolved := make([]any, len(typed))
		for index, item := range typed {
			mapped, err := resolveSeedanceV3ParameterMappingValue(item, source)
			if err != nil {
				return nil, err
			}
			resolved[index] = mapped
		}
		return resolved, nil
	default:
		return value, nil
	}
}

func seedanceV3ParameterMappingPath(value string) (string, bool) {
	if len(value) < 3 || value[0] != '{' || value[len(value)-1] != '}' {
		return "", false
	}
	path := value[1 : len(value)-1]
	if path == "" || strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") {
		return "", false
	}
	for _, char := range path {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' {
			continue
		}
		return "", false
	}
	return path, true
}

func seedanceV3ParameterSourceValue(source map[string]any, path string) (any, bool) {
	var current any = source
	for _, segment := range strings.Split(path, ".") {
		switch typed := current.(type) {
		case map[string]any:
			value, ok := typed[segment]
			if !ok {
				return nil, false
			}
			current = value
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func applySeedanceV3QueryParameters(target string, parameters map[string]any) (string, error) {
	if len(parameters) == 0 {
		return target, nil
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for key, value := range parameters {
		query.Del(key)
		if value == nil {
			continue
		}
		values, ok := value.([]any)
		if !ok {
			values = []any{value}
		}
		for _, item := range values {
			encoded, err := seedanceV3QueryValue(item)
			if err != nil {
				return "", err
			}
			query.Add(key, encoded)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func seedanceV3QueryValue(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil
	case float64:
		return fmt.Sprint(typed), nil
	default:
		encoded, err := common.Marshal(value)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	}
}

func mergeSeedanceV3Parameters(current, patch map[string]any) {
	for key, patchValue := range patch {
		if patchValue == nil {
			delete(current, key)
			continue
		}
		patchObject, patchIsObject := patchValue.(map[string]any)
		currentObject, currentIsObject := current[key].(map[string]any)
		if patchIsObject {
			if !currentIsObject {
				currentObject = map[string]any{}
			}
			mergeSeedanceV3Parameters(currentObject, patchObject)
			current[key] = currentObject
			continue
		}
		current[key] = patchValue
	}
}

func replaceSeedanceV3TaskID(parameters map[string]any, taskID string) map[string]any {
	if len(parameters) == 0 {
		return nil
	}
	replaced := make(map[string]any, len(parameters))
	for key, value := range parameters {
		replaced[key] = replaceSeedanceV3TaskIDValue(value, taskID)
	}
	return replaced
}

func replaceSeedanceV3TaskIDValue(value any, taskID string) any {
	switch typed := value.(type) {
	case string:
		return strings.ReplaceAll(typed, seedanceTaskIDPlaceholder, taskID)
	case map[string]any:
		return replaceSeedanceV3TaskID(typed, taskID)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = replaceSeedanceV3TaskIDValue(item, taskID)
		}
		return result
	default:
		return value
	}
}
