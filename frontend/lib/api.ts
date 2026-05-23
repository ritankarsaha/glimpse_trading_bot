import type { MarketSummary, Outcome, StartPayload, QuickTradePayload, QuickTradeResult } from './types';

const BASE = '/api';

async function handleResponse(r: Response) {
  if (!r.ok) {
    const j = await r.json().catch(() => ({})) as { error?: string };
    throw new Error(j.error ?? `HTTP ${r.status}`);
  }
  return r.json();
}

export async function postToken(token: string): Promise<void> {
  await handleResponse(
    await fetch(`${BASE}/token`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    })
  );
}

export async function fetchMarkets(): Promise<MarketSummary[]> {
  return handleResponse(await fetch(`${BASE}/markets`));
}

export async function fetchQuotes(topicId: number): Promise<Outcome[]> {
  return handleResponse(await fetch(`${BASE}/quotes?topic_id=${topicId}`));
}

export async function startBot(payload: StartPayload): Promise<void> {
  await handleResponse(
    await fetch(`${BASE}/start`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
  );
}

export async function stopBot(): Promise<void> {
  await handleResponse(await fetch(`${BASE}/stop`, { method: 'POST' }));
}

export async function quickTrade(payload: QuickTradePayload): Promise<QuickTradeResult> {
  return handleResponse(
    await fetch(`${BASE}/quicktrade`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
  );
}

export async function swrFetcher<T>(url: string): Promise<T> {
  const r = await fetch(url);
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json();
}
