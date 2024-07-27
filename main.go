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

func main() {
    http.HandleFunc("/check_proxies", checkProxiesHandler)
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

    proxies, err := getProxyList()
    if err != nil {
        http.Error(w, "Failed to get proxy list", http.StatusInternalServerError)
        return
    }

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