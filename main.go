package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"bufio"
	"html/template"
)

var (
	workingProxies    []string
	proxyMutex        sync.RWMutex
	supportedModels   []string
	modelsMutex       sync.RWMutex
	lastProxyUpdate   time.Time
	lastModelsUpdate  time.Time
	apiKey            string
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
	modelsEndpoint   = "/models"
	proxyListURL     = "https://api.proxyscrape.com/v3/free-proxy-list/get?request=displayproxies&protocol=http&proxy_format=protocolipport&format=text&anonymity=Elite,Anonymous&timeout=5015"
	proxyUpdateTime  = 10 * time.Minute
	modelsUpdateTime = 60 * time.Minute
	maxProxyAttempts = 30
	maxRetries = 3
)

func main() {
	fmt.Println("Starting service initialization...")
	
	apiKey = os.Getenv("API_KEY")
	if apiKey == "" {
		fmt.Println("Warning: API_KEY environment variable not set. Authentication will be disabled.")
	} else {
		fmt.Println("API key authentication enabled")
	}
	
	initReady := make(chan bool)
	go initializeService(initReady)
	
	<-initReady
	
	http.HandleFunc("/v1/chat/completions", authMiddleware(chatCompletionsHandler))
	http.HandleFunc("/models", modelsHandler)
	http.HandleFunc("/docs", swaggerHandler)
	http.HandleFunc("/openapi.json", openAPIHandler)
	
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("Server started on port %s\n", port)
	http.ListenAndServe(":"+port, nil)
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" {
			next(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			sendErrorResponse(w, "Missing API key", "invalid_request_error", http.StatusUnauthorized, "invalid_api_key")
			return
		}

		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(auth, bearerPrefix) {
			sendErrorResponse(w, "Invalid API key format", "invalid_request_error", http.StatusUnauthorized, "invalid_api_key")
			return
		}

		providedKey := strings.TrimPrefix(auth, bearerPrefix)
		if providedKey != apiKey {
			sendErrorResponse(w, "Invalid API key", "invalid_request_error", http.StatusUnauthorized, "invalid_api_key")
			return
		}

		next(w, r)
	}
}

func initializeService(ready chan<- bool) {
	fmt.Println("Initializing proxies...")
	updateWorkingProxies()
	
	if len(workingProxies) == 0 {
		fmt.Println("No working proxies found. Retrying...")
		updateWorkingProxies()
	}
	
	fmt.Printf("Found %d working proxies\n", len(workingProxies))
	
	fmt.Println("Initializing supported models...")
	updateSupportedModels()
	
	if len(supportedModels) == 0 {
		fmt.Println("No supported models found. Retrying...")
		updateSupportedModels()
	}
	
	fmt.Printf("Found %d supported models\n", len(supportedModels))
	
	go manageProxiesAndModels()
	
	ready <- true
	
	fmt.Println("Service is ready to use")
}

func manageProxiesAndModels() {
	proxyTicker := time.NewTicker(proxyUpdateTime)
	modelsTicker := time.NewTicker(modelsUpdateTime)
	
	for {
		select {
		case <-proxyTicker.C:
			updateWorkingProxies()
		case <-modelsTicker.C:
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

func updateWorkingProxies() {
	proxies, err := getProxyList()
	if err != nil {
		return
	}

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
}

func updateSupportedModels() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	newModels, err := fetchSupportedModels(ctx)
	if err != nil {
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

func swaggerHandler(w http.ResponseWriter, r *http.Request) {
	const swaggerTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>DeepInfra OpenAI API Proxy - Swagger UI</title>
  <link rel="stylesheet" type="text/css" href="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/4.18.3/swagger-ui.css" />
  <style>
    html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin: 0; background: #fafafa; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/4.18.3/swagger-ui-bundle.js" charset="UTF-8"></script>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/4.18.3/swagger-ui-standalone-preset.js" charset="UTF-8"></script>
  <script>
    window.onload = function() {
      const ui = SwaggerUIBundle({
        url: "/openapi.json",
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        layout: "StandaloneLayout"
      });
      window.ui = ui;
    };
  </script>
</body>
</html>`

	tmpl, err := template.New("swagger").Parse(swaggerTemplate)
	if err != nil {
		http.Error(w, "Error generating Swagger UI", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.Execute(w, nil)
}

func openAPIHandler(w http.ResponseWriter, r *http.Request) {
	modelsMutex.RLock()
	models := make([]string, len(supportedModels))
	copy(models, supportedModels)
	modelsMutex.RUnlock()

	modelEnum := make([]interface{}, len(models))
	for i, model := range models {
		modelEnum[i] = model
	}

	securitySchemes := map[string]interface{}{}
	security := []map[string]interface{}{}
	
	if apiKey != "" {
		securitySchemes["ApiKeyAuth"] = map[string]interface{}{
			"type": "http",
			"scheme": "bearer",
			"bearerFormat": "API key",
		}
		security = []map[string]interface{}{
			{
				"ApiKeyAuth": []string{},
			},
		}
	}

	openAPISpec := map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":       "DeepInfra OpenAI API Proxy",
			"description": "A proxy service for DeepInfra's OpenAI compatible API",
			"version":     "1.0.0",
		},
		"servers": []map[string]interface{}{
			{
				"url": "/",
			},
		},
		"paths": map[string]interface{}{
			"/v1/chat/completions": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Create a chat completion",
					"operationId": "createChatCompletion",
					"security":    security,
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/ChatCompletionRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Successful response",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Bad request",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"500": map[string]interface{}{
							"description": "Internal server error",
						},
					},
				},
			},
			"/models": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List available models",
					"operationId": "listModels",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Successful response",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "array",
										"items": map[string]interface{}{
											"type": "string",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"ChatCompletionRequest": map[string]interface{}{
					"type": "object",
					"required": []string{
						"model",
						"messages",
					},
					"properties": map[string]interface{}{
						"model": map[string]interface{}{
							"type": "string",
							"enum": modelEnum,
						},
						"messages": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/ChatMessage",
							},
						},
						"stream": map[string]interface{}{
							"type": "boolean",
							"default": false,
						},
						"temperature": map[string]interface{}{
							"type": "number",
							"format": "float",
							"minimum": 0,
							"maximum": 2,
							"default": 0.7,
						},
						"max_tokens": map[string]interface{}{
							"type": "integer",
							"minimum": 1,
							"default": 15000,
						},
					},
				},
				"ChatMessage": map[string]interface{}{
					"type": "object",
					"required": []string{
						"role",
						"content",
					},
					"properties": map[string]interface{}{
						"role": map[string]interface{}{
							"type": "string",
							"enum": []string{
								"system",
								"user",
								"assistant",
							},
						},
						"content": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
			"securitySchemes": securitySchemes,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openAPISpec)
}