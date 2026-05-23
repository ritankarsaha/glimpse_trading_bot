'use client';

import { useEffect, useRef } from 'react';

interface Props {
  logs?: string[];
}

function logClass(line: string): string {
  if (line.includes('[ERROR]') || line.includes('[PAUSED]')) return 'text-[#ff4d4d]';
  if (line.includes('[DRY RUN]')) return 'text-[#f5c518]';
  if (line.includes('Trade placed')) return 'text-[#00c48c]';
  return 'text-[#aaaaaa]';
}

export default function LogBox({ logs }: Props) {
  const boxRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (boxRef.current) {
      boxRef.current.scrollTop = boxRef.current.scrollHeight;
    }
  }, [logs]);

  return (
    <div
      ref={boxRef}
      className="bg-[#060606] border border-[#222222] rounded-md p-3 h-56 overflow-y-auto font-mono text-[12px] leading-[1.7] scroll-smooth"
    >
      {!logs || logs.length === 0 ? (
        <span className="text-[#666666]">Waiting for bot to start…</span>
      ) : (
        logs.map((line, i) => (
          <div key={i} className={`whitespace-pre-wrap ${logClass(line)}`}>
            {line}
          </div>
        ))
      )}
    </div>
  );
}
