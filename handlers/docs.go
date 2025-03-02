package handlers

import (
    "encoding/json"
    "fmt"
    "html/template"
    "net/http"

    "../services"
)

func SwaggerHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Printf("📚 Serving Swagger UI for %s\n", r.RemoteAddr)
    
    const swaggerTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>DeepInfra OpenAI API Proxy - Swagger UI</title>
  <link rel="stylesheet" type="text/css" href="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/4.18.3/swagger-ui.css" />
  <style>
    html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin: 0; background: #fafafa; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/4.18.3/swagger-ui-bundle.js" charset="UTF-8"></script>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/4.18.3/swagger-ui-standalone-preset.js" charset="UTF-8"></script>
  <script>
    window.onload = function() {
      const ui = SwaggerUIBundle({
        url: "/openapi.json",
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        layout: "StandaloneLayout"
      });
      window.ui = ui;
    };
  </script>
</body>
</html>`

    tmpl, err := template.New("swagger").Parse(swaggerTemplate)
    if err != nil {
        http.Error(w, "Error generating Swagger UI", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "text/html")
    tmpl.Execute(w, nil)
}

func OpenAPIHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Printf("📄 Serving OpenAPI JSON for %s\n", r.RemoteAddr)
    
    models := services.GetSupportedModels()
    
    modelEnum := make([]interface{}, len(models))
    for i, model := range models {
        modelEnum[i] = model
    }

    securitySchemes := map[string]interface{}{}
    security := []map[string]interface{}{}
    
    if services.IsAuthEnabled() {
        securitySchemes["ApiKeyAuth"] = map[string]interface{}{
            "type": "http",
            "scheme": "bearer",
            "bearerFormat": "API key",
        }
        security = []map[string]interface{}{
            {
                "ApiKeyAuth": []string{},
            },
        }
    }

    openAPISpec := map[string]interface{}{
        "openapi": "3.0.0",
        "info": map[string]interface{}{
            "title":       "DeepInfra OpenAI API Proxy",
            "description": "A proxy service for DeepInfra's OpenAI compatible API",
            "version":     "1.0.0",
        },
        "servers": []map[string]interface{}{
            {
                "url": "/",
            },
        },
        "paths": map[string]interface{}{
            "/v1/chat/completions": map[string]interface{}{
                "post": map[string]interface{}{
                    "summary":     "Create a chat completion",
                    "operationId": "createChatCompletion",
                    "security":    security,
                    "requestBody": map[string]interface{}{
                        "required": true,
                        "content": map[string]interface{}{
                            "application/json": map[string]interface{}{
                                "schema": map[string]interface{}{
                                    "$ref": "#/components/schemas/ChatCompletionRequest",
                                },
                            },
                        },
                    },
                    "responses": map[string]interface{}{
                        "200": map[string]interface{}{
                            "description": "Successful response",
                            "content": map[string]interface{}{
                                "application/json": map[string]interface{}{
                                    "schema": map[string]interface{}{
                                        "type": "object",
                                    },
                                },
                            },
                        },
                        "400": map[string]interface{}{
                            "description": "Bad request",
                        },
                        "401": map[string]interface{}{
                            "description": "Unauthorized",
                        },
                        "500": map[string]interface{}{
                            "description": "Internal server error",
                        },
                    },
                },
            },
            "/models": map[string]interface{}{
                "get": map[string]interface{}{
                    "summary":     "List available models",
                    "operationId": "listModels",
                    "responses": map[string]interface{}{
                        "200": map[string]interface{}{
                            "description": "Successful response",
                            "content": map[string]interface{}{
                                "application/json": map[string]interface{}{
                                    "schema": map[string]interface{}{
                                        "type": "array",
                                        "items": map[string]interface{}{
                                            "type": "string",
                                        },
                                    },
                                },
                            },
                        },
                    },
                },
            },
        },
        "components": map[string]interface{}{
            "schemas": map[string]interface{}{
                "ChatCompletionRequest": map[string]interface{}{
                    "type": "object",
                    "required": []string{
                        "model",
                        "messages",
                    },
                    "properties": map[string]interface{}{
                        "model": map[string]interface{}{
                            "type": "string",
                            "enum": modelEnum,
                        },
                        "messages": map[string]interface{}{
                            "type": "array",
                            "items": map[string]interface{}{
                                "$ref": "#/components/schemas/ChatMessage",
                            },
                        },
                        "stream": map[string]interface{}{
                            "type": "boolean",
                            "default": false,
                        },
                        "temperature": map[string]interface{}{
                            "type": "number",
                            "format": "float",
                            "minimum": 0,
                            "maximum": 2,
                            "default": 0.7,
                        },
                        "max_tokens": map[string]interface{}{
                            "type": "integer",
                            "minimum": 1,
                            "default": 15000,
                        },
                    },
                },
                "ChatMessage": map[string]interface{}{
                    "type": "object",
                    "required": []string{
                        "role",
                        "content",
                    },
                    "properties": map[string]interface{}{
                        "role": map[string]interface{}{
                            "type": "string",
                            "enum": []string{
                                "system",
                                "user",
                                "assistant",
                            },
                        },
                        "content": map[string]interface{}{
                            "type": "string",
                        },
                    },
                },
            },
            "securitySchemes": securitySchemes,
        },
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(openAPISpec)
}