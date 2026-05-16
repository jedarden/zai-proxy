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
import { formatLatency } from '../../lib/format';

interface LatencyPanelProps {
  data: MetricSnapshot[];
  variant: VariantFilter;
  height?: number;
}

interface ChartDataPoint {
  timestamp: number;
  time: string;
  // Production data
  latency_p50_prod?: number;
  latency_p95_prod?: number;
  latency_p99_prod?: number;
  // Canary data
  latency_p50_canary?: number;
  latency_p95_canary?: number;
  latency_p99_canary?: number;
  // Single variant
  latency_p50?: number;
  latency_p95?: number;
  latency_p99?: number;
}

const COLORS = {
  p50: '#3b82f6', // blue
  p95: '#f59e0b', // orange
  p99: '#ef4444', // red
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
          {entry.name}: {entry.value.toFixed(0)}ms
        </p>
      ))}
    </div>
  );
};

export function LatencyPanel({ data, variant, height = 180 }: LatencyPanelProps) {
  // Calculate current value from latest data
  const currentValue = useMemo(() => {
    if (data.length === 0) return null;
    const latest = data[data.length - 1];
    return latest.latency_p50;
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
          existing.latency_p50_prod = snapshot.latency_p50;
          existing.latency_p95_prod = snapshot.latency_p95;
          existing.latency_p99_prod = snapshot.latency_p99;
        } else {
          existing.latency_p50_canary = snapshot.latency_p50;
          existing.latency_p95_canary = snapshot.latency_p95;
          existing.latency_p99_canary = snapshot.latency_p99;
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
        latency_p50: snapshot.latency_p50,
        latency_p95: snapshot.latency_p95,
        latency_p99: snapshot.latency_p99,
      }));
    }
  }, [data, variant]);

  const yAxisFormatter = (value: number) => `${value.toFixed(0)}ms`;

  return (
    <div className="panel">
      <div className="flex items-start justify-between mb-2">
        <h3 className="panel-header">Latency (p50 / p95 / p99)</h3>
        {currentValue !== null && (
          <div className="text-right">
            <div className="panel-value">{formatLatency(currentValue)}</div>
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
              value: 'ms',
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
                dataKey="latency_p50_prod"
                name="p50 (prod)"
                stroke={COLORS.p50}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
              <Line
                type="monotone"
                dataKey="latency_p95_prod"
                name="p95 (prod)"
                stroke={COLORS.p95}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
              <Line
                type="monotone"
                dataKey="latency_p99_prod"
                name="p99 (prod)"
                stroke={COLORS.p99}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
              {/* Canary - dashed lines */}
              <Line
                type="monotone"
                dataKey="latency_p50_canary"
                name="p50 (canary)"
                stroke={COLORS.p50}
                strokeWidth={2}
                strokeDasharray="5 5"
                dot={false}
              />
              <Line
                type="monotone"
                dataKey="latency_p95_canary"
                name="p95 (canary)"
                stroke={COLORS.p95}
                strokeWidth={2}
                strokeDasharray="5 5"
                dot={false}
              />
              <Line
                type="monotone"
                dataKey="latency_p99_canary"
                name="p99 (canary)"
                stroke={COLORS.p99}
                strokeWidth={2}
                strokeDasharray="5 5"
                dot={false}
              />
            </>
          ) : (
            <>
              <Line
                type="monotone"
                dataKey="latency_p50"
                name="p50"
                stroke={COLORS.p50}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
              <Line
                type="monotone"
                dataKey="latency_p95"
                name="p95"
                stroke={COLORS.p95}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
              <Line
                type="monotone"
                dataKey="latency_p99"
                name="p99"
                stroke={COLORS.p99}
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
