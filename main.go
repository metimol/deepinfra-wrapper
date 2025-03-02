package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"./handlers"
	"./services"
)

func main() {
	fmt.Println("🚀 Starting DeepInfra proxy service...")
	
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		fmt.Println("⚠️  Warning: API_KEY environment variable not set. Authentication will be disabled.")
	} else {
		fmt.Println("🔐 API key authentication enabled")
	}
	
	// Initialize services
	services.InitAPIKey(apiKey)
	
	initReady := make(chan bool)
	go initializeServices(initReady)
	
	<-initReady
	
	// Set up routes
	http.HandleFunc("/v1/chat/completions", handlers.AuthMiddleware(handlers.ChatCompletionsHandler))
	http.HandleFunc("/models", handlers.ModelsHandler)
	http.HandleFunc("/docs", handlers.SwaggerHandler)
	http.HandleFunc("/openapi.json", handlers.OpenAPIHandler)
	
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("✅ Server started on port %s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func initializeServices(ready chan<- bool) {
	fmt.Println("🔄 Initializing services...")
	
	fmt.Println("🔍 Searching for working proxies...")
	services.UpdateWorkingProxies()
	
	proxyCount := services.GetProxyCount()
	if proxyCount == 0 {
		fmt.Println("⚠️  No working proxies found. Retrying...")
		services.UpdateWorkingProxies()
		proxyCount = services.GetProxyCount()
	}
	
	fmt.Printf("✅ Found %d working proxies\n", proxyCount)
	
	fmt.Println("🔍 Discovering supported models...")
	services.UpdateSupportedModels()
	
	modelCount := services.GetModelCount()
	if modelCount == 0 {
		fmt.Println("⚠️  No supported models found. Retrying...")
		services.UpdateSupportedModels()
		modelCount = services.GetModelCount()
	}
	
	fmt.Printf("✅ Found %d supported models\n", modelCount)
	
	go manageProxiesAndModels()
	
	ready <- true
	
	fmt.Println("🎉 Service is ready to use")
}

func manageProxiesAndModels() {
	proxyTicker := time.NewTicker(services.ProxyUpdateTime)
	modelsTicker := time.NewTicker(services.ModelsUpdateTime)
	
	for {
		select {
		case <-proxyTicker.C:
			fmt.Println("🔄 Refreshing proxy list...")
			oldCount := services.GetProxyCount()
			services.UpdateWorkingProxies()
			newCount := services.GetProxyCount()
			fmt.Printf("✅ Proxy refresh complete: %d → %d working proxies\n", oldCount, newCount)
		case <-modelsTicker.C:
			fmt.Println("🔄 Refreshing models list...")
			oldCount := services.GetModelCount()
			services.UpdateSupportedModels()
			newCount := services.GetModelCount()
			fmt.Printf("✅ Models refresh complete: %d → %d supported models\n", oldCount, newCount)
		}
	}
}