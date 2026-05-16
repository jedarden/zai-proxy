import { useSSE } from './useSSE';
import type { MetricSnapshot } from '../lib/types';

export interface UseLiveMetricsReturn {
  data: MetricSnapshot[];
  isConnected: boolean;
  isLoading: boolean;
  latestSnapshot: MetricSnapshot | null;
  lastUpdate: number | null;
  reconnectAttempts: number;
  maxReconnectDelay: number;
  initialReconnectDelay: number;
  manualReconnect: () => void;
  latestByVariant: Record<string, MetricSnapshot>;
}

interface UseLiveMetricsOptions {
  sseUrl: string;
  historyFetch?: () => Promise<MetricSnapshot[]>;
  bufferSize?: number;
}

/**
 * Hook for managing live metric data via SSE.
 * Delegates to useSSE internally.
 */
export function useLiveMetrics({
  sseUrl,
  historyFetch,
  bufferSize,
}: UseLiveMetricsOptions): UseLiveMetricsReturn {
  return useSSE({ url: sseUrl, historyFetch, bufferSize });
}
