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
)

var (
	workingProxies []string
	proxyMutex     sync.RWMutex
	lastUpdate     time.Time
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

const (
	baseURL      = "https://api.deepinfra.com/v1/openai/chat/completions"
	whisperURL   = "https://api.deepinfra.com/v1/inference/openai/whisper-large-v3"
)

func main() {
	fmt.Println("Starting proxy update routine")
	go updateWorkingProxiesPeriodically()
	http.HandleFunc("/v1/chat/completions", chatCompletionsHandler)
	http.HandleFunc("/v1/audio/transcriptions", whisperHandler)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("Server starting on port %s\n", port)
	http.ListenAndServe(":"+port, nil)
}

func chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Received chat completion request: %s %s\n", r.Method, r.URL.Path)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var chatReq ChatCompletionRequest
	err := json.NewDecoder(r.Body).Decode(&chatReq)
	if err != nil {
		http.Error(w, "Failed to parse request body", http.StatusBadRequest)
		return
	}

	if chatReq.Temperature == 0 {
		chatReq.Temperature = 0.7
	}
	if chatReq.MaxTokens == 0 {
		chatReq.MaxTokens = 15000
	}
	chatReq.Stream = true

	data, err := json.Marshal(chatReq)
	if err != nil {
		http.Error(w, "Failed to marshal request", http.StatusInternalServerError)
		return
	}

	for i := 0; i < 30; i++ {
		proxy := getWorkingProxy()
		if proxy == "" {
			fmt.Println("No working proxies available")
			time.Sleep(time.Second)
			continue
		}

		proxyURL, _ := url.Parse(proxy)
		client := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			},
			Timeout: 60 * time.Second,
		}

		req, _ := http.NewRequest("POST", baseURL, bytes.NewBuffer(data))
		req.Header = getHeaders()

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Failed to call Deepinfra API: %v\n", err)
			removeProxy(proxy)
			time.Sleep(time.Second)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if line != "" {
					fmt.Fprintf(w, "data: %s\n\n", line)
					w.(http.Flusher).Flush()
				}
			}
			resp.Body.Close()
			return
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		removeProxy(proxy)
		fmt.Printf("Error response from Deepinfra API: %s\n", string(body))
		time.Sleep(time.Second)
	}

	errorResponse := OpenAIError{
		Error: struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		}{
			Message: "Unable to process the request after multiple attempts",
			Type:    "internal_error",
			Code:    "internal_error",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(errorResponse)
}

func whisperHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Received whisper request: %s %s\n", r.Method, r.URL.Path)
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		fmt.Printf("Failed to parse form: %v\n", err)
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		fmt.Printf("Failed to get file from form: %v\n", err)
		http.Error(w, "Failed to get file from form", http.StatusBadRequest)
		return
	}
	defer file.Close()

	task := r.FormValue("task")
	if task == "" {
		task = "transcribe"
	}

	language := r.FormValue("language")

	whisperReq := WhisperRequest{
		File:     file,
		Task:     task,
		Language: language,
	}

	for i := 0; i < 30; i++ {
		proxy := getWorkingProxy()
		if proxy == "" {
			fmt.Println("No working proxies available")
			time.Sleep(time.Second)
			continue
		}

		resp, err := sendWhisperRequest(whisperReq, proxy)
		if err != nil {
			fmt.Printf("Failed to call Deepinfra Whisper API: %v\n", err)
			removeProxy(proxy)
			time.Sleep(time.Second)
			continue
		}

		fmt.Printf("Received response from Deepinfra Whisper API with status: %d\n", resp.StatusCode)

		if resp.StatusCode == http.StatusOK {
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
			resp.Body.Close()
			return
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		removeProxy(proxy)
		fmt.Printf("Error response from Deepinfra Whisper API: %s\n", string(body))
		time.Sleep(time.Second)
	}

	errorResponse := OpenAIError{
		Error: struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		}{
			Message: "Unable to process the whisper request after multiple attempts",
			Type:    "internal_error",
			Code:    "internal_error",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(errorResponse)
}

func sendWhisperRequest(req WhisperRequest, proxyStr string) (*http.Response, error) {
	proxyURL, _ := url.Parse(proxyStr)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 60 * time.Second,
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("audio", "audio.wav")
	if err != nil {
		return nil, err
	}
	io.Copy(part, req.File)

	writer.WriteField("task", req.Task)
	if req.Language != "" {
		writer.WriteField("language", req.Language)
	}

	writer.Close()

	httpReq, err := http.NewRequest("POST", whisperURL, body)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	httpReq.Header.Set("X-Deepinfra-Source", "web-page")
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36")

	return client.Do(httpReq)
}

func updateWorkingProxiesPeriodically() {
	for {
		fmt.Println("Updating working proxies")
		updateWorkingProxies()
		time.Sleep(15 * time.Minute)
	}
}

func updateWorkingProxies() {
	proxies, err := getProxyList()
	if err != nil {
		fmt.Printf("Failed to get proxy list: %v\n", err)
		return
	}

	var wg sync.WaitGroup
	results := make(chan string, len(proxies))

	for _, proxy := range proxies {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if checkProxy(p) {
				results <- p
			}
		}(proxy)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	newProxies := make([]string, 0, len(proxies))
	for proxy := range results {
		newProxies = append(newProxies, proxy)
	}

	proxyMutex.Lock()
	workingProxies = newProxies
	lastUpdate = time.Now()
	proxyMutex.Unlock()

	fmt.Printf("Working proxies: %d\n", len(newProxies))
}

func getWorkingProxy() string {
	proxyMutex.RLock()
	if len(workingProxies) > 0 {
		proxy := workingProxies[0]
		proxyMutex.RUnlock()
		removeProxy(proxy)
		return proxy
	}
	proxyMutex.RUnlock()

	fmt.Println("No working proxies, updating list")
	updateWorkingProxies()

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
			workingProxies = append(workingProxies[:i], workingProxies[i+1:]...)
			break
		}
	}
}

func getProxyList() ([]string, error) {
	fmt.Println("Fetching proxy list")
	resp, err := http.Get("https://api.proxyscrape.com/v3/free-proxy-list/get?request=displayproxies&protocol=http&proxy_format=protocolipport&format=text&anonymity=Elite,Anonymous&timeout=5015")
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy list: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read proxy list: %v", err)
	}

	proxies := strings.Fields(string(body))
	fmt.Printf("Fetched proxies: %d\n", len(proxies))
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

	resp, err := client.Get("https://api.deepinfra.com/v1/openai/models")
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