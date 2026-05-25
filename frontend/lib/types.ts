export interface KellyTradeSummary {
  bin_index: number;
  range: string;
  market_pct: number;
  model_pct: number;
  edge_pct: number;
  contracts: number;
}

export interface KellyLeg {
  bin_index: number;
  range: string;
  contracts: number;
  edge_pct: number;
}

export interface BotState {
  running: boolean;
  paused: boolean;
  dry_run: boolean;
  session_trades: number;
  total_cost_millisats: number;
  last_spot: number;
  last_fng: number;
  last_fng_label: string;
  wallet_balance: number;
  last_error: string;
  last_trade_time: string;
  recent_logs: string[];
  // Kelly mode fields
  model_name?: string;
  kelly_fraction?: number;
  misprice_count?: number;
  kelly_trades?: KellyTradeSummary[];
}

export interface TradeRecord {
  trade_id: string;
  topic_id: number;
  option_id: number;
  range: string;
  contracts: number;
  cost_millisats: number;
  commission_millisats: number;
  timestamp: string;
  dry_run: boolean;
  kelly_legs?: KellyLeg[];
}

export interface MarketSummary {
  topic_id: number;
  title: string;
  batch_id: string;
  end_time_utc: number;
  is_active: boolean;
  is_resolved: boolean;
}

export interface Outcome {
  option_id: number;
  name: string;
  yes_price_millisats: number;
  no_price_millisats: number;
}

export interface StartPayload {
  topic_id: number;
  contracts: number;
  strategy: string;
  poll_sec: number;
  max_trades: number;
  dry_run: boolean;
  min_expiry_mins: number;
  max_wallet_pct: number;
  no_duplicates: boolean;
  forced_option_id: number;
  // Kelly mode
  model?: string;
  sigma_pct?: number;
  kelly_fraction?: number;
}

export interface QuickTradePayload {
  topic_id: number;
  option_id: number;
  contracts: number;
  dry_run: boolean;
}

export interface QuickTradeResult {
  dry_run: boolean;
  trade_id?: string;
  option_id: number;
  range: string;
  contracts: number;
  cost_millisats: number;
  commission_millisats: number;
  yes_price_millisats: number;
}
