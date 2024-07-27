package main

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "net/url"
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
    log.Printf("Starting server on port %s\n", port)
    log.Fatal(http.ListenAndServe(":"+port, nil))
}

func checkProxiesHandler(w http.ResponseWriter, r *http.Request) {
    log.Println("Received request to check proxies")

    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    apiKey := os.Getenv("API_KEY")
    if apiKey == "" {
        log.Println("API_KEY not set")
        http.Error(w, "API_KEY not set", http.StatusInternalServerError)
        return
    }

    authHeader := r.Header.Get("Authorization")
    if authHeader != "Bearer "+apiKey {
        log.Println("Unauthorized access attempt")
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    if time.Since(lastUpdate) > updateInterval {
        log.Println("Updating proxy list")
        if err := updateProxyList(); err != nil {
            log.Printf("Failed to update proxy list: %v", err)
            http.Error(w, "Failed to update proxy list", http.StatusInternalServerError)
            return
        }
    }

    proxyMutex.RLock()
    proxies := make([]string, len(proxyList))
    copy(proxies, proxyList)
    proxyMutex.RUnlock()

    log.Printf("Checking %d proxies", len(proxies))

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

    log.Printf("Found %d working proxies", len(workingProxies))

    json.NewEncoder(w).Encode(map[string][]string{"working_proxies": workingProxies})
}

func updateProxyList() error {
    resp, err := http.Get("https://api.proxyscrape.com/v3/free-proxy-list/get?request=displayproxies&protocol=http&proxy_format=protocolipport&format=text&anonymity=Elite,Anonymous&timeout=5015")
    if err != nil {
        return fmt.Errorf("failed to get proxy list: %v", err)
    }
    defer resp.Body.Close()

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("failed to read proxy list: %v", err)
    }

    proxyMutex.Lock()
    proxyList = strings.Split(strings.TrimSpace(string(body)), "\n")
    // Очищаем каждый прокси от лишних символов
    for i, proxy := range proxyList {
        proxyList[i] = strings.TrimSpace(proxy)
    }
    proxyMutex.Unlock()

    log.Printf("Updated proxy list with %d proxies", len(proxyList))

    lastUpdate = time.Now()
    return nil
}

func checkProxy(proxy string) bool {
    proxyURL, err := url.Parse(proxy)
    if err != nil {
        log.Printf("Failed to parse proxy URL %s: %v", proxy, err)
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
        log.Printf("Proxy %s failed: %v", proxy, err)
        return false
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusOK {
        log.Printf("Proxy %s is working", proxy)
        return true
    }

    log.Printf("Proxy %s returned status code %d", proxy, resp.StatusCode)
    return false
}