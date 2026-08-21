import { useMemo } from 'react';
import { formatCurrencyUSD } from '../../lib/format';
import type { MetricSnapshot, VariantFilter } from '../../lib/types';

interface EstimatedCostPanelProps {
  data: MetricSnapshot[];
  variant: VariantFilter;
}

const COLORS = {
  input: '#06b6d4',
  output: '#8b5cf6',
  cache_read: '#f59e0b',
  cache_write: '#10b981',
};

/** Calculate a counter increase while tolerating a proxy restart. */
function counterIncrease(snapshots: MetricSnapshot[], field: keyof MetricSnapshot): number {
  if (snapshots.length < 2) return 0;
  const first = snapshots[0][field] as number;
  const last = snapshots[snapshots.length - 1][field] as number;
  return last >= first ? last - first : last;
}

/**
 * Sum counter increases by variant so the combined view cannot subtract a
 * production value from a canary value when both streams share a timestamp.
 */
function windowTotal(data: MetricSnapshot[], field: keyof MetricSnapshot, variant: VariantFilter): number {
  const selected = variant === 'both' ? data : data.filter((snapshot) => snapshot.variant === variant);
  const snapshotsByVariant = new Map<string, MetricSnapshot[]>();

  for (const snapshot of selected) {
    const snapshots = snapshotsByVariant.get(snapshot.variant) ?? [];
    snapshots.push(snapshot);
    snapshotsByVariant.set(snapshot.variant, snapshots);
  }

  return Array.from(snapshotsByVariant.values()).reduce(
    (total, snapshots) => total + counterIncrease(snapshots, field),
    0,
  );
}

export function EstimatedCostPanel({ data, variant }: EstimatedCostPanelProps) {
  const totals = useMemo(() => {
    const input = windowTotal(data, 'estimated_cost_usd_input', variant);
    const output = windowTotal(data, 'estimated_cost_usd_output', variant);
    const cacheRead = windowTotal(data, 'estimated_cost_usd_cache_read', variant);
    const cacheWrite = windowTotal(data, 'estimated_cost_usd_cache_write', variant);
    return {
      input,
      output,
      cacheRead,
      cacheWrite,
      total: input + output + cacheRead + cacheWrite,
    };
  }, [data, variant]);

  const windowHours = useMemo(() => {
    if (data.length < 2) return null;
    const spanMs = data[data.length - 1].timestamp - data[0].timestamp;
    const hours = spanMs / 3_600_000;
    return hours >= 1 ? `${hours.toFixed(0)}h` : `${Math.round(hours * 60)}m`;
  }, [data]);

  return (
    <div className="panel">
      <div className="flex items-start justify-between mb-2">
        <h3 className="panel-header">Est. Cost</h3>
        {windowHours && <span className="text-xs text-slate-500">{windowHours} window</span>}
      </div>

      <div className="mb-3 text-center">
        <div className="text-2xl font-mono font-semibold text-slate-100">{formatCurrencyUSD(totals.total)}</div>
        <div className="text-xs text-slate-500">estimated token cost</div>
      </div>

      <div className="grid grid-cols-4 gap-1 text-center">
        <div>
          <div className="text-xs font-medium" style={{ color: COLORS.input }}>Input</div>
          <div className="font-mono text-xs text-slate-200">{formatCurrencyUSD(totals.input)}</div>
        </div>
        <div>
          <div className="text-xs font-medium" style={{ color: COLORS.output }}>Output</div>
          <div className="font-mono text-xs text-slate-200">{formatCurrencyUSD(totals.output)}</div>
        </div>
        <div>
          <div className="text-xs font-medium" style={{ color: COLORS.cache_read }}>Cache↑</div>
          <div className="font-mono text-xs text-slate-200">{formatCurrencyUSD(totals.cacheRead)}</div>
        </div>
        <div>
          <div className="text-xs font-medium" style={{ color: COLORS.cache_write }}>Cache↓</div>
          <div className="font-mono text-xs text-slate-200">{formatCurrencyUSD(totals.cacheWrite)}</div>
        </div>
      </div>

      <p className="mt-3 text-center text-xs text-slate-500">Uses the proxy&apos;s configured Z.AI token-rate estimate.</p>
    </div>
  );
}
