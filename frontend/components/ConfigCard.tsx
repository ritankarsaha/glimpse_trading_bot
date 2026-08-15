'use client';

import { useRef, useState } from 'react';
import type { MarketSummary } from '@/lib/types';
import { fetchMarkets, fetchQuotes, postAPIKey, quickTrade, startBot, stopBot } from '@/lib/api';
import type { Outcome, QuickTradeResult } from '@/lib/types';

interface Props {
  running: boolean;
  onStart: () => void;
  onStop: () => void;
  onError: (msg: string) => void;
  onClearError: () => void;
}

const STRATEGIES = [
  { value: 'nearest',     label: 'Nearest to Spot',  desc: 'Picks the range containing current BTC price. Safe, mechanical.' },
  { value: 'bestodds',    label: 'Best Odds',         desc: 'Picks the best-value range among the 3 nearest — lowest yes_price while still plausible (recommended).' },
  { value: 'feargreed',   label: 'Fear & Greed',      desc: 'Contrarian: bets bullish when market is fearful, bearish when greedy. Larger offset for extreme readings.' },
  { value: 'followcrowd', label: 'Follow Crowd',      desc: 'Picks the range with the highest market-implied probability (market consensus).' },
] as const;

const MODELS = [
  { value: 'gaussian',  label: 'Gaussian',        desc: 'Normal distribution centred on spot price. σ controls the spread.' },
  { value: 'skewed',    label: 'Skewed Gaussian', desc: 'Gaussian shifted by Fear & Greed index — contrarian (fear → bullish, greed → bearish).' },
  { value: 'lognormal', label: 'Log-Normal',      desc: 'log(P_T) ~ N(log(S)−σ²T/2, σ²T). Right-skewed, correct for asset prices. Uses annual vol + time-to-expiry automatically.' },
  { value: 'uniform',   label: 'Uniform',         desc: 'Equal probability across all 500 bins. Useful as a baseline / stress-test.' },
  { value: 'benter',    label: 'Benter (Two-Stage Logit)', desc: "Blends a log-normal fundamental forecast with the market-implied probability via Benter's (1994) two-stage logit combination. Uses calibrated weights (β_F, β_M) from data/calibration.json if present — run `-calibrate` to fit them — else defaults to β_F=β_M=1." },
] as const;

function fmtExpiry(endTimeUtc: number): string {
  if (!endTimeUtc) return '—';
  const diffMs = endTimeUtc * 1000 - Date.now();
  if (diffMs <= 0) return 'expired';
  const diffMins = Math.floor(diffMs / 60000);
  if (diffMins < 60) return `~${diffMins}m left`;
  const diffHours = Math.floor(diffMs / 3600000);
  if (diffHours < 24) return `~${diffHours}h · ends ${new Date(endTimeUtc * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`;
  const diffDays = Math.floor(diffMs / 86400000);
  if (diffDays < 7) return `${diffDays}d · ${new Date(endTimeUtc * 1000).toLocaleDateString([], { month: 'short', day: 'numeric' })}`;
  return new Date(endTimeUtc * 1000).toLocaleDateString([], { year: 'numeric', month: 'short', day: 'numeric' });
}

function Tooltip({ text }: { text: string }) {
  return (
    <span className="ml-1 cursor-help text-[#444] hover:text-[#666] text-[11px]" title={text}>ⓘ</span>
  );
}

type Mode = 'strategy' | 'kelly';

export default function ConfigCard({ running, onStart, onStop, onError, onClearError }: Props) {
  const [apiKey, setApiKey] = useState('');
  const [apiKeyBorder, setApiKeyBorder] = useState('border-[#222222]');
  const [markets, setMarkets] = useState<MarketSummary[]>([]);
  const [selectedMarket, setSelectedMarket] = useState('');
  const [marketHint, setMarketHint] = useState('');
  const [marketHintColor, setMarketHintColor] = useState('text-[#666666]');
  const [loadingMarkets, setLoadingMarkets] = useState(false);

  const [mode, setMode] = useState<Mode>('strategy');

  const [strategy, setStrategy] = useState('bestodds');
  const [contracts, setContracts] = useState(1);
  const [noDuplicates, setNoDuplicates] = useState(true);
  const [selectedOptionId, setSelectedOptionId] = useState(0);

  const [model, setModel] = useState('gaussian');
  const [sigmaPct, setSigmaPct] = useState(5);
  const [annualVolPct, setAnnualVolPct] = useState(65);
  const [kellyFraction, setKellyFraction] = useState(0.25);

  const [multiMarket, setMultiMarket] = useState(false);
  const [themeCapPct, setThemeCapPct] = useState(25);
  const [perMarketCapPct, setPerMarketCapPct] = useState(10);

  const [pollSec, setPollSec] = useState(60);
  const [maxTrades, setMaxTrades] = useState(5);
  const [dryRun, setDryRun] = useState(true);
  const [minExpiryMins, setMinExpiryMins] = useState(10);
  const [maxWalletPct, setMaxWalletPct] = useState(10);

  const [outcomes, setOutcomes] = useState<Outcome[]>([]);
  const [loadingOutcomes, setLoadingOutcomes] = useState(false);
  const [quickContracts, setQuickContracts] = useState(1);
  const [quickDryRun, setQuickDryRun] = useState(false);
  const [quickBusy, setQuickBusy] = useState(false);
  const [quickResult, setQuickResult] = useState<(QuickTradeResult & { error?: string }) | null>(null);
  const apiKeyTimer = useRef<ReturnType<typeof setTimeout>>();

  const handleAPIKeyChange = (val: string) => {
    setApiKey(val);
    if (!val) { setApiKeyBorder('border-[#222222]'); return; }
    if (!val.startsWith('glp_')) { setApiKeyBorder('border-[#ff4d4d]'); return; }
    setApiKeyBorder('border-[#f7931a]');
    clearTimeout(apiKeyTimer.current);
    apiKeyTimer.current = setTimeout(async () => {
      try { await postAPIKey(val); onClearError(); }
      catch (e: unknown) { onError('API key update failed: ' + (e instanceof Error ? e.message : e)); }
    }, 500);
  };

  const handleLoadMarkets = async () => {
    setLoadingMarkets(true);
    setMarketHint('');
    try {
      const ms = await fetchMarkets();
      if (!ms || ms.length === 0) {
        setMarkets([]);
        setMarketHint('All markets are resolved or expired.');
        setMarketHintColor('text-[#666666]');
        return;
      }
      const top = ms.slice(0, 40);
      setMarkets(top);
      setSelectedMarket(String(top[0].topic_id));
      const hourly = ms.filter(m => m.end_time_utc && (m.end_time_utc - Date.now() / 1000) < 86400).length;
      const extra = ms.length > 40 ? ` (showing 40 of ${ms.length})` : '';
      setMarketHint(`${ms.length} active markets${extra} — soonest first. ${hourly > 0 ? `${hourly} expire within 24h.` : ''}`);
      setMarketHintColor('text-[#00c48c]');
    } catch (e: unknown) {
      setMarketHint('Failed: ' + (e instanceof Error ? e.message : e));
      setMarketHintColor('text-[#ff4d4d]');
    } finally {
      setLoadingMarkets(false);
    }
  };

  const handleStart = async () => {
    const topicID = parseInt(selectedMarket) || 0;
    const skipMarketCheck = mode === 'kelly' && multiMarket;
    if (!topicID && !skipMarketCheck) {
      onError('Select a market first — click "Load ↻" to fetch active markets.');
      return;
    }
    if (apiKey && apiKey.startsWith('glp_')) {
      try { await postAPIKey(apiKey); } catch { }
    }
    try {
      if (mode === 'kelly') {
        await startBot({
          topic_id: topicID,
          contracts: 1,
          strategy: '',
          poll_sec: pollSec,
          max_trades: maxTrades,
          dry_run: dryRun,
          min_expiry_mins: minExpiryMins,
          max_wallet_pct: maxWalletPct,
          no_duplicates: false,
          forced_option_id: 0,
          model,
          sigma_pct: sigmaPct,
          annual_vol_pct: annualVolPct,
          kelly_fraction: kellyFraction,
          multi_market: multiMarket,
          theme_cap_pct: themeCapPct,
          per_market_cap_pct: perMarketCapPct,
        });
      } else {
        await startBot({
          topic_id: topicID,
          contracts,
          strategy,
          poll_sec: pollSec,
          max_trades: maxTrades,
          dry_run: dryRun,
          min_expiry_mins: minExpiryMins,
          max_wallet_pct: maxWalletPct,
          no_duplicates: noDuplicates,
          forced_option_id: selectedOptionId,
        });
      }
      onClearError();
      onStart();
    } catch (e: unknown) {
      onError(e instanceof Error ? e.message : 'Failed to start');
    }
  };

  const handleStop = async () => {
    try { await stopBot(); onStop(); }
    catch (e: unknown) { onError(e instanceof Error ? e.message : 'Failed to stop'); }
  };

  const handleLoadOutcomes = async () => {
    const topicID = parseInt(selectedMarket) || 0;
    if (!topicID) return;
    setLoadingOutcomes(true);
    setOutcomes([]);
    setSelectedOptionId(0);
    setQuickResult(null);
    try {
      setOutcomes(await fetchQuotes(topicID));
    } catch (e: unknown) {
      onError('Failed to load ranges: ' + (e instanceof Error ? e.message : e));
    } finally {
      setLoadingOutcomes(false);
    }
  };

  const handleQuickTrade = async () => {
    const topicID = parseInt(selectedMarket) || 0;
    if (!topicID) { onError('Select a market first'); return; }
    if (!selectedOptionId) { onError('Select a range to trade'); return; }
    setQuickBusy(true);
    setQuickResult(null);
    try {
      setQuickResult(await quickTrade({ topic_id: topicID, option_id: selectedOptionId, contracts: quickContracts, dry_run: quickDryRun }));
    } catch (e: unknown) {
      setQuickResult({ error: e instanceof Error ? e.message : 'Trade failed', option_id: 0, range: '', contracts: 0, cost_millisats: 0, commission_millisats: 0, yes_price_millisats: 0, dry_run: false });
    } finally {
      setQuickBusy(false);
    }
  };

  const kellyFractionLabel = () => {
    const pct = Math.round(kellyFraction * 100);
    if (kellyFraction <= 0.25) return `${pct}%-Kelly — conservative (Benter: ≤ ¼ for new models)`;
    if (kellyFraction >= 0.75) return `${pct}%-Kelly — ⚠ high; overestimated edge causes capital shrinkage`;
    return `${pct}%-Kelly`;
  };

  const selectedStratDesc = STRATEGIES.find(s => s.value === strategy)?.desc ?? '';
  const selectedModelDesc = MODELS.find(m => m.value === model)?.desc ?? '';

  return (
    <div className="card p-5">
      <h2 className="section-title mb-4">Configuration</h2>

      {/* API Key */}
      <div className="mb-3.5">
        <label className="field-label">API Key (optional — overrides the server-configured key)</label>
        <input
          type="password"
          value={apiKey}
          onChange={(e) => handleAPIKeyChange(e.target.value)}
          placeholder="glp_live_... (leave blank to use GLIMPSE_API_KEY)"
          autoComplete="off"
          className={`w-full bg-[#0a0a0a] border ${apiKeyBorder} rounded text-[#e0e0e0] font-mono text-[13px] px-2.5 py-2 outline-none transition-colors`}
        />
        <p className="text-[11px] mt-1 text-[#555]">The bot already trades using the API key configured on the server. Only paste one here to switch keys without restarting.</p>
      </div>

      {/* Market */}
      <div className="mb-3.5">
        <label className="field-label">Market</label>
        <div className="flex gap-2 items-center">
          {markets.length > 0 ? (
            <select
              value={selectedMarket}
              onChange={(e) => setSelectedMarket(e.target.value)}
              className="field-input flex-1 cursor-pointer"
            >
              {markets.map((m) => (
                <option key={m.topic_id} value={m.topic_id}>
                  #{m.topic_id} — {m.title} ({fmtExpiry(m.end_time_utc)})
                </option>
              ))}
            </select>
          ) : (
            <input
              type="number"
              value={selectedMarket}
              onChange={(e) => setSelectedMarket(e.target.value)}
              placeholder="Enter topic ID (e.g. 1116)"
              className="field-input flex-1"
              min={1}
            />
          )}
          <button
            onClick={handleLoadMarkets}
            disabled={loadingMarkets}
            className="px-3.5 py-2 bg-[#1a1a1a] border border-[#222222] rounded text-[#f7931a] text-[12px] font-semibold whitespace-nowrap hover:opacity-80 disabled:opacity-40 disabled:cursor-not-allowed transition-opacity cursor-pointer"
          >
            {loadingMarkets ? 'Loading…' : 'Load ↻'}
          </button>
        </div>
        {markets.length === 0 && !marketHint && (
          <p className="text-[11px] mt-1 text-[#555]">Type a topic ID directly if Load fails — find it in the Glimpse URL or DevTools.</p>
        )}
        {marketHint && <p className={`text-[11px] mt-1 ${marketHintColor}`}>{marketHint}</p>}
      </div>

      { }
      <div className="mb-3.5">
        <label className="field-label">Trading Mode</label>
        <div className="flex rounded overflow-hidden border border-[#222222]">
          <button
            onClick={() => setMode('strategy')}
            className={`flex-1 py-2 text-[12px] font-semibold transition-colors cursor-pointer ${
              mode === 'strategy' ? 'bg-[#f7931a] text-black' : 'bg-[#0a0a0a] text-[#666] hover:text-[#e0e0e0]'
            }`}
          >
            Strategy
          </button>
          <button
            onClick={() => setMode('kelly')}
            className={`flex-1 py-2 text-[12px] font-semibold transition-colors cursor-pointer ${
              mode === 'kelly' ? 'bg-[#00c48c] text-black' : 'bg-[#0a0a0a] text-[#666] hover:text-[#e0e0e0]'
            }`}
          >
            Kelly (QLS-LMSR)
          </button>
        </div>
      </div>

      { }
      {mode === 'strategy' && (
        <>
          <div className="mb-1">
            <label className="field-label">Strategy</label>
            <select value={strategy} onChange={(e) => setStrategy(e.target.value)} className="field-input cursor-pointer">
              {STRATEGIES.map(s => <option key={s.value} value={s.value}>{s.label}</option>)}
            </select>
            {selectedStratDesc && <p className="text-[11px] text-[#555] mt-1 leading-relaxed">{selectedStratDesc}</p>}
          </div>

          <div className="my-4 border-t border-[#1a1a1a]" />

          <div className="mb-3.5">
            <label className="field-label">Contracts per trade</label>
            <div className="flex items-center gap-2.5">
              <input type="range" min={1} max={10} value={contracts} onChange={(e) => setContracts(Number(e.target.value))} className="flex-1 accent-[#f7931a]" />
              <span className="font-mono text-[13px] text-[#f7931a] min-w-[28px] text-right">{contracts}</span>
            </div>
          </div>
        </>
      )}

      { }
      {mode === 'kelly' && (
        <>
          <div className="mb-3.5">
            <label className="field-label">
              Forecast Model
              <Tooltip text="The probability model that generates π*ᵢ — the distribution over 500 BTC price bins. Kelly trades bins where model prob > market-implied prob." />
            </label>
            <select value={model} onChange={(e) => setModel(e.target.value)} className="field-input cursor-pointer" style={{ borderColor: '#00c48c33' }}>
              {MODELS.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}
            </select>
            {selectedModelDesc && <p className="text-[11px] text-[#00c48c]/60 mt-1 leading-relaxed">{selectedModelDesc}</p>}
          </div>

          <div className="mb-3.5">
            <label className="field-label">
              Annualised volatility (%)
              <Tooltip text="BTC annualised vol used by Gaussian and Log-Normal models to compute σ = spot × vol × √(T/1yr). Historical BTC vol is ~60–80%. Overrides the fixed σ% below when set." />
            </label>
            <div className="flex items-center gap-2.5">
              <input type="range" min={10} max={150} step={5} value={annualVolPct} onChange={(e) => setAnnualVolPct(Number(e.target.value))} className="flex-1 accent-[#00c48c]" />
              <span className="font-mono text-[13px] text-[#00c48c] min-w-[36px] text-right">{annualVolPct}%</span>
            </div>
          </div>

          {(model === 'gaussian' || model === 'skewed') && (
            <div className="mb-3.5">
              <label className="field-label">
                σ fallback (% of spot, used when vol+time unavailable)
                <Tooltip text="Fallback width of the Gaussian forecast when time-to-expiry is unknown. 5% at $80k = ±$4000." />
              </label>
              <div className="flex items-center gap-2.5">
                <input type="range" min={1} max={20} step={0.5} value={sigmaPct} onChange={(e) => setSigmaPct(Number(e.target.value))} className="flex-1 accent-[#00c48c]" />
                <span className="font-mono text-[13px] text-[#00c48c] min-w-[36px] text-right">{sigmaPct}%</span>
              </div>
            </div>
          )}

          <div className="mb-1">
            <label className="field-label">
              Kelly fraction c
              <Tooltip text="Fraction of the Kelly-optimal bet to execute. Benter (1994) warns: if edge is overestimated by 2×, full Kelly shrinks your capital. ¼-Kelly is the safe default." />
            </label>
            <div className="flex items-center gap-2.5">
              <input type="range" min={0.05} max={1} step={0.05} value={kellyFraction} onChange={(e) => setKellyFraction(Number(e.target.value))} className="flex-1 accent-[#00c48c]" />
              <span className="font-mono text-[13px] text-[#00c48c] min-w-[36px] text-right">{kellyFraction.toFixed(2)}</span>
            </div>
            <p className={`text-[11px] mt-1 leading-relaxed ${kellyFraction >= 0.75 ? 'text-[#ff4d4d]' : 'text-[#555]'}`}>
              {kellyFractionLabel()}
            </p>
          </div>

          <div className="my-4 border-t border-[#1a1a1a]" />

          <div className="mb-3.5">
            <label className="flex items-center gap-2 cursor-pointer">
              <input type="checkbox" checked={multiMarket} onChange={(e) => setMultiMarket(e.target.checked)} className="w-4 h-4 accent-[#00c48c] cursor-pointer" />
              <span className="text-[13px]">
                Multi-Market (Portfolio)
                <Tooltip text="Trades every active Glimpse market each tick instead of just the selected one. Each market is sized independently with Kelly, then the Trader Education Part 3 rule applies: correlated BTC markets are treated as one theme and capped together." />
              </span>
            </label>
            {multiMarket && (
              <p className="text-[11px] text-[#00c48c]/60 mt-1 leading-relaxed">
                Ignores the selected market above — scans all active markets every tick.
              </p>
            )}
          </div>

          {multiMarket && (
            <>
              <div className="mb-3.5">
                <label className="field-label">
                  Theme cap (% of budget)
                  <Tooltip text="Maximum share of the Kelly budget allowed across all open markets combined. If the unconstrained sizes exceed this, every market's contracts are scaled down proportionally." />
                </label>
                <div className="flex items-center gap-2.5">
                  <input type="range" min={5} max={100} step={5} value={themeCapPct} onChange={(e) => setThemeCapPct(Number(e.target.value))} className="flex-1 accent-[#00c48c]" />
                  <span className="font-mono text-[13px] text-[#00c48c] min-w-[36px] text-right">{themeCapPct}%</span>
                </div>
              </div>

              <div className="mb-3.5">
                <label className="field-label">
                  Per-market cap (% of budget)
                  <Tooltip text="Maximum share of the Kelly budget allowed in any single market, applied after the theme cap." />
                </label>
                <div className="flex items-center gap-2.5">
                  <input type="range" min={5} max={50} step={5} value={perMarketCapPct} onChange={(e) => setPerMarketCapPct(Number(e.target.value))} className="flex-1 accent-[#00c48c]" />
                  <span className="font-mono text-[13px] text-[#00c48c] min-w-[36px] text-right">{perMarketCapPct}%</span>
                </div>
              </div>
            </>
          )}

          <div className="my-4 border-t border-[#1a1a1a]" />
        </>
      )}

      { }
      <div className="mb-3.5">
        <label className="field-label">Poll interval (seconds)</label>
        <div className="flex items-center gap-2.5">
          <input type="range" min={10} max={300} step={5} value={pollSec} onChange={(e) => setPollSec(Number(e.target.value))} className="flex-1 accent-[#f7931a]" />
          <span className="font-mono text-[13px] text-[#f7931a] min-w-[28px] text-right">{pollSec}s</span>
        </div>
      </div>

      <div className="mb-3.5">
        <label className="field-label">Max trades this session</label>
        <input type="number" value={maxTrades} onChange={(e) => setMaxTrades(Number(e.target.value))} min={1} max={100} className="field-input" />
      </div>

      <div className="my-4 border-t border-[#1a1a1a]" />
      <p className="section-title mb-3">Risk Controls</p>

      <div className="mb-3.5">
        <label className="field-label">
          Min time before expiry (minutes)
          <Tooltip text="Skip trading if the market expires within this many minutes. Near expiry, price swings are unpredictable." />
        </label>
        <div className="flex items-center gap-2.5">
          <input type="range" min={0} max={60} step={5} value={minExpiryMins} onChange={(e) => setMinExpiryMins(Number(e.target.value))} className="flex-1 accent-[#f7931a]" />
          <span className="font-mono text-[13px] text-[#f7931a] min-w-[28px] text-right">
            {minExpiryMins === 0 ? 'off' : `${minExpiryMins}m`}
          </span>
        </div>
      </div>

      <div className="mb-3.5">
        <label className="field-label">
          Max wallet % per trade
          <Tooltip text="In Kelly mode: budget passed to the optimizer = this % of wallet. In strategy mode: cap per single trade." />
        </label>
        <div className="flex items-center gap-2.5">
          <input type="range" min={0} max={50} step={1} value={maxWalletPct} onChange={(e) => setMaxWalletPct(Number(e.target.value))} className="flex-1 accent-[#f7931a]" />
          <span className="font-mono text-[13px] text-[#f7931a] min-w-[28px] text-right">
            {maxWalletPct === 0 ? 'off' : `${maxWalletPct}%`}
          </span>
        </div>
      </div>

      {mode === 'strategy' && (
        <div className="mb-3.5">
          <label className="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" checked={noDuplicates} onChange={(e) => setNoDuplicates(e.target.checked)} className="w-4 h-4 accent-[#f7931a] cursor-pointer" />
            <span className="text-[13px]">
              No duplicates
              <Tooltip text="Don't trade the same option range twice per session." />
            </span>
          </label>
        </div>
      )}

      <div className="mb-3.5">
        <label className="flex items-center gap-2 cursor-pointer">
          <input type="checkbox" checked={dryRun} onChange={(e) => setDryRun(e.target.checked)} className="w-4 h-4 accent-[#f7931a] cursor-pointer" />
          <span className="text-[13px]">Dry run (log only, no real trades)</span>
        </label>
      </div>

      {/* Start / Stop */}
      <div className="flex gap-2.5 mt-5">
        <button
          onClick={handleStart}
          disabled={running}
          className={`flex-1 py-2.5 px-4 rounded text-[13px] font-semibold cursor-pointer disabled:opacity-35 disabled:cursor-not-allowed transition-opacity hover:opacity-85 ${
            mode === 'kelly' ? 'bg-[#00c48c] text-black' : 'bg-[#f7931a] text-black'
          }`}
        >
          Start
        </button>
        <button
          onClick={handleStop}
          disabled={!running}
          className="flex-1 py-2.5 px-4 rounded bg-[#222222] text-[#e0e0e0] border border-[#333] text-[13px] font-semibold cursor-pointer hover:opacity-85 disabled:opacity-35 disabled:cursor-not-allowed transition-opacity"
        >
          Stop
        </button>
      </div>

      { }
      {mode === 'strategy' && (
        <div className="mt-5 pt-4 border-t border-[#1a1a1a]">
          <div className="flex items-center justify-between mb-3">
            <p className="section-title">
              Pick Range to Trade
              <Tooltip text="Load live quotes, pick a specific range, then let the bot always trade it or place a one-off trade." />
            </p>
            <button
              onClick={handleLoadOutcomes}
              disabled={loadingOutcomes || !selectedMarket}
              className="px-3 py-1 bg-[#1a1a1a] border border-[#222222] rounded text-[#f7931a] text-[11px] font-semibold whitespace-nowrap hover:opacity-80 disabled:opacity-40 disabled:cursor-not-allowed transition-opacity cursor-pointer"
            >
              {loadingOutcomes ? 'Loading…' : 'Load Ranges ↻'}
            </button>
          </div>

          {outcomes.length > 0 && (
            <>
              <div className="mb-3 max-h-40 overflow-y-auto rounded border border-[#222222] bg-[#0a0a0a]">
                <div
                  onClick={() => { setSelectedOptionId(0); setQuickResult(null); }}
                  className={`px-3 py-2 text-[12px] font-mono cursor-pointer border-b border-[#1a1a1a] transition-colors ${selectedOptionId === 0 ? 'bg-[#f7931a]/10 text-[#f7931a]' : 'text-[#666666] hover:bg-[#1a1a1a]'}`}
                >
                  — use strategy (auto) —
                </div>
                {outcomes.map((o) => (
                  <div
                    key={o.option_id}
                    onClick={() => { setSelectedOptionId(o.option_id); setQuickResult(null); }}
                    className={`px-3 py-2 text-[12px] font-mono cursor-pointer border-b border-[#1a1a1a] last:border-0 transition-colors ${selectedOptionId === o.option_id ? 'bg-[#00c48c]/10 text-[#00c48c]' : 'text-[#e0e0e0] hover:bg-[#1a1a1a]'}`}
                  >
                    <span className="font-semibold">{o.name}</span>
                    <span className="ml-2 text-[#666666]">yes: {o.yes_price_millisats}ms · #{o.option_id}</span>
                  </div>
                ))}
              </div>

              {selectedOptionId > 0 && (
                <p className="text-[11px] text-[#f7931a] mb-3">Bot will always trade this range. Turn off No Duplicates to repeat each tick.</p>
              )}

              <div className="mb-3">
                <label className="field-label">Contracts (for one-off trade)</label>
                <input type="number" value={quickContracts} onChange={(e) => setQuickContracts(Math.max(1, parseInt(e.target.value) || 1))} min={1} max={100} className="field-input" />
              </div>

              <div className="mb-3">
                <label className="flex items-center gap-2 cursor-pointer">
                  <input type="checkbox" checked={quickDryRun} onChange={(e) => setQuickDryRun(e.target.checked)} className="w-4 h-4 accent-[#f7931a] cursor-pointer" />
                  <span className="text-[13px]">Dry run (one-off trade only)</span>
                </label>
              </div>

              <button
                onClick={handleQuickTrade}
                disabled={quickBusy || !selectedOptionId}
                className="w-full py-2.5 px-4 rounded border border-[#00c48c] text-[#00c48c] bg-[#00c48c]/5 text-[13px] font-semibold cursor-pointer hover:bg-[#00c48c]/10 disabled:opacity-35 disabled:cursor-not-allowed transition-colors"
              >
                {quickBusy ? 'Placing…' : 'Trade Now (one-off)'}
              </button>

              {quickResult && (
                <div className={`mt-3 p-3 rounded text-[12px] font-mono leading-relaxed ${quickResult.error ? 'bg-[#ff4d4d]/10 border border-[#ff4d4d] text-[#ff4d4d]' : 'bg-[#00c48c]/10 border border-[#00c48c] text-[#00c48c]'}`}>
                  {quickResult.error ? `⚠ ${quickResult.error}` : (
                    <>
                      <div>{quickResult.dry_run ? '[DRY] ' : '✓ Trade placed'}</div>
                      <div>Range: {quickResult.range} · #{quickResult.option_id}</div>
                      {!quickResult.dry_run && (
                        <>
                          <div>Cost: {quickResult.cost_millisats < 1000 ? '< 1' : Math.floor(quickResult.cost_millisats / 1000)} sat · Comm: {quickResult.commission_millisats < 1000 ? '< 1' : Math.floor(quickResult.commission_millisats / 1000)} sat</div>
                          {quickResult.trade_id && <div className="text-[11px] opacity-70 mt-1">ID: {quickResult.trade_id}</div>}
                        </>
                      )}
                      {quickResult.dry_run && <div>Yes price: {quickResult.yes_price_millisats} ms/contract</div>}
                    </>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
