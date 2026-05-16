import type { TimeRange } from '../lib/types';

interface TimeRangeSelectorProps {
  value: TimeRange;
  onChange: (range: TimeRange) => void;
}

const RANGES: { value: TimeRange; label: string }[] = [
  { value: '5m', label: '5m' },
  { value: '15m', label: '15m' },
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '24h', label: '24h' },
];

export function TimeRangeSelector({ value, onChange }: TimeRangeSelectorProps) {
  return (
    <div className="flex items-center gap-1">
      {RANGES.map((range) => (
        <button
          key={range.value}
          onClick={() => onChange(range.value)}
          className={`px-3 py-1.5 text-sm rounded transition-colors ${
            value === range.value
              ? 'bg-slate-600 text-white'
              : 'bg-slate-700 text-slate-300 hover:bg-slate-600 hover:text-white'
          }`}
        >
          {range.label}
        </button>
      ))}
    </div>
  );
}
