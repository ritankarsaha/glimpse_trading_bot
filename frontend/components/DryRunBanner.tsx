interface Props {
  show: boolean;
}

export default function DryRunBanner({ show }: Props) {
  if (!show) return null;
  return (
    <div className="mb-5 rounded-md border border-[#f5c518] bg-[#f5c518]/10 px-4 py-2.5 text-center text-[13px] font-semibold text-[#f5c518] tracking-wide">
      ⚠ DRY RUN MODE — No real trades will be placed
    </div>
  );
}
