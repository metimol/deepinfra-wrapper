package utils

import (
	"encoding/json"
	"net/http"

	"../types"
)

func SendErrorResponse(w http.ResponseWriter, message, errorType string, statusCode int, errorCode ...string) {
	code := errorType
	if len(errorCode) > 0 {
		code = errorCode[0]
	}
	
	errorResponse := types.OpenAIError{
		Error: struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		}{
			Message: message,
			Type:    errorType,
			Code:    code,
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(errorResponse)
}