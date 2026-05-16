import { useMemo } from 'react';
import {
  ResponsiveContainer,
  ComposedChart,
  Area,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ReferenceLine,
} from 'recharts';
import type { MetricSnapshot, VariantFilter } from '../../lib/types';
import { formatUtilization, getUtilizationColor } from '../../lib/format';

interface ConcurrencyPanelProps {
  data: MetricSnapshot[];
  variant: VariantFilter;
  height?: number;
}

interface ChartDataPoint {
  timestamp: number;
  time: string;
  max_workers: number;
  // Production data
  concurrent_requests_prod?: number;
  worker_utilization_prod?: number;
  // Canary data
  concurrent_requests_canary?: number;
  worker_utilization_canary?: number;
  // Single variant
  concurrent_requests?: number;
  worker_utilization?: number;
}

const COLORS = {
  concurrent: '#3b82f6', // blue
  max: '#64748b',        // slate
  utilization: '#22c55e', // green (changes based on value)
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
      {payload.map((entry, index) => {
        const unit = entry.name.includes('Utilization') ? '%' : 'req';
        const value = entry.name.includes('Utilization')
          ? (entry.value * 100).toFixed(0)
          : entry.value.toFixed(0);
        return (
          <p key={index} className="text-sm" style={{ color: entry.color }}>
            {entry.name}: {value}{unit}
          </p>
        );
      })}
    </div>
  );
};

export function ConcurrencyPanel({ data, variant, height = 180 }: ConcurrencyPanelProps) {
  // Calculate current values from latest data
  const currentValues = useMemo(() => {
    if (data.length === 0) return null;
    const latest = data[data.length - 1];
    return {
      concurrent: latest.concurrent_requests,
      max: latest.max_workers,
      utilization: latest.worker_utilization,
    };
  }, [data]);

  // Get max workers for reference line
  const maxWorkers = useMemo(() => {
    if (data.length === 0) return 0;
    return data[data.length - 1].max_workers;
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
          max_workers: snapshot.max_workers,
        };

        if (snapshot.variant === 'production') {
          existing.concurrent_requests_prod = snapshot.concurrent_requests;
          existing.worker_utilization_prod = snapshot.worker_utilization;
        } else {
          existing.concurrent_requests_canary = snapshot.concurrent_requests;
          existing.worker_utilization_canary = snapshot.worker_utilization;
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
        max_workers: snapshot.max_workers,
        concurrent_requests: snapshot.concurrent_requests,
        worker_utilization: snapshot.worker_utilization,
      }));
    }
  }, [data, variant]);

  const yAxisFormatter = (value: number) => value.toFixed(0);
  const utilizationFormatter = (value: number) => `${(value * 100).toFixed(0)}%`;

  return (
    <div className="panel">
      <div className="flex items-start justify-between mb-2">
        <h3 className="panel-header">Concurrency & Workers</h3>
        {currentValues && (
          <div className="text-right">
            <div className="panel-value">
              {currentValues.concurrent.toFixed(0)}/{currentValues.max.toFixed(0)}
            </div>
            <span className={`text-xs ${getUtilizationColor(currentValues.utilization)}`}>
              {formatUtilization(currentValues.utilization)} utilized
            </span>
          </div>
        )}
      </div>
      <ResponsiveContainer width="100%" height={height}>
        <ComposedChart data={chartData} margin={{ top: 5, right: 40, left: 0, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#334155" />
          <XAxis
            dataKey="time"
            stroke="#64748b"
            fontSize={11}
            tickLine={false}
            interval="preserveStartEnd"
          />
          <YAxis
            yAxisId="left"
            stroke="#64748b"
            fontSize={11}
            tickLine={false}
            tickFormatter={yAxisFormatter}
            label={{
              value: 'requests',
              angle: -90,
              position: 'insideLeft',
              style: { fill: '#64748b', fontSize: 11 },
            }}
          />
          <YAxis
            yAxisId="right"
            orientation="right"
            stroke="#64748b"
            fontSize={11}
            tickLine={false}
            tickFormatter={utilizationFormatter}
            domain={[0, 1]}
            label={{
              value: 'util %',
              angle: 90,
              position: 'insideRight',
              style: { fill: '#64748b', fontSize: 11 },
            }}
          />
          <Tooltip content={<CustomTooltip />} />
          <Legend wrapperStyle={{ fontSize: '11px' }} iconType="line" iconSize={10} />

          {/* Max workers reference line - dashed */}
          <ReferenceLine
            y={maxWorkers}
            yAxisId="left"
            stroke={COLORS.max}
            strokeDasharray="5 5"
            label={{
              value: 'Max',
              position: 'right',
              fill: COLORS.max,
              fontSize: 10,
            }}
          />

          {variant === 'both' ? (
            <>
              {/* Production - solid */}
              <Area
                yAxisId="left"
                type="monotone"
                dataKey="concurrent_requests_prod"
                name="Concurrent (prod)"
                stroke={COLORS.concurrent}
                fill={COLORS.concurrent}
                fillOpacity={0.2}
                strokeWidth={2}
                dot={false}
              />
              <Line
                yAxisId="right"
                type="monotone"
                dataKey="worker_utilization_prod"
                name="Utilization (prod)"
                stroke="#22c55e"
                strokeWidth={2}
                dot={false}
              />
              {/* Canary - dashed */}
              <Area
                yAxisId="left"
                type="monotone"
                dataKey="concurrent_requests_canary"
                name="Concurrent (canary)"
                stroke={COLORS.concurrent}
                fill="transparent"
                strokeWidth={2}
                strokeDasharray="5 5"
                dot={false}
              />
              <Line
                yAxisId="right"
                type="monotone"
                dataKey="worker_utilization_canary"
                name="Utilization (canary)"
                stroke="#22c55e"
                strokeWidth={2}
                strokeDasharray="5 5"
                dot={false}
              />
            </>
          ) : (
            <>
              <Area
                yAxisId="left"
                type="monotone"
                dataKey="concurrent_requests"
                name="Concurrent"
                stroke={COLORS.concurrent}
                fill={COLORS.concurrent}
                fillOpacity={0.3}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
              <Line
                yAxisId="right"
                type="monotone"
                dataKey="worker_utilization"
                name="Utilization"
                stroke="#22c55e"
                strokeWidth={2}
                dot={false}
              />
            </>
          )}
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  );
}
