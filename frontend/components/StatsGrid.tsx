import type { ReactNode } from 'react';
import type { BotState, KellyTradeSummary } from '@/lib/types';

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

function KellyPanel({ trades, modelName, kellyFraction }: {
  trades: KellyTradeSummary[];
  modelName?: string;
  kellyFraction?: number;
}) {
  return (
    <div className="card px-4 py-3">
      <div className="flex items-center justify-between mb-2.5">
        <div className="text-[11px] text-[#00c48c] uppercase tracking-[0.8px] font-semibold">
          Kelly Recommendations
        </div>
        <div className="text-[11px] text-[#666] font-mono">
          {modelName && <span className="mr-3">{modelName}</span>}
          {kellyFraction != null && <span>c = {kellyFraction.toFixed(2)}</span>}
        </div>
      </div>
      {trades.length === 0 ? (
        <p className="text-[12px] text-[#555] italic">No underpriced bins found this tick.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-[12px]">
            <thead>
              <tr>
                {['Range', 'Market %', 'Model %', 'Edge', 'Contracts'].map(col => (
                  <th
                    key={col}
                    className="text-left text-[#666666] font-semibold uppercase tracking-[0.6px] text-[10px] py-1.5 px-2 border-b border-[#222222] whitespace-nowrap"
                  >
                    {col}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {trades.map(t => (
                <tr key={t.bin_index} className="border-b border-[#161616] last:border-0 hover:bg-[#161616] transition-colors">
                  <td className="py-1.5 px-2 font-mono">{t.range}</td>
                  <td className="py-1.5 px-2 font-mono text-[#666]">{t.market_pct.toFixed(2)}%</td>
                  <td className="py-1.5 px-2 font-mono text-[#00c48c]">{t.model_pct.toFixed(2)}%</td>
                  <td className="py-1.5 px-2 font-mono text-[#f7931a] font-bold">+{t.edge_pct.toFixed(2)}%</td>
                  <td className="py-1.5 px-2 font-mono">{t.contracts}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

export default function StatsGrid({ status }: Props) {
  const totalCostSats = status?.total_cost_millisats
    ? Math.floor(status.total_cost_millisats / 1000)
    : 0;

  const isKelly = !!(status?.model_name);

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

  if (isKelly) {
    stats.push({
      label: 'Mispriced Bins',
      value: String(status?.misprice_count ?? 0),
      valueClass: status?.misprice_count ? 'text-[#00c48c]' : 'text-[#666]',
      sub: 'this tick',
    });
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-[repeat(auto-fit,minmax(140px,1fr))] gap-3">
        {stats.map((s) => (
          <div key={s.label} className="card px-4 py-3">
            <div className="text-[11px] text-[#666666] uppercase tracking-[0.8px] mb-1.5">{s.label}</div>
            <div className={`font-mono text-xl font-bold text-[#e0e0e0] ${s.valueClass ?? ''}`}>{s.value}</div>
            {s.sub && <div className="text-[11px] text-[#666666] font-mono mt-0.5">{s.sub}</div>}
          </div>
        ))}
      </div>

      { }
      {isKelly && (
        <KellyPanel
          trades={status?.kelly_trades ?? []}
          modelName={status?.model_name}
          kellyFraction={status?.kelly_fraction}
        />
      )}
    </div>
  );
}
