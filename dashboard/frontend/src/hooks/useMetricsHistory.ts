import { useState, useEffect, useCallback, useMemo } from 'react';
import type { MetricSnapshot, TimeRange, VariantFilter } from '../lib/types';

interface UseMetricsHistoryOptions {
  range: TimeRange;
  variant: VariantFilter;
  baseUrl?: string;
}

interface UseMetricsHistoryReturn {
  data: MetricSnapshot[];
  loading: boolean;
  error: Error | null;
  refetch: () => void;
}

const API_BASE = '';

/**
 * Hook for fetching historical metrics from REST API
 */
export function useMetricsHistory({
  range,
  variant,
  baseUrl = API_BASE,
}: UseMetricsHistoryOptions): UseMetricsHistoryReturn {
  const [data, setData] = useState<MetricSnapshot[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);

    try {
      const params = new URLSearchParams();
      params.set('range', range);
      if (variant !== 'both') {
        params.set('variant', variant);
      }

      const response = await fetch(`${baseUrl}/api/metrics?${params}`);

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }

      const result: MetricSnapshot[] = await response.json();
      setData(result);
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  }, [range, variant, baseUrl]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  return {
    data,
    loading,
    error,
    refetch: fetchData,
  };
}

/**
 * Hook for merging live SSE data with historical REST data
 */
interface UseMergedMetricsOptions {
  range: TimeRange;
  variant: VariantFilter;
  liveData: MetricSnapshot[];
  historicalData: MetricSnapshot[];
}

interface UseMergedMetricsReturn {
  data: MetricSnapshot[];
}

const TIME_RANGE_MS_MAP: Record<TimeRange, number> = {
  '5m': 5 * 60 * 1000,
  '15m': 15 * 60 * 1000,
  '1h': 60 * 60 * 1000,
  '6h': 6 * 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
};

export function useMergedMetrics({
  range,
  variant,
  liveData,
  historicalData,
}: UseMergedMetricsOptions): UseMergedMetricsReturn {
  const data = useMemo(() => {
    // Filter by variant if needed
    const filterByVariant = (snapshots: MetricSnapshot[]) => {
      if (variant === 'both') {
        return snapshots;
      }
      return snapshots.filter((s) => s.variant === variant);
    };

    const filteredLive = filterByVariant(liveData);
    const filteredHistorical = filterByVariant(historicalData);

    // Merge and deduplicate by timestamp + variant
    const merged = new Map<string, MetricSnapshot>();

    // Add historical data first
    for (const snapshot of filteredHistorical) {
      const key = `${snapshot.timestamp}-${snapshot.variant}`;
      merged.set(key, snapshot);
    }

    // Add live data (takes precedence for overlapping timestamps)
    for (const snapshot of filteredLive) {
      const key = `${snapshot.timestamp}-${snapshot.variant}`;
      merged.set(key, snapshot);
    }

    // Filter to time range
    const now = Date.now();
    const rangeMs = TIME_RANGE_MS_MAP[range];
    const cutoff = now - rangeMs;

    const result = Array.from(merged.values())
      .filter((s) => s.timestamp >= cutoff)
      .sort((a, b) => a.timestamp - b.timestamp);

    return result;
  }, [range, variant, liveData, historicalData]);

  return { data };
}

/**
 * Transform metric snapshots into chart-friendly format
 */
export function useChartData(
  data: MetricSnapshot[],
  fields: (keyof MetricSnapshot)[]
): Array<Record<string, number | string>> {
  return useMemo(() => {
    return data.map((snapshot) => {
      const point: Record<string, number | string> = {
        timestamp: snapshot.timestamp,
        time: new Date(snapshot.timestamp).toLocaleTimeString('en-US', {
          hour: '2-digit',
          minute: '2-digit',
          hour12: false,
        }),
      };

      for (const field of fields) {
        point[field] = snapshot[field] as number;
      }

      return point;
    });
  }, [data, fields]);
}

/**
 * Hook for creating a history fetcher function
 * Used for backfill on SSE reconnect
 */
export function useHistoryFetcher(
  range: TimeRange,
  variant: VariantFilter,
  baseUrl = API_BASE
): () => Promise<MetricSnapshot[]> {
  return useCallback(async () => {
    const params = new URLSearchParams();
    params.set('range', range);
    if (variant !== 'both') {
      params.set('variant', variant);
    }

    const response = await fetch(`${baseUrl}/api/metrics?${params}`);

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    return response.json();
  }, [range, variant, baseUrl]);
}
