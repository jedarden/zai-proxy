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
import { formatPercent, getErrorRateColor } from '../../lib/format';

interface ErrorPanelProps {
  data: MetricSnapshot[];
  variant: VariantFilter;
  height?: number;
}

interface ChartDataPoint {
  timestamp: number;
  time: string;
  // Production data
  upstream_errors_prod?: number;
  retry_attempts_prod?: number;
  error_rate_pct_prod?: number;
  // Canary data
  upstream_errors_canary?: number;
  retry_attempts_canary?: number;
  error_rate_pct_canary?: number;
  // Single variant
  upstream_errors?: number;
  retry_attempts?: number;
  error_rate_pct?: number;
}

const COLORS = {
  upstream: '#ef4444', // red
  retry: '#f59e0b',    // orange
  rate: '#ef4444',     // red for error rate line
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
        const value = entry.name.includes('Rate')
          ? `${entry.value.toFixed(2)}%`
          : entry.value.toFixed(0);
        return (
          <p key={index} className="text-sm" style={{ color: entry.color }}>
            {entry.name}: {value}
          </p>
        );
      })}
    </div>
  );
};

export function ErrorPanel({ data, variant, height = 180 }: ErrorPanelProps) {
  // Calculate current values from latest data
  const currentValues = useMemo(() => {
    if (data.length === 0) return null;
    const latest = data[data.length - 1];
    return {
      errorRate: latest.error_rate_pct,
      upstreamErrors: latest.upstream_errors,
      retries: latest.retry_attempts,
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

        if (snapshot.variant === 'production') {
          existing.upstream_errors_prod = snapshot.upstream_errors;
          existing.retry_attempts_prod = snapshot.retry_attempts;
          existing.error_rate_pct_prod = snapshot.error_rate_pct;
        } else {
          existing.upstream_errors_canary = snapshot.upstream_errors;
          existing.retry_attempts_canary = snapshot.retry_attempts;
          existing.error_rate_pct_canary = snapshot.error_rate_pct;
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
        upstream_errors: snapshot.upstream_errors,
        retry_attempts: snapshot.retry_attempts,
        error_rate_pct: snapshot.error_rate_pct,
      }));
    }
  }, [data, variant]);

  const yAxisFormatter = (value: number) => value.toFixed(0);

  return (
    <div className="panel">
      <div className="flex items-start justify-between mb-2">
        <h3 className="panel-header">Errors & Retries</h3>
        {currentValues && (
          <div className="text-right">
            <div className={`panel-value ${getErrorRateColor(currentValues.errorRate).split(' ')[1]}`}>
              {formatPercent(currentValues.errorRate)}
            </div>
            {currentValues.upstreamErrors > 0 && (
              <span className="text-xs text-slate-400">
                {currentValues.upstreamErrors.toFixed(0)} upstream, {currentValues.retries.toFixed(0)} retries
              </span>
            )}
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
              value: 'count',
              angle: -90,
              position: 'insideLeft',
              style: { fill: '#64748b', fontSize: 11 },
            }}
          />
          <Tooltip content={<CustomTooltip />} />
          <Legend wrapperStyle={{ fontSize: '11px' }} iconType="circle" iconSize={8} />

          {variant === 'both' ? (
            <>
              {/* Production - solid areas */}
              <Area
                type="monotone"
                dataKey="upstream_errors_prod"
                name="Upstream (prod)"
                stroke={COLORS.upstream}
                fill={COLORS.upstream}
                fillOpacity={0.2}
                strokeWidth={2}
                stackId="prod"
                dot={false}
              />
              <Area
                type="monotone"
                dataKey="retry_attempts_prod"
                name="Retries (prod)"
                stroke={COLORS.retry}
                fill={COLORS.retry}
                fillOpacity={0.2}
                strokeWidth={2}
                stackId="prod"
                dot={false}
              />
              {/* Canary - dashed areas */}
              <Area
                type="monotone"
                dataKey="upstream_errors_canary"
                name="Upstream (canary)"
                stroke={COLORS.upstream}
                fill="transparent"
                strokeWidth={2}
                strokeDasharray="5 5"
                stackId="canary"
                dot={false}
              />
              <Area
                type="monotone"
                dataKey="retry_attempts_canary"
                name="Retries (canary)"
                stroke={COLORS.retry}
                fill="transparent"
                strokeWidth={2}
                strokeDasharray="5 5"
                stackId="canary"
                dot={false}
              />
            </>
          ) : (
            <>
              <Area
                type="monotone"
                dataKey="upstream_errors"
                name="Upstream Errors"
                stroke={COLORS.upstream}
                fill={COLORS.upstream}
                fillOpacity={0.3}
                strokeWidth={2}
                stackId="stack"
                dot={false}
                activeDot={{ r: 4 }}
              />
              <Area
                type="monotone"
                dataKey="retry_attempts"
                name="Retry Attempts"
                stroke={COLORS.retry}
                fill={COLORS.retry}
                fillOpacity={0.3}
                strokeWidth={2}
                stackId="stack"
                dot={false}
                activeDot={{ r: 4 }}
              />
            </>
          )}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
