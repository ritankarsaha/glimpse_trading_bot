'use client';

import { useRef, useState } from 'react';
import type { MarketSummary } from '@/lib/types';
import { fetchMarkets, fetchQuotes, postToken, quickTrade, startBot, stopBot } from '@/lib/api';
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
  { value: 'followcrowd', label: 'Follow Crowd',      desc: 'Picks the range with the highest market-implied probability (most expensive = market consensus).' },
] as const;

function fmtExpiry(endTimeUtc: number): string {
  if (!endTimeUtc) return '—';
  const nowMs = Date.now();
  const endMs = endTimeUtc * 1000;
  const diffMs = endMs - nowMs;
  if (diffMs <= 0) return 'expired';
  const diffMins = Math.floor(diffMs / 60000);
  if (diffMins < 60) return `~${diffMins}m left`;
  const diffHours = Math.floor(diffMs / 3600000);
  if (diffHours < 24) {
    const t = new Date(endMs).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    return `~${diffHours}h · ends ${t}`;
  }
  const diffDays = Math.floor(diffMs / 86400000);
  if (diffDays < 7) return `${diffDays}d · ${new Date(endMs).toLocaleDateString([], { month: 'short', day: 'numeric' })}`;
  return new Date(endMs).toLocaleDateString([], { year: 'numeric', month: 'short', day: 'numeric' });
}

function Tooltip({ text }: { text: string }) {
  return (
    <span className="ml-1 cursor-help text-[#444] hover:text-[#666] text-[11px]" title={text}>
      ⓘ
    </span>
  );
}

export default function ConfigCard({ running, onStart, onStop, onError, onClearError }: Props) {
  const [token, setToken] = useState('');
  const [tokenBorder, setTokenBorder] = useState('border-[#222222]');
  const [markets, setMarkets] = useState<MarketSummary[]>([]);
  const [selectedMarket, setSelectedMarket] = useState('');
  const [marketHint, setMarketHint] = useState('');
  const [marketHintColor, setMarketHintColor] = useState('text-[#666666]');
  const [loadingMarkets, setLoadingMarkets] = useState(false);
  const [strategy, setStrategy] = useState('bestodds');
  const [contracts, setContracts] = useState(1);
  const [pollSec, setPollSec] = useState(60);
  const [maxTrades, setMaxTrades] = useState(5);
  const [dryRun, setDryRun] = useState(true);
  const [minExpiryMins, setMinExpiryMins] = useState(10);
  const [maxWalletPct, setMaxWalletPct] = useState(10);
  const [noDuplicates, setNoDuplicates] = useState(true);
  const [outcomes, setOutcomes] = useState<Outcome[]>([]);
  const [loadingOutcomes, setLoadingOutcomes] = useState(false);
  const [selectedOptionId, setSelectedOptionId] = useState(0); // 0 = use strategy
  const [quickContracts, setQuickContracts] = useState(1);
  const [quickDryRun, setQuickDryRun] = useState(false);
  const [quickBusy, setQuickBusy] = useState(false);
  const [quickResult, setQuickResult] = useState<(QuickTradeResult & { error?: string }) | null>(null);
  const tokenTimer = useRef<ReturnType<typeof setTimeout>>();

  const handleTokenChange = (val: string) => {
    setToken(val);
    if (!val) { setTokenBorder('border-[#222222]'); return; }
    if (!val.startsWith('eyJ')) { setTokenBorder('border-[#ff4d4d]'); return; }
    setTokenBorder('border-[#f7931a]');
    clearTimeout(tokenTimer.current);
    tokenTimer.current = setTimeout(async () => {
      try { await postToken(val); onClearError(); }
      catch (e: unknown) { onError('Token update failed: ' + (e instanceof Error ? e.message : e)); }
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
      setMarketHint(`${ms.length} active markets${extra} — soonest first. ${hourly > 0 ? `${hourly} expire within 24h (pick those).` : ''}`);
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
    if (!topicID) {
      onError('Select a market first — click "Load ↻" to fetch active markets.');
      return;
    }
    if (token && token.startsWith('eyJ')) {
      try { await postToken(token); } catch { }
    }
    try {
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
      onClearError();
      onStart();
    } catch (e: unknown) {
      onError(e instanceof Error ? e.message : 'Failed to start');
    }
  };

  const handleStop = async () => {
    try {
      await stopBot();
      onStop();
    } catch (e: unknown) {
      onError(e instanceof Error ? e.message : 'Failed to stop');
    }
  };

  const handleLoadOutcomes = async () => {
    const topicID = parseInt(selectedMarket) || 0;
    if (!topicID) return;
    setLoadingOutcomes(true);
    setOutcomes([]);
    setSelectedOptionId(0);
    setQuickResult(null);
    try {
      const data = await fetchQuotes(topicID);
      setOutcomes(data);
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
      const res = await quickTrade({ topic_id: topicID, option_id: selectedOptionId, contracts: quickContracts, dry_run: quickDryRun });
      setQuickResult(res);
    } catch (e: unknown) {
      setQuickResult({ error: e instanceof Error ? e.message : 'Trade failed', option_id: 0, range: '', contracts: 0, cost_millisats: 0, commission_millisats: 0, yes_price_millisats: 0, dry_run: false });
    } finally {
      setQuickBusy(false);
    }
  };

  const selectedStratDesc = STRATEGIES.find(s => s.value === strategy)?.desc ?? '';

  return (
    <div className="card p-5">
      <h2 className="section-title mb-4">Configuration</h2>

      {/* JWT Token */}
      <div className="mb-3.5">
        <label className="field-label">JWT Token (paste from DevTools)</label>
        <input
          type="password"
          value={token}
          onChange={(e) => handleTokenChange(e.target.value)}
          placeholder="eyJ..."
          autoComplete="off"
          className={`w-full bg-[#0a0a0a] border ${tokenBorder} rounded text-[#e0e0e0] font-mono text-[13px] px-2.5 py-2 outline-none transition-colors`}
        />
      </div>

      {/* Market */}
      <div className="mb-3.5">
        <label className="field-label">Market</label>
        <div className="flex gap-2 items-center">
          <select
            value={selectedMarket}
            onChange={(e) => setSelectedMarket(e.target.value)}
            className="field-input flex-1 cursor-pointer"
          >
            {markets.length === 0 ? (
              <option value="">— paste token then click Load —</option>
            ) : (
              markets.map((m) => {
                const expiry = fmtExpiry(m.end_time_utc);
                return (
                  <option key={m.topic_id} value={m.topic_id}>
                    #{m.topic_id} — {m.title} ({expiry})
                  </option>
                );
              })
            )}
          </select>
          <button
            onClick={handleLoadMarkets}
            disabled={loadingMarkets}
            className="px-3.5 py-2 bg-[#1a1a1a] border border-[#222222] rounded text-[#f7931a] text-[12px] font-semibold whitespace-nowrap hover:opacity-80 disabled:opacity-40 disabled:cursor-not-allowed transition-opacity cursor-pointer"
          >
            {loadingMarkets ? 'Loading…' : 'Load ↻'}
          </button>
        </div>
        {marketHint && (
          <p className={`text-[11px] mt-1 ${marketHintColor}`}>{marketHint}</p>
        )}
      </div>

      {/* Strategy */}
      <div className="mb-1">
        <label className="field-label">Strategy</label>
        <select
          value={strategy}
          onChange={(e) => setStrategy(e.target.value)}
          className="field-input cursor-pointer"
        >
          {STRATEGIES.map(s => (
            <option key={s.value} value={s.value}>{s.label}</option>
          ))}
        </select>
        {selectedStratDesc && (
          <p className="text-[11px] text-[#555] mt-1 leading-relaxed">{selectedStratDesc}</p>
        )}
      </div>

      <div className="my-4 border-t border-[#1a1a1a]" />

      {/* Contracts */}
      <div className="mb-3.5">
        <label className="field-label">Contracts per trade</label>
        <div className="flex items-center gap-2.5">
          <input
            type="range" min={1} max={10} value={contracts}
            onChange={(e) => setContracts(Number(e.target.value))}
            className="flex-1 accent-[#f7931a]"
          />
          <span className="font-mono text-[13px] text-[#f7931a] min-w-[28px] text-right">{contracts}</span>
        </div>
      </div>

      {/* Poll interval */}
      <div className="mb-3.5">
        <label className="field-label">Poll interval (seconds)</label>
        <div className="flex items-center gap-2.5">
          <input
            type="range" min={10} max={300} step={5} value={pollSec}
            onChange={(e) => setPollSec(Number(e.target.value))}
            className="flex-1 accent-[#f7931a]"
          />
          <span className="font-mono text-[13px] text-[#f7931a] min-w-[28px] text-right">{pollSec}s</span>
        </div>
      </div>

      {/* Max trades */}
      <div className="mb-3.5">
        <label className="field-label">Max trades this session</label>
        <input
          type="number" value={maxTrades}
          onChange={(e) => setMaxTrades(Number(e.target.value))}
          min={1} max={100}
          className="field-input"
        />
      </div>

      <div className="my-4 border-t border-[#1a1a1a]" />
      <p className="section-title mb-3">Risk Controls</p>

      {/* Min expiry guard */}
      <div className="mb-3.5">
        <label className="field-label">
          Min time before expiry (minutes)
          <Tooltip text="Skip trading if the market expires within this many minutes. Near expiry, price swings are unpredictable." />
        </label>
        <div className="flex items-center gap-2.5">
          <input
            type="range" min={0} max={60} step={5} value={minExpiryMins}
            onChange={(e) => setMinExpiryMins(Number(e.target.value))}
            className="flex-1 accent-[#f7931a]"
          />
          <span className="font-mono text-[13px] text-[#f7931a] min-w-[28px] text-right">
            {minExpiryMins === 0 ? 'off' : `${minExpiryMins}m`}
          </span>
        </div>
      </div>

      {/* Max wallet pct */}
      <div className="mb-3.5">
        <label className="field-label">
          Max wallet % per trade
          <Tooltip text="Cap each trade cost as a percentage of your current wallet balance. Set to 0 to disable." />
        </label>
        <div className="flex items-center gap-2.5">
          <input
            type="range" min={0} max={50} step={1} value={maxWalletPct}
            onChange={(e) => setMaxWalletPct(Number(e.target.value))}
            className="flex-1 accent-[#f7931a]"
          />
          <span className="font-mono text-[13px] text-[#f7931a] min-w-[28px] text-right">
            {maxWalletPct === 0 ? 'off' : `${maxWalletPct}%`}
          </span>
        </div>
      </div>

      {/* No duplicates */}
      <div className="mb-3.5">
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox" checked={noDuplicates}
            onChange={(e) => setNoDuplicates(e.target.checked)}
            className="w-4 h-4 accent-[#f7931a] cursor-pointer"
          />
          <span className="text-[13px]">
            No duplicates
            <Tooltip text="Don't trade the same option range twice per session. Prevents buying the same position repeatedly when BTC isn't moving." />
          </span>
        </label>
      </div>

      {/* Dry run */}
      <div className="mb-3.5">
        <label className="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox" checked={dryRun}
            onChange={(e) => setDryRun(e.target.checked)}
            className="w-4 h-4 accent-[#f7931a] cursor-pointer"
          />
          <span className="text-[13px]">Dry run (log only, no real trades)</span>
        </label>
      </div>

      {/* Start / Stop */}
      <div className="flex gap-2.5 mt-5">
        <button
          onClick={handleStart}
          disabled={running}
          className="flex-1 py-2.5 px-4 rounded bg-[#f7931a] text-black text-[13px] font-semibold cursor-pointer hover:opacity-85 disabled:opacity-35 disabled:cursor-not-allowed transition-opacity"
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

      {/* Range Picker + Quick Trade */}
      <div className="mt-5 pt-4 border-t border-[#1a1a1a]">
        <div className="flex items-center justify-between mb-3">
          <p className="section-title">
            Pick Range to Trade
            <Tooltip text="Load the live quotes for the selected market, pick a specific range, then either let the bot always trade it or place a one-off trade immediately." />
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
              { }
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
              <p className="text-[11px] text-[#f7931a] mb-3">
                Bot will always trade this range. Turn off No Duplicates to repeat each tick.
              </p>
            )}

            <div className="mb-3">
              <label className="field-label">Contracts (for one-off trade)</label>
              <input
                type="number"
                value={quickContracts}
                onChange={(e) => setQuickContracts(Math.max(1, parseInt(e.target.value) || 1))}
                min={1} max={100}
                className="field-input"
              />
            </div>

            <div className="mb-3">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox" checked={quickDryRun}
                  onChange={(e) => setQuickDryRun(e.target.checked)}
                  className="w-4 h-4 accent-[#f7931a] cursor-pointer"
                />
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
    </div>
  );
}
