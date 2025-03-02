package services

import "time"

const (
	DeepInfraBaseURL = "https://api.deepinfra.com/v1/openai"
	ChatEndpoint     = "/chat/completions"
	ModelsEndpoint   = "/models"
	ProxyListURL     = "https://api.proxyscrape.com/v3/free-proxy-list/get?request=displayproxies&protocol=http&proxy_format=protocolipport&format=text&anonymity=Elite,Anonymous&timeout=5015"
	ProxyUpdateTime  = 10 * time.Minute
	ModelsUpdateTime = 60 * time.Minute
	MaxProxyAttempts = 30
	MaxRetries       = 3
)