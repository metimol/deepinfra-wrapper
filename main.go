package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	proxyList     []string
	proxyMutex    sync.RWMutex
	lastUpdate    time.Time
	updateInterval = 15 * time.Minute
)

func main() {
	r := gin.Default()
	r.POST("/check_proxies", authMiddleware(), checkProxies)
	r.Run(":8080")
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != "Bearer "+os.Getenv("API_KEY") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func checkProxies(c *gin.Context) {
	if time.Since(lastUpdate) > updateInterval {
		if err := updateProxyList(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update proxy list"})
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

	c.JSON(http.StatusOK, gin.H{"working_proxies": workingProxies})
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
	proxyURL, err := url.Parse("http://" + proxy)
	if err != nil {
		return false
	}

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   2 * time.Second,
	}

	resp, err := client.Get("https://api.deepinfra.com/v1/openai/models")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
