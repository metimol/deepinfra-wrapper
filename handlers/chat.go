package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"deepinfra-wrapper/services"
	"deepinfra-wrapper/types"
	"deepinfra-wrapper/utils"
)

var chatSemaphore = make(chan struct{}, 100)

func ChatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	select {
	case chatSemaphore <- struct{}{}:
		defer func() { <-chatSemaphore }()
	default:
		utils.SendErrorResponse(w, "Server is experiencing high load. Please try again later.", "rate_limit_error", http.StatusTooManyRequests)
		return
	}

	fmt.Printf("💬 Chat completion request from %s\n", r.RemoteAddr)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("❌ Failed to read request body: %v\n", err)
		utils.SendErrorResponse(w, "Failed to read request body", "invalid_request_error", http.StatusBadRequest)
		return
	}
	r.Body.Close()
	
	var chatReq types.ChatCompletionRequest
	err = json.Unmarshal(bodyBytes, &chatReq)
	if err != nil {
		fmt.Printf("❌ Failed to parse request: %v\n", err)
		utils.SendErrorResponse(w, "Failed to parse request body", "invalid_request_error", http.StatusBadRequest)
		return
	}

	fmt.Printf("🤖 Model requested: %s\n", chatReq.Model)

	if !services.IsModelSupported(chatReq.Model) {
		fmt.Printf("❌ Unsupported model: %s\n", chatReq.Model)
		utils.SendErrorResponse(w, "Unsupported model. Please use one of the supported models.", "invalid_request_error", http.StatusBadRequest, "model_not_found")
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
		fmt.Printf("❌ Failed to marshal request: %v\n", err)
		utils.SendErrorResponse(w, "Failed to marshal request", "internal_error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	success := false
	var lastErr error
	usedProxies := make(map[string]bool)
	var mu sync.Mutex
	
	fmt.Println("🔄 Beginning proxy attempts...")
	
	resultChan := make(chan bool, 1)
	errChan := make(chan error, 1)
	
	for i := 0; i < services.MaxProxyAttempts && !success; i++ {
		select {
		case <-ctx.Done():
			fmt.Println("⏱️ Request timeout")
			utils.SendErrorResponse(w, "Request timeout", "timeout", http.StatusGatewayTimeout)
			return
		default:
			proxy := services.GetWorkingProxy()
			if proxy == "" {
				fmt.Println("⚠️ No working proxy available, waiting for refresh...")
				if i > 0 {
					time.Sleep(500 * time.Millisecond)
				}
				continue
			}
			
			mu.Lock()
			if usedProxies[proxy] {
				mu.Unlock()
				continue
			}
			usedProxies[proxy] = true
			mu.Unlock()

			fmt.Printf("🌐 Attempt %d: Using proxy %s\n", i+1, proxy)
			
			go func(p string, attemptNum int) {
				result, err := sendChatRequest(ctx, p, services.DeepInfraBaseURL+services.ChatEndpoint, data, chatReq.Stream, w)
				if err != nil {
					fmt.Printf("❌ Proxy attempt %d failed: %v\n", attemptNum, err)
					services.RemoveProxy(p)
					errChan <- err
					return
				}
				
				if result {
					fmt.Printf("✅ Chat completion successful using proxy %s (attempt %d)\n", p, attemptNum)
					resultChan <- true
				} else {
					errChan <- fmt.Errorf("proxy request failed without error")
				}
			}(proxy, i+1)
			
			select {
			case result := <-resultChan:
				if result {
					success = true
					break
				}
			case err := <-errChan:
				lastErr = err
				continue
			case <-time.After(10 * time.Second):
				continue
			}
			
			if success {
				break
			}
		}
	}

	if !success {
		errMsg := "Unable to process the request after multiple attempts"
		if lastErr != nil {
			errMsg = "Error: " + lastErr.Error()
		}
		fmt.Printf("❌ All proxy attempts failed: %s\n", errMsg)
		utils.SendErrorResponse(w, errMsg, "internal_error", http.StatusInternalServerError)
	}
}

func sendChatRequest(ctx context.Context, proxy, endpoint string, data []byte, isStream bool, w http.ResponseWriter) (bool, error) {
	proxyURL, err := url.Parse("http://" + proxy)
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
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Deepinfra-Source", "web-page")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36")
	
	fmt.Printf("📡 Sending request to %s via proxy %s\n", endpoint, proxy)
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		if isStream {
			fmt.Println("📶 Handling streaming response")
			return handleStreamResponse(w, resp)
		} else {
			fmt.Println("📄 Handling normal response")
			return handleNormalResponse(w, resp)
		}
	}

	body, _ := io.ReadAll(resp.Body)
	return false, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
}

func handleStreamResponse(w http.ResponseWriter, resp *http.Response) (bool, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	var buf bytes.Buffer
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	chunkCount := 0
	
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		
		if strings.HasPrefix(line, "data: ") {
			fmt.Fprintf(w, "%s\n\n", line)
		} else {
			fmt.Fprintf(w, "data: %s\n\n", line)
		}
		
		flusher, ok := w.(http.Flusher)
		if ok {
			flusher.Flush()
		} else {
			buf.Reset()
			return false, fmt.Errorf("response writer does not support flushing")
		}
		
		chunkCount++
	}
	
	if err := scanner.Err(); err != nil {
		fmt.Printf("❌ Stream error: %v\n", err)
		return false, err
	}
	
	fmt.Printf("✅ Stream complete, sent %d chunks\n", chunkCount)
	return true, nil
}

func handleNormalResponse(w http.ResponseWriter, resp *http.Response) (bool, error) {
	w.Header().Set("Content-Type", "application/json")
	
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read response body: %v", err)
	}
	
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(bodyBytes)
	if err != nil {
		fmt.Printf("❌ Error writing response: %v\n", err)
		return false, err
	}
	
	fmt.Printf("✅ Response sent successfully (%d bytes)\n", len(bodyBytes))
	return true, nil
}