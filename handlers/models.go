package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"../services"
)

func ModelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	fmt.Printf("📋 Handling models request from %s\n", r.RemoteAddr)
	models := services.GetSupportedModels()
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(models)
	fmt.Printf("✅ Returned %d models\n", len(models))
}