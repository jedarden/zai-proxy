import type { VariantFilter } from '../lib/types';

interface VariantToggleProps {
  value: VariantFilter;
  onChange: (variant: VariantFilter) => void;
}

const VARIANTS: { value: VariantFilter; label: string; color: string }[] = [
  { value: 'production', label: 'Production', color: 'blue' },
  { value: 'canary', label: 'Canary', color: 'purple' },
  { value: 'both', label: 'Both', color: 'slate' },
];

export function VariantToggle({ value, onChange }: VariantToggleProps) {
  return (
    <div className="flex items-center gap-1">
      {VARIANTS.map((variant) => {
        const isActive = value === variant.value;
        const colorClasses = {
          blue: isActive
            ? 'bg-blue-500/20 text-blue-400 border-blue-500'
            : 'text-blue-400 hover:bg-blue-500/10',
          purple: isActive
            ? 'bg-purple-500/20 text-purple-400 border-purple-500'
            : 'text-purple-400 hover:bg-purple-500/10',
          slate: isActive
            ? 'bg-slate-600 text-white border-slate-500'
            : 'text-slate-300 hover:bg-slate-600',
        }[variant.color];

        return (
          <button
            key={variant.value}
            onClick={() => onChange(variant.value)}
            className={`px-3 py-1.5 text-sm rounded border transition-colors ${
              isActive ? 'border' : 'border-transparent'
            } ${colorClasses}`}
          >
            {variant.label}
          </button>
        );
      })}
    </div>
  );
}
