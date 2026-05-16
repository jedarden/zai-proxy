import { useMemo } from 'react';
import {
  ResponsiveContainer,
  ComposedChart,
  Line,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
} from 'recharts';
import type { MetricSnapshot, VariantFilter } from '../../lib/types';
import { formatRate } from '../../lib/format';

interface RateLimitPanelProps {
  data: MetricSnapshot[];
  variant: VariantFilter;
  height?: number;
}

interface ChartDataPoint {
  timestamp: number;
  time: string;
  // Production data
  rate_limit_rps_prod?: number;
  rate_limit_adj_prod?: number;
  rate_limit_rejections_prod?: number;
  // Canary data
  rate_limit_rps_canary?: number;
  rate_limit_adj_canary?: number;
  rate_limit_rejections_canary?: number;
  // Single variant
  rate_limit_rps?: number;
  rate_limit_adj?: number;
  rate_limit_rejections?: number;
}

const COLORS = {
  rps: '#3b82f6',     // blue
  increase: '#22c55e', // green
  decrease: '#ef4444', // red
  rejection: '#f59e0b', // orange
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
        const value = entry.name.includes('Rejection')
          ? entry.value.toFixed(3)
          : entry.value.toFixed(1);
        const unit = entry.name.includes('Rejection') ? '/s' : '/s';
        return (
          <p key={index} className="text-sm" style={{ color: entry.color }}>
            {entry.name}: {value}{unit}
          </p>
        );
      })}
    </div>
  );
};

export function RateLimitPanel({ data, variant, height = 180 }: RateLimitPanelProps) {
  // Calculate current values from latest data
  const currentValues = useMemo(() => {
    if (data.length === 0) return null;
    const latest = data[data.length - 1];
    return {
      rps: latest.rate_limit_rps,
      rejections: latest.rate_limit_rejections,
    };
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

        // Calculate net adjustment (positive = increase, negative = decrease)
        const adj = snapshot.rate_limit_adj_increase - snapshot.rate_limit_adj_decrease;

        if (snapshot.variant === 'production') {
          existing.rate_limit_rps_prod = snapshot.rate_limit_rps;
          existing.rate_limit_adj_prod = adj;
          existing.rate_limit_rejections_prod = snapshot.rate_limit_rejections;
        } else {
          existing.rate_limit_rps_canary = snapshot.rate_limit_rps;
          existing.rate_limit_adj_canary = adj;
          existing.rate_limit_rejections_canary = snapshot.rate_limit_rejections;
        }

        grouped.set(snapshot.timestamp, existing);
      }

      return Array.from(grouped.values()).sort((a, b) => a.timestamp - b.timestamp);
    } else {
      return data.map((snapshot) => {
        const adj = snapshot.rate_limit_adj_increase - snapshot.rate_limit_adj_decrease;
        return {
          timestamp: snapshot.timestamp,
          time: new Date(snapshot.timestamp).toLocaleTimeString('en-US', {
            hour: '2-digit',
            minute: '2-digit',
            hour12: false,
          }),
          rate_limit_rps: snapshot.rate_limit_rps,
          rate_limit_adj: adj,
          rate_limit_rejections: snapshot.rate_limit_rejections,
        };
      });
    }
  }, [data, variant]);

  const yAxisFormatter = (value: number) => {
    if (value >= 1000) {
      return `${(value / 1000).toFixed(1)}k`;
    }
    return value.toFixed(1);
  };

  return (
    <div className="panel">
      <div className="flex items-start justify-between mb-2">
        <h3 className="panel-header">Rate Limiter</h3>
        {currentValues && (
          <div className="text-right">
            <div className="panel-value">{formatRate(currentValues.rps)}/s</div>
            {currentValues.rejections > 0 && (
              <span className="text-xs text-orange-400">
                {currentValues.rejections.toFixed(2)} rej/s
              </span>
            )}
          </div>
        )}
      </div>
      <ResponsiveContainer width="100%" height={height}>
        <ComposedChart data={chartData} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
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
          <Legend wrapperStyle={{ fontSize: '11px' }} iconType="line" iconSize={10} />

          {variant === 'both' ? (
            <>
              {/* Production - solid line */}
              <Line
                type="monotone"
                dataKey="rate_limit_rps_prod"
                name="Rate (prod)"
                stroke={COLORS.rps}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
              {/* Adjustment bars - production */}
              <Bar
                dataKey="rate_limit_adj_prod"
                name="Adjust (prod)"
                fill={COLORS.increase}
                barSize={3}
                opacity={0.7}
              />
              {/* Rejection rate */}
              <Line
                type="monotone"
                dataKey="rate_limit_rejections_prod"
                name="Rejections (prod)"
                stroke={COLORS.rejection}
                strokeWidth={1.5}
                dot={false}
              />
              {/* Canary - dashed line */}
              <Line
                type="monotone"
                dataKey="rate_limit_rps_canary"
                name="Rate (canary)"
                stroke={COLORS.rps}
                strokeWidth={2}
                strokeDasharray="5 5"
                dot={false}
              />
              {/* Adjustment bars - canary */}
              <Bar
                dataKey="rate_limit_adj_canary"
                name="Adjust (canary)"
                fill={COLORS.decrease}
                barSize={3}
                opacity={0.5}
              />
              {/* Rejection rate - canary */}
              <Line
                type="monotone"
                dataKey="rate_limit_rejections_canary"
                name="Rejections (canary)"
                stroke={COLORS.rejection}
                strokeWidth={1.5}
                strokeDasharray="5 5"
                dot={false}
              />
            </>
          ) : (
            <>
              <Line
                type="monotone"
                dataKey="rate_limit_rps"
                name="Rate Limit"
                stroke={COLORS.rps}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
              {/* Bar marks for adjustments (green up, red down shown via bar color) */}
              <Bar
                dataKey="rate_limit_adj"
                name="Adjustments"
                fill={COLORS.increase}
                barSize={4}
              />
              <Line
                type="monotone"
                dataKey="rate_limit_rejections"
                name="Rejection Rate"
                stroke={COLORS.rejection}
                strokeWidth={1.5}
                dot={false}
              />
            </>
          )}
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  );
}
