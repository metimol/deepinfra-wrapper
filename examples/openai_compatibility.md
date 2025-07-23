# OpenAI Compatibility Examples

This document shows how to use the DeepInfra Wrapper with various OpenAI-compatible tools and libraries.

## Configuration

For all examples below, replace `http://localhost:8080` with your actual DeepInfra Wrapper instance URL.

## Python Examples

### Using OpenAI Python Library

```python
from openai import OpenAI

# Initialize client
client = OpenAI(
    base_url="http://localhost:8080/v1/",
    api_key="dummy-key"  # Use any value if authentication is disabled
)

# List all available models
def list_models():
    models = client.models.list()
    print(f"Found {len(models.data)} models:")
    for model in models.data:
        print(f"  - {model.id}")
    return models.data

# Chat with a model
def chat_example():
    response = client.chat.completions.create(
        model="meta-llama/Llama-2-7b-chat-hf",
        messages=[
            {"role": "system", "content": "You are a helpful assistant."},
            {"role": "user", "content": "Explain quantum computing in simple terms."}
        ],
        temperature=0.7,
        max_tokens=500
    )
    return response.choices[0].message.content

# Streaming example
def streaming_example():
    stream = client.chat.completions.create(
        model="meta-llama/Llama-2-7b-chat-hf",
        messages=[{"role": "user", "content": "Write a short story about a robot."}],
        stream=True,
        max_tokens=300
    )
    
    print("Streaming response:")
    for chunk in stream:
        if chunk.choices[0].delta.content is not None:
            print(chunk.choices[0].delta.content, end="")
    print()

if __name__ == "__main__":
    # List available models
    models = list_models()
    
    # Chat example
    print("\n=== Chat Example ===")
    response = chat_example()
    print(response)
    
    # Streaming example
    print("\n=== Streaming Example ===")
    streaming_example()
```

### Using LangChain

```python
from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage, SystemMessage

# Initialize LangChain with DeepInfra Wrapper
llm = ChatOpenAI(
    base_url="http://localhost:8080/v1/",
    api_key="dummy-key",
    model="meta-llama/Llama-2-7b-chat-hf",
    temperature=0.7
)

# Simple chat
messages = [
    SystemMessage(content="You are a helpful assistant that explains concepts clearly."),
    HumanMessage(content="What is machine learning?")
]

response = llm.invoke(messages)
print(response.content)
```

## JavaScript/Node.js Examples

### Using OpenAI Node.js Library

```javascript
import { OpenAI } from "openai";

const openai = new OpenAI({
  baseURL: "http://localhost:8080/v1/",
  apiKey: "dummy-key"
});

// List models
async function listModels() {
  const models = await openai.models.list();
  console.log(`Found ${models.data.length} models:`);
  models.data.forEach(model => {
    console.log(`  - ${model.id}`);
  });
  return models.data;
}

// Chat completion
async function chatExample() {
  const completion = await openai.chat.completions.create({
    model: "meta-llama/Llama-2-7b-chat-hf",
    messages: [
      { role: "system", content: "You are a helpful assistant." },
      { role: "user", content: "Explain the concept of recursion in programming." }
    ],
    temperature: 0.7,
    max_tokens: 400
  });
  
  return completion.choices[0].message.content;
}

// Streaming example
async function streamingExample() {
  const stream = await openai.chat.completions.create({
    model: "meta-llama/Llama-2-7b-chat-hf",
    messages: [{ role: "user", content: "Tell me about the solar system." }],
    stream: true,
    max_tokens: 300
  });

  console.log("Streaming response:");
  for await (const chunk of stream) {
    const content = chunk.choices[0]?.delta?.content || "";
    process.stdout.write(content);
  }
  console.log("\n");
}

// Run examples
async function main() {
  try {
    await listModels();
    
    console.log("\n=== Chat Example ===");
    const response = await chatExample();
    console.log(response);
    
    console.log("\n=== Streaming Example ===");
    await streamingExample();
  } catch (error) {
    console.error("Error:", error);
  }
}

main();
```

## cURL Examples

### List Models
```bash
# Get all models in OpenAI format
curl -X GET "http://localhost:8080/v1/models" \
  -H "Content-Type: application/json" | jq .

# Get specific model info (if implemented)
curl -X GET "http://localhost:8080/v1/models/meta-llama/Llama-2-7b-chat-hf" \
  -H "Content-Type: application/json" | jq .
```

### Chat Completions

```bash
# Simple chat
curl -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "meta-llama/Llama-2-7b-chat-hf",
    "messages": [
      {"role": "user", "content": "Hello, world!"}
    ],
    "temperature": 0.7,
    "max_tokens": 100
  }' | jq .

# Streaming chat
curl -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "meta-llama/Llama-2-7b-chat-hf",
    "messages": [
      {"role": "user", "content": "Write a haiku about programming"}
    ],
    "stream": true,
    "max_tokens": 50
  }'
```

## Tool Integration Examples

### Using with AI Code Editors

Many AI-powered code editors support custom OpenAI-compatible endpoints:

#### Cursor
1. Go to Settings → Models
2. Add a new provider with base URL: `http://localhost:8080/v1/`
3. Use any API key (if authentication is disabled)

#### Continue.dev
```json
{
  "models": [
    {
      "title": "DeepInfra Llama 2 7B",
      "provider": "openai",
      "model": "meta-llama/Llama-2-7b-chat-hf",
      "apiBase": "http://localhost:8080/v1/",
      "apiKey": "dummy-key"
    }
  ]
}
```

### Using with ChatGPT-like Interfaces

Many ChatGPT alternative interfaces support custom OpenAI endpoints:

- **ChatBot UI**: Set API endpoint to `http://localhost:8080/v1/`
- **LibreChat**: Configure custom endpoint in settings
- **Open WebUI**: Add as OpenAI-compatible provider

## Testing Compatibility

You can test OpenAI compatibility using the official OpenAI API test suite patterns:

```python
import openai
import pytest

def test_models_endpoint():
    client = openai.OpenAI(
        base_url="http://localhost:8080/v1/",
        api_key="dummy-key"
    )
    
    models = client.models.list()
    assert models.object == "list"
    assert len(models.data) > 0
    assert all(model.object == "model" for model in models.data)

def test_chat_completions():
    client = openai.OpenAI(
        base_url="http://localhost:8080/v1/",
        api_key="dummy-key"
    )
    
    response = client.chat.completions.create(
        model="meta-llama/Llama-2-7b-chat-hf",
        messages=[{"role": "user", "content": "Hello"}],
        max_tokens=10
    )
    
    assert response.object == "chat.completion"
    assert len(response.choices) > 0
    assert response.choices[0].message.role == "assistant"
```

## Notes

- Replace `meta-llama/Llama-2-7b-chat-hf` with any available model from your `/v1/models` endpoint
- Some advanced OpenAI features (like function calling, vision) depend on the underlying DeepInfra model capabilities
- The service automatically handles proxy rotation and model availability checking
- All examples work with both authenticated and non-authenticated configurations