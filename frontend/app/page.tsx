'use client';

import { useState } from 'react';
import useSWR from 'swr';
import Header from '@/components/Header';
import ConfigCard from '@/components/ConfigCard';
import StatsGrid from '@/components/StatsGrid';
import LogBox from '@/components/LogBox';
import TradeTable from '@/components/TradeTable';
import DryRunBanner from '@/components/DryRunBanner';
import ErrorBanner from '@/components/ErrorBanner';
import type { BotState, TradeRecord } from '@/lib/types';
import { swrFetcher } from '@/lib/api';

export default function Dashboard() {
  const { data: status, mutate: mutateStatus } = useSWR<BotState>(
    '/api/status',
    swrFetcher,
    { refreshInterval: 2000, revalidateOnFocus: false }
  );

  const { data: trades } = useSWR<TradeRecord[]>(
    '/api/trades',
    swrFetcher,
    { refreshInterval: 2000, revalidateOnFocus: false }
  );

  const [localError, setLocalError] = useState('');

  const isRunning = status?.running ?? false;
  const errorMsg = localError || status?.last_error || '';

  return (
    <main className="min-h-screen bg-[#0a0a0a] text-[#e0e0e0] p-6">
      <Header />
      <DryRunBanner show={!!status?.running && !!status?.dry_run} />
      <ErrorBanner message={errorMsg} />

      <div className="grid grid-cols-1 md:grid-cols-[360px_1fr] gap-5 items-start">
        <ConfigCard
          running={isRunning}
          onStart={() => {
            setLocalError('');
            mutateStatus();
          }}
          onStop={() => {
            setLocalError('');
            mutateStatus();
          }}
          onError={setLocalError}
          onClearError={() => setLocalError('')}
        />

        <div className="flex flex-col gap-5">
          <StatsGrid status={status} />

          <div className="card p-4">
            <h2 className="section-title mb-2.5">Live Log</h2>
            <LogBox logs={status?.recent_logs} />
          </div>

          <div className="card p-4">
            <h2 className="section-title mb-2.5">Trade History</h2>
            <TradeTable trades={trades} />
          </div>
        </div>
      </div>
    </main>
  );
}
