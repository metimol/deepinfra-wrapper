package main

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "net/http"
    "os"
    "strings"
    "sync"
    "time"
)

var (
    proxyList     []string
    proxyMutex    sync.RWMutex
    lastUpdate    time.Time
    updateInterval = 15 * time.Minute
)

func main() {
    http.HandleFunc("/check_proxies", checkProxiesHandler)
    port := os.Getenv("PORT")
    if port == "" {
        port = "8080"
    }
    fmt.Printf("Starting server on port %s\n", port)
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

    authHeader := r.Header.Get("Authorization")
    if authHeader != "Bearer "+apiKey {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    if time.Since(lastUpdate) > updateInterval {
        if err := updateProxyList(); err != nil {
            http.Error(w, "Failed to update proxy list", http.StatusInternalServerError)
            return
        }
    }

    proxyMutex.RLock()
    proxies := make([]string, len(proxyList))
    copy(proxies, proxyList)
    proxyMutex.RUnlock()

    results := make(chan string, len(proxies))
    var wg sync.WaitGroup

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

    workingProxies := []string{}
    for proxy := range results {
        workingProxies = append(workingProxies, proxy)
    }

    json.NewEncoder(w).Encode(map[string][]string{"working_proxies": workingProxies})
}

func updateProxyList() error {
    resp, err := http.Get("https://api.proxyscrape.com/v3/free-proxy-list/get?request=displayproxies&protocol=http&proxy_format=protocolipport&format=text&anonymity=Elite,Anonymous&timeout=5015")
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return err
    }

    proxyMutex.Lock()
    proxyList = strings.Split(strings.TrimSpace(string(body)), "\n")
    proxyMutex.Unlock()

    lastUpdate = time.Now()
    return nil
}

func checkProxy(proxy string) bool {
    proxyURL := "http://" + proxy
    client := &http.Client{
        Transport: &http.Transport{
            Proxy: http.ProxyURL(func() *url.URL { u, _ := url.Parse(proxyURL); return u }()),
        },
        Timeout: 2 * time.Second,
    }

    resp, err := client.Get("https://api.deepinfra.com/v1/openai/models")
    if err != nil {
        return false
    }
    defer resp.Body.Close()

    return resp.StatusCode == http.StatusOK
}