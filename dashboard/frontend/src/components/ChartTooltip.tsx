import { useTheme } from '../contexts/ThemeContext';

interface ChartTooltipProps {
  active?: boolean;
  payload?: Array<{ name: string; value: number; color: string }>;
  label?: string;
  formatter?: (name: string, value: number) => string;
}

/**
 * Theme-aware tooltip component for Recharts charts.
 * Automatically adapts styling based on current theme.
 */
export function ChartTooltip({ active, payload, label, formatter }: ChartTooltipProps) {
  const { theme } = useTheme();

  if (!active || !payload) {
    return null;
  }

  return (
    <div
      className={`rounded-lg p-3 shadow-lg border ${
        theme === 'dark'
          ? 'bg-slate-800 border-slate-600'
          : 'bg-white border-slate-200'
      }`}
    >
      <p
        className={`text-xs mb-2 ${
          theme === 'dark' ? 'text-slate-400' : 'text-slate-500'
        }`}
      >
        {label}
      </p>
      {payload.map((entry, index) => {
        const displayValue = formatter
          ? formatter(entry.name, entry.value)
          : entry.value.toFixed(2);

        return (
          <p key={index} className="text-sm" style={{ color: entry.color }}>
            {entry.name}: {displayValue}
          </p>
        );
      })}
    </div>
  );
}

/**
 * Factory function to create a typed tooltip component with a custom formatter.
 * Useful for reducing boilerplate in panel components.
 */
export function createTooltip(
  formatter: (name: string, value: number) => string
) {
  return function FormattedTooltip(props: Omit<ChartTooltipProps, 'formatter'>) {
    return <ChartTooltip {...props} formatter={formatter} />;
  };
}
