package main

import (
    "bytes"
    // "encoding/json"
    "fmt"
    "io"
    "mime/multipart"
    "net/http"
    "net/url"
    "os"
    "strings"
    "sync"
    "time"
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
    TopP        float64       `json:"top_p,omitempty"`
    TopK        int           `json:"top_k,omitempty"`
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

func main() {
    go updateWorkingProxiesPeriodically()
    http.HandleFunc("/v1/chat/completions", chatCompletionsHandler)
    http.HandleFunc("/v1/audio/transcriptions", whisperHandler)
    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    http.ListenAndServe(":"+port, nil)
}

func chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    proxy := getWorkingProxy()
    if proxy == "" {
        http.Error(w, "No working proxies available", http.StatusServiceUnavailable)
        return
    }

    proxyURL, _ := url.Parse(proxy)
    transport := &http.Transport{
        Proxy: http.ProxyURL(proxyURL),
    }
    client := &http.Client{Transport: transport}

    deepinfraURL := "https://api.deepinfra.com/v1/openai/chat/completions"
    deepinfraReq, _ := http.NewRequest(r.Method, deepinfraURL, r.Body)
    deepinfraReq.Header = r.Header
    deepinfraReq.Header.Set("X-Deepinfra-Source", "web-page")
    deepinfraReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36")

    resp, err := client.Do(deepinfraReq)
    if err != nil {
        removeProxy(proxy)
        http.Error(w, "Failed to call Deepinfra API", http.StatusInternalServerError)
        return
    }
    defer resp.Body.Close()

    for key, values := range resp.Header {
        for _, value := range values {
            w.Header().Add(key, value)
        }
    }
    w.WriteHeader(resp.StatusCode)
    io.Copy(w, resp.Body)
}

func whisperHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    err := r.ParseMultipartForm(10 << 20)
    if err != nil {
        http.Error(w, "Failed to parse form", http.StatusBadRequest)
        return
    }

    file, _, err := r.FormFile("file")
    if err != nil {
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

    proxy := getWorkingProxy()
    if proxy == "" {
        http.Error(w, "No working proxies available", http.StatusServiceUnavailable)
        return
    }

    resp, err := sendWhisperRequest(whisperReq, proxy)
    if err != nil {
        removeProxy(proxy)
        http.Error(w, "Failed to call Deepinfra Whisper API", http.StatusInternalServerError)
        return
    }
    defer resp.Body.Close()

    for key, values := range resp.Header {
        for _, value := range values {
            w.Header().Add(key, value)
        }
    }
    w.WriteHeader(resp.StatusCode)
    io.Copy(w, resp.Body)
}

func sendWhisperRequest(req WhisperRequest, proxyStr string) (*http.Response, error) {
    proxyURL, _ := url.Parse(proxyStr)
    client := &http.Client{
        Transport: &http.Transport{
            Proxy: http.ProxyURL(proxyURL),
        },
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

    deepinfraURL := "https://api.deepinfra.com/v1/inference/openai/whisper-large-v3"
    httpReq, err := http.NewRequest("POST", deepinfraURL, body)
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
        updateWorkingProxies()
        time.Sleep(15 * time.Minute)
    }
}

func updateWorkingProxies() {
    proxies, err := getProxyList()
    if err != nil {
        fmt.Println("Failed to get proxy list:", err)
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
    resp, err := http.Get("https://api.proxyscrape.com/v3/free-proxy-list/get?request=displayproxies&protocol=http&proxy_format=protocolipport&format=text&anonymity=Elite,Anonymous&timeout=5015")
    if err != nil {
        return nil, fmt.Errorf("failed to get proxy list: %v", err)
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read proxy list: %v", err)
    }

    return strings.Fields(string(body)), nil
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