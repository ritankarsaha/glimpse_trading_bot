package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	glimpseBase = "https://www.glimpse.markets/api/v1"
	coinbaseURL = "https://api.coinbase.com/v2/prices/BTC-USD/spot"
	fngURL      = "https://api.alternative.me/fng/?limit=1"
	maxRetries  = 3
	retryDelay  = 2 * time.Second
	reqTimeout  = 10 * time.Second
)

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	body := e.Body
	// Strip HTML bodies (e.g. nginx 502 pages) — keep only the first line
	if len(body) > 80 || (len(body) > 0 && body[0] == '<') {
		switch e.StatusCode {
		case 502:
			body = "exchange gateway temporarily unavailable"
		case 503:
			body = "exchange service temporarily unavailable"
		case 504:
			body = "exchange gateway timed out"
		default:
			body = fmt.Sprintf("server error (body too long or HTML, %d bytes)", len(body))
		}
	}
	return fmt.Sprintf("API error %d: %s", e.StatusCode, body)
}

func IsUnauthorized(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 401
}

// IsTransient returns true for errors that are likely temporary (5xx from the exchange).
// The bot should log and skip the tick rather than stopping.
func IsTransient(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode >= 500
}

// GlimpseClient handles all API communication
type GlimpseClient struct {
	baseURL    string
	httpClient *http.Client
	mu         sync.RWMutex
	token      string
}

func NewGlimpseClient(token string) *GlimpseClient {
	return &GlimpseClient{
		baseURL:    glimpseBase,
		httpClient: &http.Client{Timeout: reqTimeout},
		token:      token,
	}
}

func (c *GlimpseClient) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

func (c *GlimpseClient) HasToken() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token != ""
}

func (c *GlimpseClient) getToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

func (c *GlimpseClient) request(method, url string, body []byte, glimpseAuth bool) ([]byte, int, error) {
	return c.requestN(method, url, body, glimpseAuth, maxRetries)
}

// requestFast makes a single attempt with no retry — used by per-tick bot calls so a
// sustained exchange outage doesn't block the goroutine for tens of seconds.
func (c *GlimpseClient) requestFast(method, url string, body []byte, glimpseAuth bool) ([]byte, int, error) {
	return c.requestN(method, url, body, glimpseAuth, fastRetries)
}

func (c *GlimpseClient) requestN(method, url string, body []byte, glimpseAuth bool, maxAttempts int) ([]byte, int, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var bodyReader io.Reader
		if len(body) > 0 {
			bodyReader = bytes.NewReader(body)
		}

		req, err := http.NewRequest(method, url, bodyReader)
		if err != nil {
			return nil, 0, fmt.Errorf("creating request: %w", err)
		}

		if glimpseAuth {
			// Raw JWT
			req.Header.Set("Authorization", c.getToken())
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "*/*")
			req.Header.Set("Origin", "https://www.glimpse.markets")
			req.Header.Set("Referer", "https://www.glimpse.markets/")
			req.Header.Set("sec-fetch-mode", "cors")
			req.Header.Set("sec-fetch-site", "same-origin")
		}

		log.Printf("[API] %s %s (attempt %d/%d)", method, url, attempt, maxRetries)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			delay := retryBaseDelay * (1 << (attempt - 1))
			log.Printf("[API] network error (attempt %d): %v — retrying in %s", attempt, err, delay)
			lastErr = err
			if attempt < maxAttempts {
				time.Sleep(delay)
			}
			continue
		}

		log.Printf("[API] %s %s → %d", method, url, resp.StatusCode)

		if resp.StatusCode >= 500 && attempt < maxAttempts {
			resp.Body.Close()
			delay := retryBaseDelay * (1 << (attempt - 1))
			log.Printf("[API] server error %d, retrying in %s (attempt %d/%d)", resp.StatusCode, delay, attempt, maxAttempts)
			time.Sleep(delay)
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", readErr)
		}

		return respBody, resp.StatusCode, nil
	}

	if lastErr != nil {
		return nil, 0, fmt.Errorf("request failed after %d attempts: %w", maxAttempts, lastErr)
	}
	return nil, 0, fmt.Errorf("request failed after %d attempts", maxAttempts)
}

func (c *GlimpseClient) glimpseGET(path string, out interface{}) error {
	return c.glimpseGETN(path, out, maxRetries)
}

// glimpseGETFast makes a single attempt — for bot per-tick calls where a sustained
// outage should fail fast and retry on the next poll interval, not block for 30s.
func (c *GlimpseClient) glimpseGETFast(path string, out interface{}) error {
	return c.glimpseGETN(path, out, fastRetries)
}

func (c *GlimpseClient) glimpseGETN(path string, out interface{}, attempts int) error {
	url := c.baseURL + path
	body, status, err := c.requestN(http.MethodGet, url, nil, true, attempts)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &APIError{StatusCode: status, Body: string(body)}
	}
	return json.Unmarshal(body, out)
}

func (c *GlimpseClient) glimpsePOST(path string, payload, out interface{}) error {
	return c.glimpsePOSTN(path, payload, out, maxRetries)
}

// glimpsePOSTFast makes a single attempt — for bot trade execution during tick.
func (c *GlimpseClient) glimpsePOSTFast(path string, payload, out interface{}) error {
	return c.glimpsePOSTN(path, payload, out, fastRetries)
}

func (c *GlimpseClient) glimpsePOSTN(path string, payload, out interface{}, attempts int) error {
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err)
	}
	url := c.baseURL + path
	body, status, err := c.requestN(http.MethodPost, url, reqBody, true, attempts)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return &APIError{StatusCode: status, Body: string(body)}
	}
	return json.Unmarshal(body, out)
}

// GetActiveMarkets returns markets that are not yet resolved and have not expired
func (c *GlimpseClient) GetActiveMarkets() ([]MarketSummary, error) {
	body, status, err := c.request(http.MethodGet, c.baseURL+"/nmarket/markets", nil, true)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, &APIError{StatusCode: status, Body: string(body)}
	}

	log.Printf("[API] /nmarket/markets raw: %s", apiTruncate(body, 600))

	var list []MarketSummary
	if json.Unmarshal(body, &list) == nil {
		return filterActiveMarkets(list), nil
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("markets endpoint returned unparseable response: %s", apiTruncate(body, 300))
	}
	for _, key := range []string{"markets", "data", "topics", "results", "items", "nmarkets", "market_list"} {
		raw, ok := wrapper[key]
		if !ok {
			continue
		}
		var inner []MarketSummary
		if json.Unmarshal(raw, &inner) == nil {
			return filterActiveMarkets(inner), nil
		}
	}

	return nil, fmt.Errorf("could not locate market array in response — check terminal for raw output")
}

func filterActiveMarkets(all []MarketSummary) []MarketSummary {
	now := time.Now().Unix()
	var active []MarketSummary
	for _, m := range all {
		if m.IsActive && !m.IsResolved && (m.EndTimeUTC == 0 || m.EndTimeUTC > now) {
			active = append(active, m)
		}
	}
	// Sort by soonest expiry first so hourly markets (expiring within hours) appear at the top.
	for i := 0; i < len(active)-1; i++ {
		for j := i + 1; j < len(active); j++ {
			if active[j].EndTimeUTC > 0 && (active[i].EndTimeUTC == 0 || active[j].EndTimeUTC < active[i].EndTimeUTC) {
				active[i], active[j] = active[j], active[i]
			}
		}
	}
	return active
}

func apiTruncate(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// GetQuotes fetches live quotes — uses fast path (1 attempt) for bot tick calls.
func (c *GlimpseClient) GetQuotes(topicID int) (*Market, error) {
	var m Market
	if err := c.glimpseGETFast(fmt.Sprintf("/nmarket/markets/%d/quotes", topicID), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// PlaceTrade submits a multi-leg trade — uses fast path; caller handles retry via next tick.
func (c *GlimpseClient) PlaceTrade(req TradeRequest) (*TradeResponse, error) {
	var resp TradeResponse
	if err := c.glimpsePOSTFast("/nmarket/enter-multi-topic-multi-leg", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *GlimpseClient) GetWalletBalance() (*WalletBalance, error) {
	var wb WalletBalance
	if err := c.glimpseGETFast("/wallet-balance", &wb); err != nil {
		return nil, err
	}
	return &wb, nil
}

// GetTradesByTopic fetches trade history for a given topic
func (c *GlimpseClient) GetTradesByTopic(topicID int) ([]interface{}, error) {
	body, status, err := c.request(http.MethodGet,
		fmt.Sprintf("%s/nmarket/trades-by-topic?topic_id=%d", c.baseURL, topicID),
		nil, true)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, &APIError{StatusCode: status, Body: string(body)}
	}
	var out []interface{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parsing trades-by-topic: %w", err)
	}
	return out, nil
}

func (c *GlimpseClient) GetBTCSpot() (float64, error) {
	body, status, err := c.request(http.MethodGet, coinbaseURL, nil, false)
	if err != nil {
		return 0, err
	}
	if status < 200 || status >= 300 {
		return 0, fmt.Errorf("coinbase returned %d", status)
	}
	var resp CoinbaseSpotResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("parsing coinbase response: %w", err)
	}
	price, err := strconv.ParseFloat(resp.Data.Amount, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing price %q: %w", resp.Data.Amount, err)
	}
	return price, nil
}

func (c *GlimpseClient) GetFearAndGreed() (int, string, error) {
	body, status, err := c.request(http.MethodGet, fngURL, nil, false)
	if err != nil {
		return 0, "", err
	}
	if status < 200 || status >= 300 {
		return 0, "", fmt.Errorf("fng API returned %d", status)
	}
	var resp FngResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, "", fmt.Errorf("parsing F&G response: %w", err)
	}
	if len(resp.Data) == 0 {
		return 0, "", fmt.Errorf("empty F&G data")
	}
	val, err := strconv.Atoi(resp.Data[0].Value)
	if err != nil {
		return 0, "", fmt.Errorf("parsing F&G value %q: %w", resp.Data[0].Value, err)
	}
	return val, resp.Data[0].ValueClassification, nil
}
