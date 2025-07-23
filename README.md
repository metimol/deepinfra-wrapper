# 🚀 DeepInfra Wrapper

A lightweight, efficient proxy service that provides free and unlimited access to DeepInfra's AI models through their OpenAI-compatible API.

## ✨ Features

- 🆓 **Free & Unlimited** - Access DeepInfra models without rate limits or costs
- 🔄 **Auto-rotating proxies** - Uses a pool of public proxies that automatically refreshes
- 🛡️ **Optional API key authentication** - Secure your instance when needed
- 📊 **Interactive Swagger UI** - Easy-to-use API documentation
- 🔍 **Model availability checks** - Only exposes models that are actually accessible
- ⚡ **Streaming support** - Full support for streaming responses
- 🔄 **OpenAI-compatible API** - Drop-in replacement for OpenAI API clients
- 📋 **OpenAI-compatible /v1/models endpoint** - Standard models listing endpoint
- 🏷️ **Model metadata** - Enhanced model information with type categorization

## 📋 Requirements

- Go 1.20 or higher
- Docker (optional, for containerized deployment)

## 🚀 Quick Start

### Using Pre-built Docker Image (Recommended)

```bash
# Pull the Docker image from GitHub Container Registry
docker pull ghcr.io/metimol/deepinfra-wrapper:latest

# Run the container
docker run -p 8080:8080 ghcr.io/metimol/deepinfra-wrapper:latest
```

### Building Docker Image Locally

```bash
# Build the Docker image
docker build -t deepinfra-proxy .

# Run the container
docker run -p 8080:8080 deepinfra-proxy
```

### Manual Build

```bash
# Download dependencies
go mod download

# Build the application
go build -o deepinfra-proxy .

# Run the application
./deepinfra-proxy
```

## 🔒 Authentication (Optional)

You can enable API key authentication by setting the `API_KEY` environment variable:

```bash
# With Docker
docker run -p 8080:8080 -e API_KEY=your-secret-key deepinfra-proxy

# Without Docker
API_KEY=your-secret-key ./deepinfra-proxy
```

When API key authentication is enabled, clients will need to include the API key in the Authorization header:

```
Authorization: Bearer your-secret-key
```

## 🔌 API Endpoints

### Chat Completions

```
POST /v1/chat/completions
```

Example request:

```json
{
  "model": "meta-llama/Llama-2-70b-chat-hf",
  "messages": [
    {
      "role": "user",
      "content": "Tell me a joke about programming"
    }
  ],
  "temperature": 0.7,
  "max_tokens": 1000,
  "stream": false
}
```

### List Available Models

#### OpenAI-Compatible Models Endpoint (Recommended)

```
GET /v1/models
```

Returns a list of all available models in OpenAI-compatible format. This endpoint follows the official OpenAI API specification and works with any OpenAI-compatible client.

Example response:
```json
{
  "object": "list",
  "data": [
    {
      "id": "meta-llama/Llama-2-70b-chat-hf",
      "object": "model",
      "created": 1677610602,
      "owned_by": "deepinfra"
    },
    {
      "id": "mistralai/Mixtral-8x7B-Instruct-v0.1",
      "object": "model",
      "created": 1677610602,
      "owned_by": "deepinfra"
    }
  ]
}
```

#### Legacy Models Endpoint

```
GET /models
```

Returns a simple array of model names. This endpoint is maintained for backward compatibility.

Example response:
```json
[
  "meta-llama/Llama-2-70b-chat-hf",
  "mistralai/Mixtral-8x7B-Instruct-v0.1"
]
```

### API Documentation

```
GET /docs
```

Interactive Swagger UI documentation for exploring and testing the API endpoints.

```
GET /openapi.json
```

OpenAPI specification document that can be imported into API tools.

## 📦 Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `API_KEY` | Secret key for API authentication | None (authentication disabled) |
| `PORT` | Port to run the server on | 8080 |

## 🔄 How It Works

1. The proxy fetches and maintains a list of working public proxies
2. It regularly checks which DeepInfra models are accessible and caches this list
3. When a request comes in, it routes the request through one of the working proxies to DeepInfra
4. If a proxy fails, it's automatically removed from the rotation
5. New proxies are regularly added to the pool to ensure reliability

## 🔗 OpenAI Compatibility

This service is fully compatible with the OpenAI API specification. You can use any OpenAI-compatible client library or tool by simply changing the base URL to point to your DeepInfra Wrapper instance.

### Key Compatible Endpoints

- `POST /v1/chat/completions` - Chat completions (matches OpenAI API)
- `GET /v1/models` - List available models (matches OpenAI API format)

### Supported Features

- ✅ Chat completions
- ✅ Streaming responses  
- ✅ Model listing
- ✅ Temperature and max_tokens parameters
- ✅ Message history and conversation context
- ✅ System messages

### Model Types Supported

The service automatically categorizes models by type:
- **Text Generation**: LLaMA, GPT, Claude, Mistral, DeepSeek, Qwen models
- **Audio**: Whisper models for speech recognition
- **Image**: Stable Diffusion, SDXL models for image generation
- **Embedding**: Text embedding models

## 📝 Client Usage Examples

### cURL

#### Chat Completions
```bash
curl -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "meta-llama/Llama-2-70b-chat-hf",
    "messages": [
      {
        "role": "user",
        "content": "Hello, how are you today?"
      }
    ]
  }'
```

#### List Models
```bash
# OpenAI-compatible format
curl "http://localhost:8080/v1/models"

# Legacy format
curl "http://localhost:8080/models"
```

### Python with OpenAI client

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1/",
    api_key="your-api-key"  # Only needed if API_KEY is set
)

# List available models
models = client.models.list()
print("Available models:")
for model in models.data:
    print(f"- {model.id} (owned by {model.owned_by})")

# Chat completion
response = client.chat.completions.create(
    model="meta-llama/Llama-2-70b-chat-hf",
    messages=[
        {"role": "user", "content": "What's the capital of France?"}
    ]
)

print(response.choices[0].message.content)
```

### JavaScript/Node.js

```javascript
import { OpenAI } from "openai";

const openai = new OpenAI({
  baseURL: "http://localhost:8080/v1/",
  apiKey: "your-api-key", // Only needed if API_KEY is set
});

async function main() {
  const response = await openai.chat.completions.create({
    model: "meta-llama/Llama-2-70b-chat-hf",
    messages: [
      { role: "user", content: "Explain quantum computing in simple terms" }
    ],
  });

  console.log(response.choices[0].message.content);
}

main();
```

## ⚠️ Limitations

- The service depends on the availability of public proxies
- Response times may vary based on proxy performance
- Some models might become temporarily unavailable

## 📚 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.