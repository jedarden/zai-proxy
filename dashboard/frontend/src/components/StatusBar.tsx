import { formatRate, formatLatency, formatPercent, getErrorRateColor } from '../lib/format';
import { useTheme } from '../contexts/ThemeContext';
import { StaleIndicator } from './StaleIndicator';
import { ReconnectCountdown } from './ReconnectCountdown';
import type { MetricSnapshot, VariantFilter } from '../lib/types';

interface StatusBarProps {
  isConnected: boolean;
  latestSnapshot: MetricSnapshot | null;
  variant?: VariantFilter;
  reconnectAttempts?: number;
  maxReconnectDelay?: number;
  initialReconnectDelay?: number;
  onManualReconnect?: () => void;
  lastUpdate?: number | null;
  latestByVariant?: Record<string, MetricSnapshot>;
}

export function StatusBar({
  isConnected,
  latestSnapshot,
  variant = 'production',
  reconnectAttempts = 0,
  maxReconnectDelay = 30000,
  initialReconnectDelay = 1000,
  onManualReconnect,
  lastUpdate,
  latestByVariant,
}: StatusBarProps) {
  const { theme } = useTheme();

  // Connection status with reconnect countdown
  const connectionStatus = isConnected ? (
    <span className="status-indicator status-connected">
      <span className="w-2 h-2 rounded-full bg-green-400"></span>
      Connected
    </span>
  ) : (
    <ReconnectCountdown
      reconnectAttempts={reconnectAttempts}
      maxReconnectDelay={maxReconnectDelay}
      initialReconnectDelay={initialReconnectDelay}
      onManualReconnect={onManualReconnect}
    />
  );

  const snapshot = latestSnapshot;

  // Build stale indicators for each variant
  const staleIndicators = [];
  if (latestByVariant && lastUpdate) {
    const variants = Object.keys(latestByVariant);
    for (const v of variants) {
      const s = latestByVariant[v];
      staleIndicators.push(
        <StaleIndicator
          key={v}
          lastUpdateMs={s.timestamp}
          variant={v}
        />
      );
    }
  }

  return (
    <div className={`${theme === 'dark' ? 'bg-slate-800 border-slate-700' : 'bg-white border-slate-200'} border-b px-4 py-2`}>
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-6 flex-wrap">
          {/* Connection Status */}
          <div>{connectionStatus}</div>

          {/* Stale Indicators */}
          {staleIndicators.length > 0 && (
            <div className="flex items-center gap-2">
              {staleIndicators}
            </div>
          )}

          {/* Metrics Summary */}
          {snapshot && isConnected && (
            <>
              {/* Request Rate */}
              <div className="flex items-center gap-2">
                <span className={`${theme === 'dark' ? 'text-slate-400' : 'text-slate-500'} text-sm`}>Req:</span>
                <span className={`${theme === 'dark' ? 'text-slate-100' : 'text-slate-800'} font-mono`}>
                  {formatRate(snapshot.req_rate)}/s
                </span>
              </div>

              {/* Latency */}
              <div className="flex items-center gap-2">
                <span className={`${theme === 'dark' ? 'text-slate-400' : 'text-slate-500'} text-sm`}>p50:</span>
                <span className={`${theme === 'dark' ? 'text-slate-100' : 'text-slate-800'} font-mono`}>
                  {formatLatency(snapshot.latency_p50)}
                </span>
              </div>

              {/* Token Rate */}
              <div className="flex items-center gap-2">
                <span className={`${theme === 'dark' ? 'text-slate-400' : 'text-slate-500'} text-sm`}>Tokens:</span>
                <span className={`${theme === 'dark' ? 'text-slate-100' : 'text-slate-800'} font-mono`}>
                  {formatRate(snapshot.token_rate_in + snapshot.token_rate_out)}/s
                </span>
              </div>

              {/* Error Rate */}
              <div className="flex items-center gap-2">
                <span className={`${theme === 'dark' ? 'text-slate-400' : 'text-slate-500'} text-sm`}>Err:</span>
                <span className={`font-mono px-2 py-0.5 rounded ${getErrorRateColor(snapshot.error_rate_pct)}`}>
                  {formatPercent(snapshot.error_rate_pct)}
                </span>
              </div>

              {/* Workers */}
              <div className="flex items-center gap-2">
                <span className={`${theme === 'dark' ? 'text-slate-400' : 'text-slate-500'} text-sm`}>Workers:</span>
                <span className={`${theme === 'dark' ? 'text-slate-100' : 'text-slate-800'} font-mono`}>
                  {snapshot.concurrent_requests.toFixed(0)}/{snapshot.max_workers.toFixed(0)}
                </span>
              </div>
            </>
          )}
        </div>

        {/* Variant Indicator */}
        {variant !== 'both' && (
          <div className="flex items-center gap-2">
            <span className={`${theme === 'dark' ? 'text-slate-400' : 'text-slate-500'} text-sm`}>Variant:</span>
            <span className={`px-2 py-0.5 rounded text-xs font-medium ${
              variant === 'production'
                ? theme === 'dark' ? 'bg-blue-500/20 text-blue-400' : 'bg-blue-100 text-blue-700'
                : theme === 'dark' ? 'bg-purple-500/20 text-purple-400' : 'bg-purple-100 text-purple-700'
            }`}>
              {variant}
            </span>
          </div>
        )}
      </div>
    </div>
  );
}
