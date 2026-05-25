'use client';

import { useState } from 'react';
import type { TradeRecord } from '@/lib/types';

interface Props {
  trades?: TradeRecord[];
}

function fmtSats(ms: number): string {
  if (!ms) return '0';
  const sats = Math.floor(ms / 1000);
  return sats === 0 ? '< 1' : sats.toLocaleString();
}

function fmtTime(ts: string | undefined): string {
  if (!ts || ts === '0001-01-01T00:00:00Z') return '—';
  return new Date(ts).toLocaleTimeString();
}

const COLUMNS = ['Time', 'Range', 'Option ID', 'Contracts', 'Cost (sats)', 'Comm (sats)', 'Trade ID', 'Mode'];

export default function TradeTable({ trades }: Props) {
  const rows = trades ? [...trades].reverse() : [];
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const toggle = (id: string) => {
    setExpanded(prev => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  };

  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-[12px]">
        <thead>
          <tr>
            {COLUMNS.map((col) => (
              <th
                key={col}
                className="text-left text-[#666666] font-semibold uppercase tracking-[0.6px] text-[10px] py-1.5 px-2.5 border-b border-[#222222] whitespace-nowrap"
              >
                {col}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={8} className="text-center text-[#666666] py-6 italic font-sans">
                No trades yet
              </td>
            </tr>
          ) : (
            rows.map((t, i) => {
              const key = t.trade_id ?? String(i);
              const isKelly = !!(t.kelly_legs && t.kelly_legs.length > 0);
              const isExpanded = expanded.has(key);

              return (
                <>
                  <tr
                    key={key}
                    onClick={() => isKelly && toggle(key)}
                    className={`border-b border-[#161616] last:border-0 transition-colors ${isKelly ? 'cursor-pointer hover:bg-[#0d1f1a]' : 'hover:bg-[#161616]'}`}
                  >
                    <td className="py-2 px-2.5 font-mono align-middle">{fmtTime(t.timestamp)}</td>
                    <td className="py-2 px-2.5 font-mono align-middle">
                      <span className={isKelly ? 'text-[#00c48c] font-semibold' : ''}>{t.range || '—'}</span>
                      {isKelly && (
                        <span className="ml-1.5 text-[10px] text-[#00c48c]/60">
                          {isExpanded ? '▲' : '▼'}
                        </span>
                      )}
                    </td>
                    <td className="py-2 px-2.5 font-mono align-middle text-[#666]">{t.option_id || '—'}</td>
                    <td className="py-2 px-2.5 font-mono align-middle">{t.contracts || '—'}</td>
                    <td className="py-2 px-2.5 font-mono align-middle">{fmtSats(t.cost_millisats ?? 0)}</td>
                    <td className="py-2 px-2.5 font-mono align-middle">{fmtSats(t.commission_millisats ?? 0)}</td>
                    <td className="py-2 px-2.5 font-mono align-middle text-[11px]" title={t.trade_id}>
                      {t.trade_id ? `${t.trade_id.substring(0, 12)}…` : '—'}
                    </td>
                    <td className="py-2 px-2.5 font-mono align-middle">
                      {isKelly ? (
                        <span className="text-[#00c48c] text-[10px] font-bold">KELLY</span>
                      ) : t.dry_run ? (
                        <span className="text-[#f5c518] text-[10px] font-bold">DRY</span>
                      ) : (
                        <span className="text-[#00c48c] text-[10px] font-bold">REAL</span>
                      )}
                    </td>
                  </tr>

                  { }
                  {isKelly && isExpanded && (
                    <tr key={`${key}-legs`} className="bg-[#0a1a14]">
                      <td colSpan={8} className="px-4 py-2">
                        <div className="text-[10px] text-[#00c48c]/60 uppercase tracking-wider mb-1.5">
                          Kelly Legs {t.dry_run && <span className="text-[#f5c518]">(dry)</span>}
                        </div>
                        <table className="w-full text-[11px] border-collapse">
                          <thead>
                            <tr>
                              {['Bin', 'Range', 'Contracts', 'Edge'].map(c => (
                                <th key={c} className="text-left text-[#555] font-semibold text-[10px] pb-1 pr-4 whitespace-nowrap">{c}</th>
                              ))}
                            </tr>
                          </thead>
                          <tbody>
                            {t.kelly_legs!.map(leg => (
                              <tr key={leg.bin_index}>
                                <td className="font-mono pr-4 pb-0.5 text-[#555]">#{leg.bin_index}</td>
                                <td className="font-mono pr-4 pb-0.5">{leg.range}</td>
                                <td className="font-mono pr-4 pb-0.5">{leg.contracts}</td>
                                <td className="font-mono pb-0.5 text-[#f7931a]">+{leg.edge_pct.toFixed(2)}%</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </td>
                    </tr>
                  )}
                </>
              );
            })
          )}
        </tbody>
      </table>
    </div>
  );
}
