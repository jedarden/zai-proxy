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
  // Single-variant keys
  token_rate_in?: number;
  token_rate_out?: number;
  token_rate_cache_read?: number;
  token_rate_cache_write?: number;
  // Both-variant keys (prod)
  token_rate_in_prod?: number;
  token_rate_out_prod?: number;
  token_rate_cache_read_prod?: number;
  token_rate_cache_write_prod?: number;
  // Both-variant keys (canary)
  token_rate_in_canary?: number;
  token_rate_out_canary?: number;
  token_rate_cache_read_canary?: number;
  token_rate_cache_write_canary?: number;
}

const COLORS = {
  input:       '#06b6d4', // cyan
  output:      '#8b5cf6', // purple
  cache_read:  '#f59e0b', // amber
  cache_write: '#10b981', // emerald
};

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return n.toFixed(0);
}

const CustomTooltip = ({
  active,
  payload,
  label,
}: {
  active?: boolean;
  payload?: Array<{ name: string; value: number; color: string }>;
  label?: string;
}) => {
  if (!active || !payload) return null;
  return (
    <div className="bg-slate-800 border border-slate-600 rounded-lg p-3 shadow-lg">
      <p className="text-slate-400 text-xs mb-2">{label}</p>
      {payload.map((entry, i) => (
        <p key={i} className="text-sm" style={{ color: entry.color }}>
          {entry.name}: {entry.value.toFixed(0)} tok/s
        </p>
      ))}
    </div>
  );
};

/** Compute running totals over the visible window from cumulative counter values. */
function windowTotal(data: MetricSnapshot[], field: keyof MetricSnapshot): number {
  if (data.length < 2) return 0;
  const first = data[0][field] as number;
  const last = data[data.length - 1][field] as number;
  const delta = last - first;
  return delta >= 0 ? delta : last; // guard against counter resets
}

export function TokenPanel({ data, variant, height = 180 }: TokenPanelProps) {
  // Running totals over the visible window
  const totals = useMemo(() => {
    const filtered = variant === 'both' ? data : data.filter((s) => s.variant === variant);
    return {
      input:       windowTotal(filtered, 'tokens_input'),
      output:      windowTotal(filtered, 'tokens_output'),
      cache_read:  windowTotal(filtered, 'tokens_cache_read'),
      cache_write: windowTotal(filtered, 'tokens_cache_write'),
    };
  }, [data, variant]);

  // Duration label for the summary (window span in hours)
  const windowHours = useMemo(() => {
    if (data.length < 2) return null;
    const spanMs = data[data.length - 1].timestamp - data[0].timestamp;
    const hrs = spanMs / 3_600_000;
    return hrs >= 1 ? `${hrs.toFixed(0)}h` : `${Math.round(hrs * 60)}m`;
  }, [data]);

  const chartData = useMemo<ChartDataPoint[]>(() => {
    if (variant === 'both') {
      const grouped = new Map<number, ChartDataPoint>();
      for (const s of data) {
        const pt = grouped.get(s.timestamp) ?? {
          timestamp: s.timestamp,
          time: new Date(s.timestamp).toLocaleTimeString('en-US', {
            hour: '2-digit', minute: '2-digit', hour12: false,
          }),
        };
        if (s.variant === 'production') {
          pt.token_rate_in_prod = s.token_rate_in;
          pt.token_rate_out_prod = s.token_rate_out;
          pt.token_rate_cache_read_prod = s.token_rate_cache_read;
          pt.token_rate_cache_write_prod = s.token_rate_cache_write;
        } else {
          pt.token_rate_in_canary = s.token_rate_in;
          pt.token_rate_out_canary = s.token_rate_out;
          pt.token_rate_cache_read_canary = s.token_rate_cache_read;
          pt.token_rate_cache_write_canary = s.token_rate_cache_write;
        }
        grouped.set(s.timestamp, pt);
      }
      return Array.from(grouped.values()).sort((a, b) => a.timestamp - b.timestamp);
    }
    return data.map((s) => ({
      timestamp: s.timestamp,
      time: new Date(s.timestamp).toLocaleTimeString('en-US', {
        hour: '2-digit', minute: '2-digit', hour12: false,
      }),
      token_rate_in:         s.token_rate_in,
      token_rate_out:        s.token_rate_out,
      token_rate_cache_read: s.token_rate_cache_read,
      token_rate_cache_write: s.token_rate_cache_write,
    }));
  }, [data, variant]);

  const yAxisFormatter = (v: number) =>
    v >= 1000 ? `${(v / 1000).toFixed(1)}k` : v.toFixed(0);

  return (
    <div className="panel">
      {/* Header: title + running totals summary */}
      <div className="flex items-start justify-between mb-2">
        <h3 className="panel-header">Token Throughput</h3>
        {windowHours && (
          <span className="text-xs text-slate-500">{windowHours} window</span>
        )}
      </div>

      {/* Running totals grid */}
      <div className="grid grid-cols-4 gap-1 mb-3 text-center">
        <div>
          <div className="text-xs font-medium" style={{ color: COLORS.input }}>Input</div>
          <div className="font-mono text-xs text-slate-200">{formatTokenCount(totals.input)}</div>
        </div>
        <div>
          <div className="text-xs font-medium" style={{ color: COLORS.output }}>Output</div>
          <div className="font-mono text-xs text-slate-200">{formatTokenCount(totals.output)}</div>
        </div>
        <div>
          <div className="text-xs font-medium" style={{ color: COLORS.cache_read }}>Cache↑</div>
          <div className="font-mono text-xs text-slate-200">{formatTokenCount(totals.cache_read)}</div>
        </div>
        <div>
          <div className="text-xs font-medium" style={{ color: COLORS.cache_write }}>Cache↓</div>
          <div className="font-mono text-xs text-slate-200">{formatTokenCount(totals.cache_write)}</div>
        </div>
      </div>

      {/* Rate time series */}
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
              <Line type="monotone" dataKey="token_rate_in_prod"         name="In (prod)"         stroke={COLORS.input}       strokeWidth={2} dot={false} activeDot={{ r: 3 }} />
              <Line type="monotone" dataKey="token_rate_out_prod"        name="Out (prod)"        stroke={COLORS.output}      strokeWidth={2} dot={false} activeDot={{ r: 3 }} />
              <Line type="monotone" dataKey="token_rate_cache_read_prod" name="Cache↑ (prod)"     stroke={COLORS.cache_read}  strokeWidth={1} dot={false} strokeDasharray="4 2" />
              <Line type="monotone" dataKey="token_rate_cache_write_prod" name="Cache↓ (prod)"    stroke={COLORS.cache_write} strokeWidth={1} dot={false} strokeDasharray="4 2" />
              <Line type="monotone" dataKey="token_rate_in_canary"       name="In (canary)"       stroke={COLORS.input}       strokeWidth={2} dot={false} strokeDasharray="5 5" />
              <Line type="monotone" dataKey="token_rate_out_canary"      name="Out (canary)"      stroke={COLORS.output}      strokeWidth={2} dot={false} strokeDasharray="5 5" />
              <Line type="monotone" dataKey="token_rate_cache_read_canary" name="Cache↑ (canary)" stroke={COLORS.cache_read}  strokeWidth={1} dot={false} strokeDasharray="2 4" />
              <Line type="monotone" dataKey="token_rate_cache_write_canary" name="Cache↓ (canary)" stroke={COLORS.cache_write} strokeWidth={1} dot={false} strokeDasharray="2 4" />
            </>
          ) : (
            <>
              <Line type="monotone" dataKey="token_rate_in"         name="Input"      stroke={COLORS.input}       strokeWidth={2} dot={false} activeDot={{ r: 4 }} />
              <Line type="monotone" dataKey="token_rate_out"        name="Output"     stroke={COLORS.output}      strokeWidth={2} dot={false} activeDot={{ r: 4 }} />
              <Line type="monotone" dataKey="token_rate_cache_read" name="Cache Read"  stroke={COLORS.cache_read}  strokeWidth={1} dot={false} strokeDasharray="4 2" />
              <Line type="monotone" dataKey="token_rate_cache_write" name="Cache Write" stroke={COLORS.cache_write} strokeWidth={1} dot={false} strokeDasharray="4 2" />
            </>
          )}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
