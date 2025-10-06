package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"deepinfra-wrapper/types"
)

var (
	supportedModels  []string
	modelMetadata    map[string]ModelInfo
	modelsMutex      sync.RWMutex
	lastModelsUpdate time.Time
	apiKey           string
)

// ModelInfo contains additional metadata about models
type ModelInfo struct {
	ID          string    `json:"id"`
	Object      string    `json:"object"`
	Created     int64     `json:"created"`
	OwnedBy     string    `json:"owned_by"`
	Description string    `json:"description,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Type        string    `json:"type,omitempty"`
	Pricing     *Pricing  `json:"pricing,omitempty"`
}

type Pricing struct {
	InputCost  float64 `json:"input_cost,omitempty"`
	OutputCost float64 `json:"output_cost,omitempty"`
	Unit       string  `json:"unit,omitempty"`
}

func InitAPIKey(key string) {
	apiKey = key
	modelMetadata = make(map[string]ModelInfo)
}

func GetAPIKey() string {
	return apiKey
}

func IsAuthEnabled() bool {
    return apiKey != ""
}

func GetModelCount() int {
	modelsMutex.RLock()
	defer modelsMutex.RUnlock()
	return len(supportedModels)
}

func GetSupportedModels() []string {
	modelsMutex.RLock()
	defer modelsMutex.RUnlock()
	
	models := make([]string, len(supportedModels))
	copy(models, supportedModels)
	return models
}

// GetModelInfo returns detailed information about a specific model
func GetModelInfo(modelID string) (ModelInfo, bool) {
	modelsMutex.RLock()
	defer modelsMutex.RUnlock()
	
	info, exists := modelMetadata[modelID]
	return info, exists
}

// GetAllModelInfo returns detailed information about all models
func GetAllModelInfo() []ModelInfo {
	modelsMutex.RLock()
	defer modelsMutex.RUnlock()
	
	var models []ModelInfo
	for _, modelName := range supportedModels {
		if info, exists := modelMetadata[modelName]; exists {
			models = append(models, info)
		} else {
			// Fallback to basic info if detailed metadata is not available
			models = append(models, ModelInfo{
				ID:      modelName,
				Object:  "model",
				Created: time.Now().Unix(),
				OwnedBy: "deepinfra",
			})
		}
	}
	return models
}

func UpdateSupportedModels() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Println("🧩 Fetching all available models...")
	newModels, modelInfo, err := fetchSupportedModels(ctx)
	if err != nil {
		fmt.Printf("❌ Error fetching supported models: %v\n", err)
		return
	}

	if len(newModels) > 0 {
		modelsMutex.Lock()
		supportedModels = newModels
		// Update model metadata
		for id, info := range modelInfo {
			modelMetadata[id] = info
		}
		lastModelsUpdate = time.Now()
		modelsMutex.Unlock()
	}
}

func fetchSupportedModels(ctx context.Context) ([]string, map[string]ModelInfo, error) {
	allModels, modelInfo, err := fetchAllModels(ctx)
	if err != nil {
		return nil, nil, err
	}

	if len(allModels) == 0 {
		return nil, nil, fmt.Errorf("no models received from API")
	}

	fmt.Printf("🔍 Testing accessibility for %d models...\n", len(allModels))

	var accessibleModels []string
	for i, model := range allModels {
		fmt.Printf("Checking model %d/%d: %s\n", i+1, len(allModels), model)
		if isModelAccessible(ctx, model) {
			fmt.Printf("✅ Model accessible: %s\n", model)
			accessibleModels = append(accessibleModels, model)
		} else {
			fmt.Printf("❌ Model not accessible: %s\n", model)
		}
		// Add a 1.5-second delay between checks
		time.Sleep(1500 * time.Millisecond)
	}

	fmt.Printf("✅ Found %d accessible models out of %d total\n", len(accessibleModels), len(allModels))
	return accessibleModels, modelInfo, nil
}

func fetchAllModels(ctx context.Context) ([]string, map[string]ModelInfo, error) {
	var models []string
	var lastError error
	modelInfo := make(map[string]ModelInfo)

	for attempts := 0; attempts < MaxRetries; attempts++ {
		fmt.Println("🌐 Fetching model list directly...")

		client := &http.Client{
			Timeout: 30 * time.Second,
		}

		req, err := http.NewRequestWithContext(ctx, "GET", DeepInfraBaseURL+ModelsEndpoint, nil)
		if err != nil {
			lastError = err
			time.Sleep(1 * time.Second)
			continue
		}

		req.Header = getHeaders()

		resp, err := client.Do(req)
		if err != nil {
			lastError = err
			time.Sleep(1 * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastError = fmt.Errorf("failed to get models list: status %d", resp.StatusCode)
			time.Sleep(1 * time.Second)
			continue
		}

		var modelResp types.ModelResponse
		err = json.NewDecoder(resp.Body).Decode(&modelResp)
		resp.Body.Close()

		if err != nil {
			lastError = err
			time.Sleep(1 * time.Second)
			continue
		}

		currentTime := time.Now().Unix()
		for _, model := range modelResp.Data {
			models = append(models, model.ID)

			// Create enhanced model info
			modelInfo[model.ID] = ModelInfo{
				ID:      model.ID,
				Object:  "model",
				Created: currentTime,
				OwnedBy: "deepinfra",
				Type:    inferModelType(model.ID),
			}
		}

		fmt.Printf("📋 Retrieved %d models from API\n", len(models))
		return models, modelInfo, nil
	}

	if lastError != nil {
		return nil, nil, fmt.Errorf("failed to fetch models after %d attempts: %v", MaxRetries, lastError)
	}

	return nil, nil, fmt.Errorf("failed to fetch models after %d attempts", MaxRetries)
}

// inferModelType attempts to categorize models based on their names
func inferModelType(modelID string) string {
	modelLower := strings.ToLower(modelID)
	
	if strings.Contains(modelLower, "whisper") {
		return "audio"
	}
	if strings.Contains(modelLower, "stable-diffusion") || strings.Contains(modelLower, "sdxl") || strings.Contains(modelLower, "dalle") {
		return "image"
	}
	if strings.Contains(modelLower, "embedding") {
		return "embedding"
	}
	if strings.Contains(modelLower, "llama") || strings.Contains(modelLower, "gpt") || strings.Contains(modelLower, "claude") || 
	   strings.Contains(modelLower, "mistral") || strings.Contains(modelLower, "deepseek") || strings.Contains(modelLower, "qwen") {
		return "text"
	}
	
	// Default to text for most models
	return "text"
}

func isModelAccessible(ctx context.Context, model string) bool {
	for attempts := 0; attempts < 2; attempts++ {
		client := &http.Client{
			Timeout: 20 * time.Second,
		}

		chatReq := types.ChatCompletionRequest{
			Model: model,
			Messages: []types.ChatMessage{
				{
					Role:    "user",
					Content: "Hello",
				},
			},
			MaxTokens: 10,
		}

		data, err := json.Marshal(chatReq)
		if err != nil {
			continue
		}

		req, err := http.NewRequestWithContext(ctx, "POST", DeepInfraBaseURL+ChatEndpoint, bytes.NewBuffer(data))
		if err != nil {
			continue
		}

		req.Header = getHeaders()

		resp, err := client.Do(req)
		if err != nil {
			// Don't remove proxy, just log error and retry
			fmt.Printf("Error checking model accessibility for %s: %v\n", model, err)
			time.Sleep(1 * time.Second)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if strings.Contains(string(body), "Not authenticated") {
			return false
		}

		return resp.StatusCode == http.StatusOK
	}

	return false
}

func IsModelSupported(model string) bool {
	modelsMutex.RLock()
	
	if len(supportedModels) == 0 && time.Since(lastModelsUpdate) > 5*time.Second {
		modelsMutex.RUnlock()
		
		go func() {
			UpdateSupportedModels()
		}()
		
		return true
	}
	
	for _, supportedModel := range supportedModels {
		if model == supportedModel {
			modelsMutex.RUnlock()
			return true
		}
	}
	
	modelsMutex.RUnlock()
	return false
}

func getHeaders() http.Header {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Deepinfra-Source", "web-page")
	headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36")
	return headers
}