package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"deepinfra-wrapper/types"
)

var (
	supportedModels  []string
	modelsMutex      sync.RWMutex
	lastModelsUpdate time.Time
	apiKey           string
)

func InitAPIKey(key string) {
	apiKey = key
}

func GetAPIKey() string {
	return apiKey
}

func GetModelCount() int {
	modelsMutex.RLock()
	defer modelsMutex.RUnlock()
	return len(supportedModels)
}

func GetSupportedModels() []string {
	modelsMutex.RLock()
	models := make([]string, len(supportedModels))
	copy(models, supportedModels)
	modelsMutex.RUnlock()
	return models
}

func UpdateSupportedModels() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Println("🧩 Fetching all available models...")
	newModels, err := fetchSupportedModels(ctx)
	if err != nil {
		fmt.Printf("❌ Error fetching supported models: %v\n", err)
		return
	}

	if len(newModels) > 0 {
		modelsMutex.Lock()
		supportedModels = newModels
		lastModelsUpdate = time.Now()
		modelsMutex.Unlock()
	}
}

func fetchSupportedModels(ctx context.Context) ([]string, error) {
	allModels, err := fetchAllModels(ctx)
	if err != nil {
		return nil, err
	}
	
	fmt.Printf("🔍 Testing accessibility for %d models...\n", len(allModels))
	
	var wg sync.WaitGroup
	results := make(chan string, len(allModels))
	semaphore := make(chan struct{}, 10)
	
	for _, model := range allModels {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			
			if isModelAccessible(ctx, m) {
				fmt.Printf("✅ Model accessible: %s\n", m)
				results <- m
			} else {
				fmt.Printf("❌ Model not accessible: %s\n", m)
			}
		}(model)
	}
	
	go func() {
		wg.Wait()
		close(results)
	}()
	
	var accessibleModels []string
	for model := range results {
		accessibleModels = append(accessibleModels, model)
	}
	
	fmt.Printf("✅ Found %d accessible models out of %d total\n", len(accessibleModels), len(allModels))
	return accessibleModels, nil
}

func fetchAllModels(ctx context.Context) ([]string, error) {
	proxy := GetWorkingProxy()
	if proxy == "" {
		return nil, fmt.Errorf("no working proxy available")
	}
	
	fmt.Printf("🌐 Fetching model list using proxy: %s\n", proxy)
	
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return nil, err
	}
	
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 30 * time.Second,
	}
	
	req, err := http.NewRequestWithContext(ctx, "GET", DeepInfraBaseURL+ModelsEndpoint, nil)
	if err != nil {
		return nil, err
	}
	
	req.Header = getHeaders()
	
	resp, err := client.Do(req)
	if err != nil {
		RemoveProxy(proxy)
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		RemoveProxy(proxy)
		return nil, fmt.Errorf("failed to get models list: status %d", resp.StatusCode)
	}
	
	var modelResp types.ModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelResp); err != nil {
		return nil, err
	}
	
	var models []string
	for _, model := range modelResp.Data {
		models = append(models, model.ID)
	}
	
	fmt.Printf("📋 Retrieved %d models from API\n", len(models))
	return models, nil
}

func isModelAccessible(ctx context.Context, model string) bool {
	proxy := GetWorkingProxy()
	if proxy == "" {
		return false
	}
	
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return false
	}
	
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
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
	}
	
	data, err := json.Marshal(chatReq)
	if err != nil {
		return false
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", DeepInfraBaseURL+ChatEndpoint, bytes.NewBuffer(data))
	if err != nil {
		return false
	}
	
	req.Header = getHeaders()
	
	resp, err := client.Do(req)
	if err != nil {
		RemoveProxy(proxy)
		return false
	}
	defer resp.Body.Close()
	
	body, _ := json.ReadAll(resp.Body)
	
	if strings.Contains(string(body), "Not authenticated") {
		return false
	}
	
	return resp.StatusCode == http.StatusOK
}

func IsModelSupported(model string) bool {
	modelsMutex.RLock()
	defer modelsMutex.RUnlock()
	
	if len(supportedModels) == 0 && time.Since(lastModelsUpdate) > 5*time.Second {
		modelsMutex.RUnlock()
		UpdateSupportedModels()
		modelsMutex.RLock()
	}
	
	for _, supportedModel := range supportedModels {
		if model == supportedModel {
			return true
		}
	}
	return false
}

func getHeaders() http.Header {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Deepinfra-Source", "web-page")
	headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36")
	return headers
}