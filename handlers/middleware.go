package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"deepinfra-wrapper/services"
	"deepinfra-wrapper/utils"
)

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := services.GetAPIKey()
		if apiKey == "" {
			fmt.Println("🔓 No API key set, skipping authentication")
			next(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			fmt.Println("❌ Authentication failed: Missing API key")
			utils.SendErrorResponse(w, "Missing API key", "invalid_request_error", http.StatusUnauthorized, "invalid_api_key")
			return
		}

		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(auth, bearerPrefix) {
			fmt.Println("❌ Authentication failed: Invalid API key format")
			utils.SendErrorResponse(w, "Invalid API key format", "invalid_request_error", http.StatusUnauthorized, "invalid_api_key")
			return
		}

		providedKey := strings.TrimPrefix(auth, bearerPrefix)
		if providedKey != apiKey {
			fmt.Println("❌ Authentication failed: Invalid API key")
			utils.SendErrorResponse(w, "Invalid API key", "invalid_request_error", http.StatusUnauthorized, "invalid_api_key")
			return
		}

		fmt.Println("✅ Authentication successful")
		next(w, r)
	}
}