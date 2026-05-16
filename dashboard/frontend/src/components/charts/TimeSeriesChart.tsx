import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  Area,
  AreaChart,
  ReferenceLine,
} from 'recharts';

interface DataPoint {
  timestamp: number;
  time: string;
  [key: string]: number | string;
}

interface SeriesConfig {
  key: string;
  name: string;
  color: string;
  type?: 'line' | 'area';
  strokeDasharray?: string;
}

interface TimeSeriesChartProps {
  data: DataPoint[];
  series: SeriesConfig[];
  yAxisLabel?: string;
  yAxisFormatter?: (value: number) => string;
  height?: number;
  showLegend?: boolean;
  showGrid?: boolean;
  stacked?: boolean;
  referenceLines?: Array<{
    value: number;
    label?: string;
    color: string;
    strokeDasharray?: string;
  }>;
  variant?: 'line' | 'area';
}

const defaultYAxisFormatter = (value: number) => {
  if (value >= 1000) {
    return `${(value / 1000).toFixed(1)}k`;
  }
  return value.toFixed(0);
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
          {entry.name}: {entry.value.toFixed(2)}
        </p>
      ))}
    </div>
  );
};

export function TimeSeriesChart({
  data,
  series,
  yAxisLabel,
  yAxisFormatter = defaultYAxisFormatter,
  height = 200,
  showLegend = true,
  showGrid = true,
  stacked = false,
  referenceLines = [],
  variant = 'line',
}: TimeSeriesChartProps) {
  return (
    <ResponsiveContainer width="100%" height={height}>
      {variant === 'area' ? (
        <AreaChart data={data} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
          {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#334155" />}
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
            label={
              yAxisLabel
                ? {
                    value: yAxisLabel,
                    angle: -90,
                    position: 'insideLeft',
                    style: { fill: '#64748b', fontSize: 11 },
                  }
                : undefined
            }
          />
          <Tooltip content={<CustomTooltip />} />
          {showLegend && (
            <Legend
              wrapperStyle={{ fontSize: '11px' }}
              iconType="line"
              iconSize={10}
            />
          )}
          {referenceLines.map((ref, index) => (
            <ReferenceLine
              key={index}
              y={ref.value}
              stroke={ref.color}
              strokeDasharray={ref.strokeDasharray || '5 5'}
              label={
                ref.label
                  ? {
                      value: ref.label,
                      position: 'right',
                      style: { fill: ref.color, fontSize: 10 },
                    }
                  : undefined
              }
            />
          ))}
          {series.map((s) => (
            <Area
              key={s.key}
              type="monotone"
              dataKey={s.key}
              name={s.name}
              stroke={s.color}
              fill={s.color}
              fillOpacity={0.3}
              strokeWidth={2}
              strokeDasharray={s.strokeDasharray}
              stackId={stacked ? 'stack' : undefined}
              dot={false}
              activeDot={{ r: 4 }}
            />
          ))}
        </AreaChart>
      ) : (
        <LineChart data={data} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
          {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#334155" />}
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
            label={
              yAxisLabel
                ? {
                    value: yAxisLabel,
                    angle: -90,
                    position: 'insideLeft',
                    style: { fill: '#64748b', fontSize: 11 },
                  }
                : undefined
            }
          />
          <Tooltip content={<CustomTooltip />} />
          {showLegend && (
            <Legend
              wrapperStyle={{ fontSize: '11px' }}
              iconType="line"
              iconSize={10}
            />
          )}
          {referenceLines.map((ref, index) => (
            <ReferenceLine
              key={index}
              y={ref.value}
              stroke={ref.color}
              strokeDasharray={ref.strokeDasharray || '5 5'}
              label={
                ref.label
                  ? {
                      value: ref.label,
                      position: 'right',
                      style: { fill: ref.color, fontSize: 10 },
                    }
                  : undefined
              }
            />
          ))}
          {series.map((s) => (
            <Line
              key={s.key}
              type="monotone"
              dataKey={s.key}
              name={s.name}
              stroke={s.color}
              strokeWidth={2}
              strokeDasharray={s.strokeDasharray}
              dot={false}
              activeDot={{ r: 4 }}
            />
          ))}
        </LineChart>
      )}
    </ResponsiveContainer>
  );
}

// Pre-configured chart components

interface RequestRateChartProps {
  data: DataPoint[];
  height?: number;
}

export function RequestRateChart({ data, height = 200 }: RequestRateChartProps) {
  return (
    <TimeSeriesChart
      data={data}
      series={[
        { key: 'requests_2xx', name: '2xx', color: '#22c55e', type: 'area' },
        { key: 'requests_4xx', name: '4xx', color: '#eab308', type: 'area' },
        { key: 'requests_5xx', name: '5xx', color: '#ef4444', type: 'area' },
      ]}
      yAxisLabel="req/s"
      height={height}
      stacked
      variant="area"
    />
  );
}

interface LatencyChartProps {
  data: DataPoint[];
  height?: number;
}

export function LatencyChart({ data, height = 200 }: LatencyChartProps) {
  return (
    <TimeSeriesChart
      data={data}
      series={[
        { key: 'latency_p50', name: 'p50', color: '#3b82f6' },
        { key: 'latency_p95', name: 'p95', color: '#f59e0b' },
        { key: 'latency_p99', name: 'p99', color: '#ef4444' },
      ]}
      yAxisLabel="ms"
      yAxisFormatter={(v) => `${v.toFixed(0)}ms`}
      height={height}
    />
  );
}

interface TokenChartProps {
  data: DataPoint[];
  height?: number;
}

export function TokenChart({ data, height = 200 }: TokenChartProps) {
  return (
    <TimeSeriesChart
      data={data}
      series={[
        { key: 'token_rate_in', name: 'Input', color: '#06b6d4' },
        { key: 'token_rate_out', name: 'Output', color: '#8b5cf6' },
      ]}
      yAxisLabel="tokens/s"
      height={height}
    />
  );
}

interface ConcurrencyChartProps {
  data: DataPoint[];
  maxWorkers?: number;
  height?: number;
}

export function ConcurrencyChart({ data, maxWorkers, height = 200 }: ConcurrencyChartProps) {
  const referenceLines = maxWorkers
    ? [{ value: maxWorkers, label: 'Max', color: '#64748b' }]
    : [];

  return (
    <TimeSeriesChart
      data={data}
      series={[
        { key: 'concurrent_requests', name: 'Concurrent', color: '#3b82f6', type: 'area' },
      ]}
      yAxisLabel="requests"
      height={height}
      referenceLines={referenceLines}
      variant="area"
      showLegend={false}
    />
  );
}
