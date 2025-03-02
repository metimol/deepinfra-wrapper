package services

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	workingProxies  []string
	proxyMutex      sync.RWMutex
	lastProxyUpdate time.Time
	proxyIndex      int
	proxyIndexMutex sync.Mutex
)

func GetProxyCount() int {
	proxyMutex.RLock()
	defer proxyMutex.RUnlock()
	return len(workingProxies)
}

func UpdateWorkingProxies() {
	proxies, err := getProxyList()
	if err != nil {
		fmt.Printf("❌ Failed to get proxy list: %v\n", err)
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

	if len(newProxies) > 0 {
		proxyMutex.Lock()
		workingProxies = newProxies
		lastProxyUpdate = time.Now()
		proxyMutex.Unlock()
		
		proxyIndexMutex.Lock()
		proxyIndex = 0 
		proxyIndexMutex.Unlock()
		
		fmt.Printf("✅ Found %d working proxies out of %d tested\n", len(newProxies), len(proxies))
	} else {
		fmt.Println("⚠️ No working proxies found after testing")
	}
}

func GetWorkingProxy() string {
	proxyMutex.RLock()
	if len(workingProxies) == 0 {
		proxyMutex.RUnlock()
		if time.Since(lastProxyUpdate) > 2*time.Minute {
			fmt.Println("⚠️ No working proxies available, refreshing list...")
			UpdateWorkingProxies()
		}
		
		proxyMutex.RLock()
		if len(workingProxies) == 0 {
			proxyMutex.RUnlock()
			return ""
		}
	}
	
	proxyCount := len(workingProxies)
	proxyMutex.RUnlock()
	
	proxyIndexMutex.Lock()
	selectedIdx := proxyIndex
	proxyIndex = (proxyIndex + 1) % proxyCount
	proxyIndexMutex.Unlock()
	
	proxyMutex.RLock()
	defer proxyMutex.RUnlock()
	
	if selectedIdx >= len(workingProxies) {
		if len(workingProxies) == 0 {
			return ""
		}
		return workingProxies[0]
	}
	
	return workingProxies[selectedIdx]
}

func RemoveProxy(proxy string) {
	if proxy == "" {
		return
	}
	
	fmt.Printf("❌ Removing non-working proxy: %s\n", proxy)
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
	fmt.Println("📡 Fetching proxy list from external service...")
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	
	resp, err := client.Get(ProxyListURL)
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
	
	rand.Shuffle(len(proxies), func(i, j int) {
		proxies[i], proxies[j] = proxies[j], proxies[i]
	})
	
	fmt.Printf("📋 Retrieved %d potential proxies\n", len(proxies))
	return proxies, nil
}

func checkProxy(proxy string) bool {
	if proxy == "" {
		return false
	}
	
	proxyURL, err := url.Parse("http://" + proxy)
	if err != nil {
		return false
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(DeepInfraBaseURL + ModelsEndpoint)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}