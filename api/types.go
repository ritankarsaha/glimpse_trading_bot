package api

// Market is the response from GET /nmarket/markets/{topic_id}/quotes
type Market struct {
	TopicID             int       `json:"topic_id"`
	Title               string    `json:"title"`
	BatchID             string    `json:"batch_id"`
	MarketEndTimeUTC    int64     `json:"market_end_time_utc"`
	IsResolved          bool      `json:"is_resolved"`
	QuoteMode           string    `json:"quote_mode"`
	MarketSubsidised    bool      `json:"market_subsidised"`
	TotalAmountInMarket int64     `json:"total_amount_in_market"`
	Outcomes            []Outcome `json:"outcomes"`
}

type Outcome struct {
	OptionID          int    `json:"option_id"`
	Name              string `json:"name"` 
	YesPrice          int64  `json:"yes_price"`
	NoPrice           int64  `json:"no_price"`
	YesPriceMillisats int64  `json:"yes_price_millisats"`
	NoPriceMillisats  int64  `json:"no_price_millisats"`
	Shares            int    `json:"shares"`
	SubsidisedShares  int    `json:"subsidised_shares"`
}

// TradeRequest is the body for POST /nmarket/enter-multi-topic-multi-leg
type TradeRequest struct {
	Topics []TopicTrade `json:"topics"`
}

type TopicTrade struct {
	TopicID int   `json:"topic_id"`
	Legs    []Leg `json:"legs"`
}

type Leg struct {
	OptionID  int `json:"option_id"`
	Contracts int `json:"contracts"`
}

// TradeResponse is returned from the trade endpoint
type TradeResponse struct {
	TopicsRequested          int           `json:"topics_requested"`
	TopicsEntered            int           `json:"topics_entered"`
	TopicsFailed             int           `json:"topics_failed"`
	TotalCostMillisats       int64         `json:"total_cost_millisats"`
	TotalCommissionMillisats int64         `json:"total_commission_millisats"`
	Results                  []TradeResult `json:"results"`
}

type TradeResult struct {
	TopicID             int    `json:"topic_id"`
	TotalCostMillisats  int64  `json:"total_cost_millisats"`
	CommissionMillisats int64  `json:"commission_millisats"`
	TradeID             string `json:"trade_id"`
}

// WalletBalance from GET /wallet-balance
type WalletBalance struct {
	Balance                 int64 `json:"balance"`
	CurrentOpenExposureSats int64 `json:"currentOpenExposureSats"`
	MaxExposureSats         int64 `json:"maxExposureSats"`
	Success                 bool  `json:"success"`
}

// MarketSummary is one entry from the GET /nmarket/markets listing
type MarketSummary struct {
	TopicID    int    `json:"topic_id"`
	Title      string `json:"title"`
	BatchID    string `json:"batch_id"`
	EndTimeUTC int64  `json:"end_time_utc"`
	IsActive   bool   `json:"is_active"`
	IsResolved bool   `json:"is_resolved"`
}

// CoinbaseSpotResponse from the Coinbase price API
type CoinbaseSpotResponse struct {
	Data struct {
		Amount   string `json:"amount"`
		Base     string `json:"base"`
		Currency string `json:"currency"`
	} `json:"data"`
}

// FngResponse from alternative.me Fear & Greed index
type FngResponse struct {
	Name string `json:"name"`
	Data []struct {
		Value               string `json:"value"`
		ValueClassification string `json:"value_classification"`
		Timestamp           string `json:"timestamp"`
	} `json:"data"`
}
