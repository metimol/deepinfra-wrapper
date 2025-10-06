package services

import "time"

const (
	DeepInfraBaseURL = "https://api.deepinfra.com/v1/openai"
	ChatEndpoint     = "/chat/completions"
	ModelsEndpoint   = "/models"
	ProxyListURL     = "https://api.proxyscrape.com/v3/free-proxy-list/get?request=displayproxies&protocol=http&proxy_format=ipport&format=text&anonymity=Elite,Anonymous&timeout=5000"
	ProxyUpdateTime  = 10 * time.Minute
	ModelsUpdateTime = 24 * time.Hour
	MaxProxyAttempts = 10
	MaxRetries       = 3
)