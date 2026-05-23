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
              <td
                colSpan={8}
                className="text-center text-[#666666] py-6 italic font-sans"
              >
                No trades yet
              </td>
            </tr>
          ) : (
            rows.map((t, i) => {
              const cost = fmtSats(t.cost_millisats ?? 0);
              const comm = fmtSats(t.commission_millisats ?? 0);
              const shortId = t.trade_id ? `${t.trade_id.substring(0, 12)}…` : '—';

              return (
                <tr
                  key={t.trade_id ?? i}
                  className="border-b border-[#161616] last:border-0 hover:bg-[#161616] transition-colors"
                >
                  <td className="py-2 px-2.5 font-mono align-middle">{fmtTime(t.timestamp)}</td>
                  <td className="py-2 px-2.5 font-mono align-middle">{t.range || '—'}</td>
                  <td className="py-2 px-2.5 font-mono align-middle">{t.option_id}</td>
                  <td className="py-2 px-2.5 font-mono align-middle">{t.contracts}</td>
                  <td className="py-2 px-2.5 font-mono align-middle">{cost}</td>
                  <td className="py-2 px-2.5 font-mono align-middle">{comm}</td>
                  <td
                    className="py-2 px-2.5 font-mono align-middle text-[11px]"
                    title={t.trade_id}
                  >
                    {shortId}
                  </td>
                  <td className="py-2 px-2.5 font-mono align-middle">
                    {t.dry_run ? (
                      <span className="text-[#f5c518] text-[10px] font-bold">DRY</span>
                    ) : (
                      <span className="text-[#00c48c] text-[10px] font-bold">REAL</span>
                    )}
                  </td>
                </tr>
              );
            })
          )}
        </tbody>
      </table>
    </div>
  );
}
