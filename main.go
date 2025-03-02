package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"bufio"
	"context"
)

var (
	workingProxies    []string
	proxyMutex        sync.RWMutex
	supportedModels   []string
	modelsMutex       sync.RWMutex
	lastProxyUpdate   time.Time
	lastModelsUpdate  time.Time
)

type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type WhisperRequest struct {
	File     multipart.File
	Task     string
	Language string
}

type OpenAIError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

type ModelResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	} `json:"data"`
}

const (
	deepInfraBaseURL = "https://api.deepinfra.com/v1/openai"
	chatEndpoint     = "/chat/completions"
	whisperEndpoint  = "/v1/inference/openai/whisper-large-v3"
	modelsEndpoint   = "/models"
	proxyListURL     = "https://api.proxyscrape.com/v3/free-proxy-list/get?request=displayproxies&protocol=http&proxy_format=protocolipport&format=text&anonymity=Elite,Anonymous&timeout=5015"
	proxyUpdateTime  = 10 * time.Minute
	modelsUpdateTime = 60 * time.Minute
	maxProxyAttempts = 30
	maxRetries = 3
)

func main() {
	go manageProxiesAndModels()
	
	http.HandleFunc("/v1/chat/completions", chatCompletionsHandler)
	http.HandleFunc("/v1/audio/transcriptions", whisperHandler)
	http.HandleFunc("/models", modelsHandler)
	
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("🚀 Server starting on port %s\n", port)
	http.ListenAndServe(":"+port, nil)
}

func manageProxiesAndModels() {
	updateWorkingProxies()
	updateSupportedModels()
	
	proxyTicker := time.NewTicker(proxyUpdateTime)
	modelsTicker := time.NewTicker(modelsUpdateTime)
	
	for {
		select {
		case <-proxyTicker.C:
			fmt.Println("⏰ Scheduled proxy update")
			updateWorkingProxies()
		case <-modelsTicker.C:
			fmt.Println("⏰ Scheduled models update")
			updateSupportedModels()
		}
	}
}

func modelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	modelsMutex.RLock()
	models := make([]string, len(supportedModels))
	copy(models, supportedModels)
	modelsMutex.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(models)
}

func chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("📨 Received chat completion request: %s %s\n", r.Method, r.URL.Path)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var chatReq ChatCompletionRequest
	err := json.NewDecoder(r.Body).Decode(&chatReq)
	if err != nil {
		sendErrorResponse(w, "Failed to parse request body", "invalid_request_error", http.StatusBadRequest)
		return
	}

	if !isModelSupported(chatReq.Model) {
		sendErrorResponse(w, "Unsupported model. Please use one of the supported models.", "invalid_request_error", http.StatusBadRequest, "model_not_found")
		return
	}

	if chatReq.Temperature == 0 {
		chatReq.Temperature = 0.7
	}
	if chatReq.MaxTokens == 0 {
		chatReq.MaxTokens = 15000
	}

	for i := range chatReq.Messages {
		if chatReq.Messages[i].Role == "content" && chatReq.Messages[i].Content == "user" {
			chatReq.Messages[i].Role, chatReq.Messages[i].Content = chatReq.Messages[i].Content, chatReq.Messages[i].Role
		}
	}

	data, err := json.Marshal(chatReq)
	if err != nil {
		sendErrorResponse(w, "Failed to marshal request", "internal_error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	success := false
	var lastErr error
	usedProxies := make(map[string]bool)

	for i := 0; i < maxProxyAttempts && !success; i++ {
		select {
		case <-ctx.Done():
			sendErrorResponse(w, "Request timeout", "timeout", http.StatusGatewayTimeout)
			return
		default:
			proxy := getWorkingProxy()
			if proxy == "" {
				if i > 0 {
					time.Sleep(500 * time.Millisecond)
				}
				continue
			}
			usedProxies[proxy] = true

			result, err := sendChatRequest(ctx, proxy, deepInfraBaseURL+chatEndpoint, data, chatReq.Stream, w)
			if err != nil {
				lastErr = err
				removeProxy(proxy)
				continue
			}
			
			if result {
				success = true
				break
			}
		}
	}

	if !success {
		errMsg := "Unable to process the request after multiple attempts"
		if lastErr != nil {
			errMsg = "Error: " + lastErr.Error()
		}
		sendErrorResponse(w, errMsg, "internal_error", http.StatusInternalServerError)
	}
}

func sendChatRequest(ctx context.Context, proxy, endpoint string, data []byte, isStream bool, w http.ResponseWriter) (bool, error) {
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return false, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 60 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(data))
	if err != nil {
		return false, err
	}
	
	req.Header = getHeaders()
	fmt.Printf("🔗 Sending request to %s using proxy %s\n", endpoint, proxy)
	
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		if isStream {
			return handleStreamResponse(w, resp)
		} else {
			return handleNormalResponse(w, resp)
		}
	}

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("❌ Error response from Deepinfra API: %s\n", string(body))
	return false, fmt.Errorf("API error: %s", string(body))
}

func handleStreamResponse(w http.ResponseWriter, resp *http.Response) (bool, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			if strings.HasPrefix(line, "data: ") {
				fmt.Fprintf(w, "%s\n\n", line)
			} else {
				fmt.Fprintf(w, "data: %s\n\n", line)
			}
			w.(http.Flusher).Flush()
		}
	}
	
	if err := scanner.Err(); err != nil {
		return false, err
	}
	
	return true, nil
}

func handleNormalResponse(w http.ResponseWriter, resp *http.Response) (bool, error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := io.Copy(w, resp.Body)
	return err == nil, err
}

func whisperHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("📨 Received whisper request: %s %s\n", r.Method, r.URL.Path)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		sendErrorResponse(w, "Failed to parse form", "invalid_request_error", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		sendErrorResponse(w, "Failed to get file from form", "invalid_request_error", http.StatusBadRequest)
		return
	}
	defer file.Close()

	task := r.FormValue("task")
	if task == "" {
		task = "transcribe"
	}

	language := r.FormValue("language")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	success := false
	for i := 0; i < maxRetries && !success; i++ {
		select {
		case <-ctx.Done():
			sendErrorResponse(w, "Request timeout", "timeout", http.StatusGatewayTimeout)
			return
		default:
			proxy := getWorkingProxy()
			if proxy == "" {
				time.Sleep(time.Second)
				continue
			}

			fmt.Printf("🔗 Sending whisper request using proxy %s\n", proxy)
			resp, err := sendWhisperRequest(ctx, proxy, file, task, language)
			if err != nil {
				removeProxy(proxy)
				time.Sleep(time.Second)
				continue
			}

			file.Seek(0, 0)

			if resp.StatusCode == http.StatusOK {
				for key, values := range resp.Header {
					for _, value := range values {
						w.Header().Add(key, value)
					}
				}
				w.WriteHeader(resp.StatusCode)
				io.Copy(w, resp.Body)
				resp.Body.Close()
				success = true
				break
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			removeProxy(proxy)
			fmt.Printf("❌ Error response from Deepinfra Whisper API: %s\n", string(body))
			time.Sleep(time.Second)
		}
	}

	if !success {
		sendErrorResponse(w, "Unable to process the whisper request after multiple attempts", "internal_error", http.StatusInternalServerError)
	}
}

func sendWhisperRequest(ctx context.Context, proxyStr string, fileData multipart.File, task, language string) (*http.Response, error) {
	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 5 * time.Minute,
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("audio", "audio.wav")
	if err != nil {
		return nil, err
	}
	
	_, err = io.Copy(part, fileData)
	if err != nil {
		return nil, err
	}

	writer.WriteField("task", task)
	if language != "" {
		writer.WriteField("language", language)
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", deepInfraBaseURL+whisperEndpoint, body)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	httpReq.Header.Set("X-Deepinfra-Source", "web-page")
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36")

	return client.Do(httpReq)
}

func updateWorkingProxies() {
	proxies, err := getProxyList()
	if err != nil {
		fmt.Printf("❌ Error fetching proxy list: %v\n", err)
		return
	}

	fmt.Printf("🔍 Testing %d proxies...\n", len(proxies))
	var wg sync.WaitGroup
	results := make(chan string, len(proxies))
	semaphore := make(chan struct{}, 50)

	for _, proxy := range proxies {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			
			if checkProxy(p) {
				results <- p
			}
		}(proxy)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	newProxies := make([]string, 0, len(proxies)/10)
	for proxy := range results {
		newProxies = append(newProxies, proxy)
	}

	proxyMutex.Lock()
	workingProxies = newProxies
	lastProxyUpdate = time.Now()
	proxyMutex.Unlock()

	fmt.Printf("✅ Found %d working proxies\n", len(newProxies))
}

func updateSupportedModels() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	newModels, err := fetchSupportedModels(ctx)
	if err != nil {
		fmt.Printf("❌ Error fetching models list: %v\n", err)
		return
	}

	if len(newModels) > 0 {
		modelsMutex.Lock()
		supportedModels = newModels
		lastModelsUpdate = time.Now()
		modelsMutex.Unlock()
		fmt.Printf("📋 Updated supported models list with %d models\n", len(newModels))
	} else {
		fmt.Printf("⚠️ No models were found, keeping existing list\n")
	}
}

func fetchSupportedModels(ctx context.Context) ([]string, error) {
	allModels, err := fetchAllModels(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Printf("🔍 Found %d models, testing which ones are accessible...\n", len(allModels))
	
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
				results <- m
				fmt.Printf("✅ Model accessible: %s\n", m)
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
	
	return accessibleModels, nil
}

func fetchAllModels(ctx context.Context) ([]string, error) {
	proxy := getWorkingProxy()
	if proxy == "" {
		return nil, fmt.Errorf("no working proxy available")
	}
	
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
	
	req, err := http.NewRequestWithContext(ctx, "GET", deepInfraBaseURL+modelsEndpoint, nil)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Deepinfra-Source", "web-page")
	
	resp, err := client.Do(req)
	if err != nil {
		removeProxy(proxy)
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		removeProxy(proxy)
		return nil, fmt.Errorf("failed to get models list: status %d", resp.StatusCode)
	}
	
	var modelResp ModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelResp); err != nil {
		return nil, err
	}
	
	var models []string
	for _, model := range modelResp.Data {
		models = append(models, model.ID)
	}
	
	return models, nil
}

func isModelAccessible(ctx context.Context, model string) bool {
	proxy := getWorkingProxy()
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
	
	chatReq := ChatCompletionRequest{
		Model: model,
		Messages: []ChatMessage{
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
	
	req, err := http.NewRequestWithContext(ctx, "POST", deepInfraBaseURL+chatEndpoint, bytes.NewBuffer(data))
	if err != nil {
		return false
	}
	
	req.Header = getHeaders()
	
	resp, err := client.Do(req)
	if err != nil {
		removeProxy(proxy)
		return false
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	
	if strings.Contains(string(body), "Not authenticated") {
		return false
	}
	
	return resp.StatusCode == http.StatusOK
}

func getWorkingProxy() string {
	proxyMutex.RLock()
	if len(workingProxies) > 0 {
		proxy := workingProxies[0]
		proxyMutex.RUnlock()
		return proxy
	}
	proxyMutex.RUnlock()

	if time.Since(lastProxyUpdate) > 2*time.Minute {
		updateWorkingProxies()
	}

	proxyMutex.RLock()
	defer proxyMutex.RUnlock()
	if len(workingProxies) > 0 {
		return workingProxies[0]
	}
	return ""
}

func removeProxy(proxy string) {
	proxyMutex.Lock()
	defer proxyMutex.Unlock()
	for i, p := range workingProxies {
		if p == proxy {
			workingProxies[i] = workingProxies[len(workingProxies)-1]
			workingProxies = workingProxies[:len(workingProxies)-1]
			break
		}
	}
}

func getProxyList() ([]string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	
	resp, err := client.Get(proxyListURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy list: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read proxy list: %v", err)
	}

	proxies := strings.Fields(string(body))
	if len(proxies) == 0 {
		return nil, fmt.Errorf("empty proxy list received")
	}
	
	return proxies, nil
}

func checkProxy(proxy string) bool {
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		return false
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(deepInfraBaseURL + modelsEndpoint)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func getHeaders() http.Header {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Deepinfra-Source", "web-page")
	headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36")
	return headers
}

func isModelSupported(model string) bool {
	modelsMutex.RLock()
	defer modelsMutex.RUnlock()
	
	if len(supportedModels) == 0 && time.Since(lastModelsUpdate) > 5*time.Second {
		modelsMutex.RUnlock()
		updateSupportedModels()
		modelsMutex.RLock()
	}
	
	for _, supportedModel := range supportedModels {
		if model == supportedModel {
			return true
		}
	}
	return false
}

func sendErrorResponse(w http.ResponseWriter, message, errorType string, statusCode int, errorCode ...string) {
	code := errorType
	if len(errorCode) > 0 {
		code = errorCode[0]
	}
	
	errorResponse := OpenAIError{
		Error: struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		}{
			Message: message,
			Type:    errorType,
			Code:    code,
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(errorResponse)
}