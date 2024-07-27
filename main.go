package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	proxyList []string
	proxyMutex sync.RWMutex
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

	authHeader := r.Header.Get("Authorization")
	if authHeader != "Bearer "+apiKey {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := updateProxyList(); err != nil {
		http.Error(w, "Failed to update proxy list", http.StatusInternalServerError)
		return
	}

	proxyMutex.RLock()
	proxies := make([]string, len(proxyList))
	copy(proxies, proxyList)
	proxyMutex.RUnlock()

	results := make(chan string, len(proxies))
	sem := make(chan struct{}, 100)
	var wg sync.WaitGroup

	for _, proxy := range proxies {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
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
		return fmt.Errorf("failed to get proxy list: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var newProxyList []string
	for scanner.Scan() {
		newProxyList = append(newProxyList, strings.TrimSpace(scanner.Text()))
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read proxy list: %v", err)
	}

	proxyMutex.Lock()
	proxyList = newProxyList
	proxyMutex.Unlock()

	return nil
}

func checkProxy(proxy string) bool {
	proxyURL, err := url.Parse("http://" + proxy)
	if err != nil {
		return false
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			DialContext: (&net.Dialer{
				Timeout: 3 * time.Second,
			}).DialContext,
		},
		Timeout: 5 * time.Second,
	}

	req, _ := http.NewRequest("HEAD", "https://api.deepinfra.com/v1/openai/models", nil)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}