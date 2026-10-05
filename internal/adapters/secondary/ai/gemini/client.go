// Package gemini — Gemini low-level provider HTTP client.
//
// This file holds ONLY:
//   1. GeminiHTTPClient struct (provider configuration + HTTP client)
//   2. NewGeminiHTTPClient constructor
//   3. Getters (BaseURL, APIKey, Model) — used by Gemini capability adapters and bootstrap
// Capability-specific prompts and serializers live outside this provider client.
//
// The old generic decision path and its legacy proposal parser were removed.
// Domain-specific Gemini adapters own their request/response contracts and
// use this client only for shared HTTP configuration.
//
// Per contract ④ §8:
//   Mujeeb System Contract → system_instruction
//   Mujeeb Input Context → input (contents)
//   Catalog boundary → Structured Output (responseSchema)
//   Mujeeb Output Contract → capability-specific proposal
//
// Per contract ④ §3 (No Execution): this client contains no database or domain imports.
// It owns provider HTTP transport only.

package gemini

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultMaxOutputTokens = 2048
	defaultBaseURL         = "https://generativelanguage.googleapis.com"
	defaultModel           = "gemini-3.8-flash"
)

// GeminiHTTPClientConfig contains only runtime configuration. API keys are never copied
// into a proposal, error, log, or domain record (per contract ⑧ §23).
type GeminiHTTPClientConfig struct {
	BaseURL            string
	APIKey             string
	Model              string
	HTTPClient         *http.Client
	RequestTimeout     time.Duration
	MaxOutputTokens    int
	MaxInputCharacters int
}

// GeminiHTTPClient is the low-level Gemini HTTP client. Capability-specific adapters
// wrap it to implement explicit application ports.
//
// Per contract ④ §3 (No Execution): this client does HTTP only.
// No database imports, no external API calls beyond Gemini.
type GeminiHTTPClient struct {
	baseURL            string
	apiKey             string
	model              string
	httpClient         *http.Client
	requestTimeout     time.Duration
	maxOutputTokens    int
	maxInputCharacters int
}

// NewGeminiHTTPClient creates a Gemini HTTP client with the given config.
//
// The client contains no capability-specific prompt defaults.
func NewGeminiHTTPClient(cfg GeminiHTTPClientConfig) (*GeminiHTTPClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("gemini base URL must be an absolute HTTP or HTTPS URL")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("gemini API key is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultModel
	}
	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 60 * time.Second
	}
	maxOutputTokens := cfg.MaxOutputTokens
	if maxOutputTokens <= 0 {
		maxOutputTokens = defaultMaxOutputTokens
	}
	maxInputCharacters := cfg.MaxInputCharacters
	if maxInputCharacters <= 0 {
		maxInputCharacters = 48000
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &GeminiHTTPClient{
		baseURL:            baseURL,
		apiKey:             strings.TrimSpace(cfg.APIKey),
		model:              model,
		httpClient:         client,
		requestTimeout:     requestTimeout,
		maxOutputTokens:    maxOutputTokens,
		maxInputCharacters: maxInputCharacters,
	}, nil
}

// BaseURL returns the configured Gemini API base URL.
func (c *GeminiHTTPClient) BaseURL() string { return c.baseURL }

// APIKey returns the configured Gemini API key.
func (c *GeminiHTTPClient) APIKey() string { return c.apiKey }

// Model returns the configured Gemini model name.
func (c *GeminiHTTPClient) Model() string { return c.model }

// MaxInputCharacters returns the max input character limit.
func (c *GeminiHTTPClient) MaxInputCharacters() int { return c.maxInputCharacters }

// MaxOutputTokens returns the max output token limit.
func (c *GeminiHTTPClient) MaxOutputTokens() int { return c.maxOutputTokens }

// RequestTimeout returns the configured request timeout.
func (c *GeminiHTTPClient) RequestTimeout() time.Duration { return c.requestTimeout }

// HTTPClient returns the configured HTTP client shared by Gemini capability adapters.
func (c *GeminiHTTPClient) HTTPClient() *http.Client { return c.httpClient }
