package main

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
    "strings"
    "sync"
    "time"
)

var (
    workingProxies []string
    proxyMutex     sync.Mutex
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

func main() {
    http.HandleFunc("/check_proxies", checkProxiesHandler)
    http.HandleFunc("/v1/chat/completions", chatCompletionsHandler)
    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    http.ListenAndServe(":"+port, nil)
}

func checkProxiesHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    apiKey := os.Getenv("API_KEY")
    if apiKey == "" {
        http.Error(w, "API_KEY not set", http.StatusInternalServerError)
        return
    }

    if r.Header.Get("Authorization") != "Bearer "+apiKey {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    updateWorkingProxies()

    json.NewEncoder(w).Encode(map[string][]string{"working_proxies": workingProxies})
}

func chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req ChatCompletionRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    if len(req.Messages) == 0 {
        http.Error(w, "Messages are required", http.StatusBadRequest)
        return
    }

    proxy := getWorkingProxy()
    if proxy == "" {
        http.Error(w, "No working proxies available", http.StatusServiceUnavailable)
        return
    }

    proxyURL, _ := url.Parse(proxy)
    client := &http.Client{
        Transport: &http.Transport{
            Proxy: http.ProxyURL(proxyURL),
        },
    }

    deepinfraURL := "https://api.deepinfra.com/v1/openai/chat/completions"
    deepinfraReq, _ := http.NewRequest("POST", deepinfraURL, r.Body)
    deepinfraReq.Header = r.Header
    deepinfraReq.Header.Set("X-Deepinfra-Source", "web-page")
    deepinfraReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.107 Safari/537.36")

    resp, err := client.Do(deepinfraReq)
    if err != nil {
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

func updateWorkingProxies() {
    proxyMutex.Lock()
    defer proxyMutex.Unlock()

    if time.Since(lastUpdate) < 15*time.Minute && len(workingProxies) > 0 {
        return
    }

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

    workingProxies = []string{}
    for proxy := range results {
        workingProxies = append(workingProxies, proxy)
    }

    lastUpdate = time.Now()
}

func getWorkingProxy() string {
    proxyMutex.Lock()
    defer proxyMutex.Unlock()

    if len(workingProxies) == 0 || time.Since(lastUpdate) > 15*time.Minute {
        updateWorkingProxies()
    }

    if len(workingProxies) == 0 {
        return ""
    }

    proxy := workingProxies[0]
    workingProxies = workingProxies[1:]
    return proxy
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