import { useMemo } from 'react';
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from 'recharts';
import type { MetricSnapshot, VariantFilter } from '../../lib/types';
import { formatRate } from '../../lib/format';

interface TokenPanelProps {
  data: MetricSnapshot[];
  variant: VariantFilter;
  height?: number;
}

interface ChartDataPoint {
  timestamp: number;
  time: string;
  // Production data
  token_rate_in_prod?: number;
  token_rate_out_prod?: number;
  // Canary data
  token_rate_in_canary?: number;
  token_rate_out_canary?: number;
  // Single variant
  token_rate_in?: number;
  token_rate_out?: number;
}

const COLORS = {
  input: '#06b6d4',  // cyan
  output: '#8b5cf6', // purple
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
          {entry.name}: {entry.value.toFixed(0)} tok/s
        </p>
      ))}
    </div>
  );
};

export function TokenPanel({ data, variant, height = 180 }: TokenPanelProps) {
  // Calculate current value from latest data
  const currentValue = useMemo(() => {
    if (data.length === 0) return null;
    const latest = data[data.length - 1];
    return latest.token_rate_in + latest.token_rate_out;
  }, [data]);

  // Transform data for chart
  const chartData = useMemo(() => {
    if (variant === 'both') {
      // Group by timestamp and separate by variant
      const grouped = new Map<number, ChartDataPoint>();

      for (const snapshot of data) {
        const existing = grouped.get(snapshot.timestamp) || {
          timestamp: snapshot.timestamp,
          time: new Date(snapshot.timestamp).toLocaleTimeString('en-US', {
            hour: '2-digit',
            minute: '2-digit',
            hour12: false,
          }),
        };

        if (snapshot.variant === 'production') {
          existing.token_rate_in_prod = snapshot.token_rate_in;
          existing.token_rate_out_prod = snapshot.token_rate_out;
        } else {
          existing.token_rate_in_canary = snapshot.token_rate_in;
          existing.token_rate_out_canary = snapshot.token_rate_out;
        }

        grouped.set(snapshot.timestamp, existing);
      }

      return Array.from(grouped.values()).sort((a, b) => a.timestamp - b.timestamp);
    } else {
      return data.map((snapshot) => ({
        timestamp: snapshot.timestamp,
        time: new Date(snapshot.timestamp).toLocaleTimeString('en-US', {
          hour: '2-digit',
          minute: '2-digit',
          hour12: false,
        }),
        token_rate_in: snapshot.token_rate_in,
        token_rate_out: snapshot.token_rate_out,
      }));
    }
  }, [data, variant]);

  const yAxisFormatter = (value: number) => {
    if (value >= 1000) {
      return `${(value / 1000).toFixed(1)}k`;
    }
    return value.toFixed(0);
  };

  return (
    <div className="panel">
      <div className="flex items-start justify-between mb-2">
        <h3 className="panel-header">Token Throughput</h3>
        {currentValue !== null && (
          <div className="text-right">
            <div className="panel-value">{formatRate(currentValue)}/s</div>
          </div>
        )}
      </div>
      <ResponsiveContainer width="100%" height={height}>
        <LineChart data={chartData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
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
              value: 'tok/s',
              angle: -90,
              position: 'insideLeft',
              style: { fill: '#64748b', fontSize: 11 },
            }}
          />
          <Tooltip content={<CustomTooltip />} />
          <Legend wrapperStyle={{ fontSize: '11px' }} iconType="line" iconSize={10} />

          {variant === 'both' ? (
            <>
              {/* Production - solid lines */}
              <Line
                type="monotone"
                dataKey="token_rate_in_prod"
                name="Input (prod)"
                stroke={COLORS.input}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
              <Line
                type="monotone"
                dataKey="token_rate_out_prod"
                name="Output (prod)"
                stroke={COLORS.output}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
              {/* Canary - dashed lines */}
              <Line
                type="monotone"
                dataKey="token_rate_in_canary"
                name="Input (canary)"
                stroke={COLORS.input}
                strokeWidth={2}
                strokeDasharray="5 5"
                dot={false}
              />
              <Line
                type="monotone"
                dataKey="token_rate_out_canary"
                name="Output (canary)"
                stroke={COLORS.output}
                strokeWidth={2}
                strokeDasharray="5 5"
                dot={false}
              />
            </>
          ) : (
            <>
              <Line
                type="monotone"
                dataKey="token_rate_in"
                name="Input"
                stroke={COLORS.input}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
              <Line
                type="monotone"
                dataKey="token_rate_out"
                name="Output"
                stroke={COLORS.output}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
            </>
          )}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
