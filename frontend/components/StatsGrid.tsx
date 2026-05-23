import type { ReactNode } from 'react';
import type { BotState } from '@/lib/types';

interface Props {
  status?: BotState;
}

function fmt(n: number | undefined | null): string {
  if (!n) return '—';
  return n.toLocaleString();
}

function fmtPrice(n: number | undefined | null): string {
  if (!n) return '—';
  return '$' + n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function StatusPill({ status }: { status?: BotState }) {
  if (!status?.running) return <span className="pill-stopped">stopped</span>;
  if (status.paused) return <span className="pill-paused">paused</span>;
  return <span className="pill-running">running</span>;
}

export default function StatsGrid({ status }: Props) {
  const totalCostSats = status?.total_cost_millisats
    ? Math.floor(status.total_cost_millisats / 1000)
    : 0;

  const stats: Array<{
    label: string;
    value: ReactNode;
    sub?: string;
    valueClass?: string;
  }> = [
    { label: 'Status', value: <StatusPill status={status} /> },
    {
      label: 'Wallet Balance',
      value: status?.wallet_balance ? fmt(status.wallet_balance) : '—',
      sub: 'sats',
      valueClass: 'text-[#f7931a]',
    },
    {
      label: 'BTC Spot',
      value: fmtPrice(status?.last_spot),
      sub: 'USD',
    },
    {
      label: 'Fear & Greed',
      value: status?.last_fng ? String(status.last_fng) : '—',
      sub: status?.last_fng_label || '—',
    },
    {
      label: 'Session Trades',
      value: String(status?.session_trades ?? 0),
      valueClass: 'text-[#00c48c]',
    },
    {
      label: 'Total Cost',
      value: totalCostSats ? fmt(totalCostSats) : '0',
      sub: 'sats',
    },
  ];

  return (
    <div className="grid grid-cols-[repeat(auto-fit,minmax(140px,1fr))] gap-3">
      {stats.map((s) => (
        <div key={s.label} className="card px-4 py-3">
          <div className="text-[11px] text-[#666666] uppercase tracking-[0.8px] mb-1.5">
            {s.label}
          </div>
          <div
            className={`font-mono text-xl font-bold text-[#e0e0e0] ${s.valueClass ?? ''}`}
          >
            {s.value}
          </div>
          {s.sub && (
            <div className="text-[11px] text-[#666666] font-mono mt-0.5">{s.sub}</div>
          )}
        </div>
      ))}
    </div>
  );
}
