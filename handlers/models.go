package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"deepinfra-wrapper/services"
	"deepinfra-wrapper/types"
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

// OpenAI-compatible /v1/models endpoint
func OpenAIModelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	fmt.Printf("📋 Handling OpenAI-compatible models request from %s\n", r.RemoteAddr)
	modelInfos := services.GetAllModelInfo()
	
	// Convert to OpenAI-compatible format
	openAIModels := make([]types.OpenAIModel, len(modelInfos))
	
	for i, modelInfo := range modelInfos {
		openAIModels[i] = types.OpenAIModel{
			ID:      modelInfo.ID,
			Object:  "model",
			Created: modelInfo.Created,
			OwnedBy: modelInfo.OwnedBy,
		}
	}
	
	response := types.OpenAIModelsResponse{
		Object: "list",
		Data:   openAIModels,
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
	fmt.Printf("✅ Returned %d models in OpenAI format\n", len(modelInfos))
}