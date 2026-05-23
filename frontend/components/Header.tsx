export default function Header() {
  return (
    <header className="flex items-center gap-3 mb-6">
      <h1 className="text-2xl font-bold tracking-tight">
        <span className="text-[#f7931a]">⚡</span>{' '}
        Glimpse <span className="text-[#f7931a]">Bot</span>
      </h1>
      <span className="text-[#666666] text-xs font-mono">v1.0.0</span>
    </header>
  );
}
