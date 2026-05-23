interface Props {
  message?: string;
}

export default function ErrorBanner({ message }: Props) {
  if (!message) return null;
  return (
    <div className="mb-5 rounded-md border border-[#ff4d4d] bg-[#ff4d4d]/10 px-4 py-2.5 text-[12px] font-mono text-[#ff4d4d] break-all">
      ⚠ {message}
    </div>
  );
}
