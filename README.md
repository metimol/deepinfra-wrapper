# Proxy Checker API

This service provides a fast API for checking and returning working proxies.

## API Usage

Send a POST request to `/check_proxies` with the following header:

```
Authorization: Bearer your_api_key
```

The API will return a JSON response with working proxies:

```json
{
  "working_proxies": [
    "123.45.67.89:8080",
    "98.76.54.32:3128"
  ]
}
```

## Deployment

1. Clone this repository
2. Set your API_KEY in the deployment platform's environment variables
3. Deploy using the provided Dockerfile
