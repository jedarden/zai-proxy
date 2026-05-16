import { useMemo } from 'react';
import {
  ResponsiveContainer,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from 'recharts';
import type { MetricSnapshot, VariantFilter } from '../../lib/types';
import { formatRate } from '../../lib/format';

interface RequestRatePanelProps {
  data: MetricSnapshot[];
  variant: VariantFilter;
  height?: number;
}

interface ChartDataPoint {
  timestamp: number;
  time: string;
  [key: string]: number | string;
}

// Assign a color to a status code based on its HTTP family
const getCodeColor = (code: string): string => {
  const family = code.charAt(0);
  const idx = (parseInt(code) % 100) % 5;

  const palettes: Record<string, string[]> = {
    '2': ['#22c55e', '#16a34a', '#4ade80', '#15803d', '#86efac'],
    '4': ['#eab308', '#ca8a04', '#fbbf24', '#f59e0b', '#d97706'],
    '5': ['#ef4444', '#dc2626', '#f87171', '#b91c1c', '#fca5a5'],
  };

  const palette = palettes[family] ?? ['#94a3b8', '#64748b', '#475569', '#334155', '#1e293b'];
  return palette[idx] ?? palette[0];
};

const CustomTooltip = ({
  active,
  payload,
  label,
}: {
  active?: boolean;
  payload?: Array<{ name: string; value: number; color: string }>;
  label?: string;
}) => {
  if (!active || !payload) {
    return null;
  }

  return (
    <div className="bg-slate-800 border border-slate-600 rounded-lg p-3 shadow-lg">
      <p className="text-slate-400 text-xs mb-2">{label}</p>
      {payload.map((entry, index) => (
        <p key={index} className="text-sm" style={{ color: entry.color }}>
          {entry.name}: {entry.value.toFixed(2)} req/s
        </p>
      ))}
    </div>
  );
};

export function RequestRatePanel({ data, variant, height = 180 }: RequestRatePanelProps) {
  const currentValue = useMemo(() => {
    if (data.length === 0) return null;
    return data[data.length - 1].req_rate;
  }, [data]);

  // Collect all unique status codes seen across the dataset
  const allCodes = useMemo(() => {
    const codes = new Set<string>();
    for (const snapshot of data) {
      if (snapshot.status_code_rates) {
        for (const code of Object.keys(snapshot.status_code_rates)) {
          codes.add(code);
        }
      }
    }
    return Array.from(codes).sort();
  }, [data]);

  const chartData = useMemo(() => {
    if (variant === 'both') {
      const grouped = new Map<number, ChartDataPoint>();

      for (const snapshot of data) {
        const existing: ChartDataPoint = grouped.get(snapshot.timestamp) ?? {
          timestamp: snapshot.timestamp,
          time: new Date(snapshot.timestamp).toLocaleTimeString('en-US', {
            hour: '2-digit',
            minute: '2-digit',
            hour12: false,
          }),
        };

        const rates = snapshot.status_code_rates ?? {};
        const suffix = snapshot.variant === 'production' ? '_prod' : '_canary';
        for (const code of allCodes) {
          existing[`${code}${suffix}`] = rates[code] ?? 0;
        }

        grouped.set(snapshot.timestamp, existing);
      }

      return Array.from(grouped.values()).sort((a, b) => a.timestamp - b.timestamp);
    } else {
      return data.map((snapshot) => {
        const rates = snapshot.status_code_rates ?? {};
        const point: ChartDataPoint = {
          timestamp: snapshot.timestamp,
          time: new Date(snapshot.timestamp).toLocaleTimeString('en-US', {
            hour: '2-digit',
            minute: '2-digit',
            hour12: false,
          }),
        };
        for (const code of allCodes) {
          point[code] = rates[code] ?? 0;
        }
        return point;
      });
    }
  }, [data, variant, allCodes]);

  const yAxisFormatter = (value: number) => {
    if (value >= 1000) {
      return `${(value / 1000).toFixed(1)}k`;
    }
    return value.toFixed(0);
  };

  return (
    <div className="panel">
      <div className="flex items-start justify-between mb-2">
        <h3 className="panel-header">Request Rate</h3>
        {currentValue !== null && (
          <div className="text-right">
            <div className="panel-value">{formatRate(currentValue)}/s</div>
          </div>
        )}
      </div>
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={chartData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#334155" />
          <XAxis
            dataKey="time"
            stroke="#64748b"
            fontSize={11}
            tickLine={false}
            interval="preserveStartEnd"
          />
          <YAxis
            stroke="#64748b"
            fontSize={11}
            tickLine={false}
            tickFormatter={yAxisFormatter}
            label={{
              value: 'req/s',
              angle: -90,
              position: 'insideLeft',
              style: { fill: '#64748b', fontSize: 11 },
            }}
          />
          <Tooltip content={<CustomTooltip />} />
          <Legend wrapperStyle={{ fontSize: '11px' }} iconType="circle" iconSize={8} />

          {variant === 'both' ? (
            <>
              {allCodes.map((code) => {
                const color = getCodeColor(code);
                return (
                  <Area
                    key={`${code}_prod`}
                    type="monotone"
                    dataKey={`${code}_prod`}
                    name={`${code} (prod)`}
                    stroke={color}
                    fill={color}
                    fillOpacity={0.2}
                    strokeWidth={2}
                    stackId="prod"
                    dot={false}
                  />
                );
              })}
              {allCodes.map((code) => {
                const color = getCodeColor(code);
                return (
                  <Area
                    key={`${code}_canary`}
                    type="monotone"
                    dataKey={`${code}_canary`}
                    name={`${code} (canary)`}
                    stroke={color}
                    fill="transparent"
                    strokeWidth={2}
                    strokeDasharray="5 5"
                    stackId="canary"
                    dot={false}
                  />
                );
              })}
            </>
          ) : (
            <>
              {allCodes.map((code) => {
                const color = getCodeColor(code);
                return (
                  <Area
                    key={code}
                    type="monotone"
                    dataKey={code}
                    name={code}
                    stroke={color}
                    fill={color}
                    fillOpacity={0.3}
                    strokeWidth={2}
                    stackId="stack"
                    dot={false}
                    activeDot={{ r: 4 }}
                  />
                );
              })}
            </>
          )}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
