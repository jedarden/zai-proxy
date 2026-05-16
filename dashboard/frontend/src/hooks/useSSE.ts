import { useState, useEffect, useCallback, useRef } from 'react';
import type { MetricSnapshot, SSEMessage } from '../lib/types';

export interface UseSSEReturn {
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

export interface UseSSEOptions {
  url: string;
  historyFetch?: () => Promise<MetricSnapshot[]>;
  bufferSize?: number;
}

const INITIAL_DELAY = 1000;
const MAX_DELAY = 30000;

/**
 * Hook for managing live metric data from SSE (Server-Sent Events).
 * Uses the EventSource API which works natively through Cloudflare tunnels
 * configured with http2Origin=true (no protocol upgrade needed).
 */
export function useSSE({ url, historyFetch, bufferSize = 1000 }: UseSSEOptions): UseSSEReturn {
  const [data, setData] = useState<MetricSnapshot[]>([]);
  const [latestSnapshot, setLatestSnapshot] = useState<MetricSnapshot | null>(null);
  const [lastUpdate, setLastUpdate] = useState<number | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [reconnectAttempts, setReconnectAttempts] = useState(0);
  const [needsBackfill, setNeedsBackfill] = useState(true);

  const esRef = useRef<EventSource | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const shouldReconnectRef = useRef(true);
  const reconnectAttemptsRef = useRef(0);

  const clearReconnectTimeout = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
  }, []);

  const connect = useCallback(() => {
    clearReconnectTimeout();
    shouldReconnectRef.current = true;

    if (esRef.current) {
      esRef.current.close();
      esRef.current = null;
    }

    const es = new EventSource(url);
    esRef.current = es;

    es.onopen = () => {
      setIsConnected(true);
      setReconnectAttempts(0);
      reconnectAttemptsRef.current = 0;
      setNeedsBackfill(true);
    };

    es.onmessage = (event) => {
      try {
        const message: SSEMessage = JSON.parse(event.data);
        if (message.type === 'metrics' && message.data) {
          const snap = message.data;
          setLatestSnapshot(snap);
          setLastUpdate(Date.now());
          setIsLoading(false);

          setData((prev) => {
            const key = `${snap.timestamp}-${snap.variant}`;
            if (prev.some((s) => `${s.timestamp}-${s.variant}` === key)) {
              return prev;
            }
            const next = [...prev, snap];
            return next.length > bufferSize ? next.slice(-bufferSize) : next;
          });
        }
      } catch {
        // ignore parse errors
      }
    };

    es.onerror = () => {
      setIsConnected(false);
      es.close();
      esRef.current = null;

      if (shouldReconnectRef.current) {
        const delay = Math.min(
          INITIAL_DELAY * Math.pow(2, reconnectAttemptsRef.current),
          MAX_DELAY,
        );
        reconnectTimeoutRef.current = setTimeout(() => {
          reconnectAttemptsRef.current += 1;
          setReconnectAttempts(reconnectAttemptsRef.current);
          connect();
        }, delay);
      }
    };
  }, [url, bufferSize, clearReconnectTimeout]);

  const manualReconnect = useCallback(() => {
    clearReconnectTimeout();
    reconnectAttemptsRef.current = 0;
    setReconnectAttempts(0);
    connect();
  }, [connect, clearReconnectTimeout]);

  useEffect(() => {
    connect();
    return () => {
      shouldReconnectRef.current = false;
      clearReconnectTimeout();
      if (esRef.current) {
        esRef.current.close();
        esRef.current = null;
      }
    };
  }, [url]); // reconnect if URL changes

  // Historical backfill on connect
  useEffect(() => {
    if (isConnected && needsBackfill && historyFetch) {
      historyFetch()
        .then((history) => {
          setData((prev) => {
            const merged = new Map<string, MetricSnapshot>();
            for (const s of history) merged.set(`${s.timestamp}-${s.variant}`, s);
            for (const s of prev) merged.set(`${s.timestamp}-${s.variant}`, s);
            return Array.from(merged.values()).sort((a, b) => a.timestamp - b.timestamp);
          });
          setNeedsBackfill(false);
          setIsLoading(false);
        })
        .catch(() => {
          setIsLoading(false);
        });
    } else if (isConnected && needsBackfill) {
      setIsLoading(false);
      setNeedsBackfill(false);
    }
  }, [isConnected, needsBackfill, historyFetch]);

  const latestByVariant = data.reduce<Record<string, MetricSnapshot>>((acc, s) => {
    if (!acc[s.variant] || s.timestamp > acc[s.variant].timestamp) {
      acc[s.variant] = s;
    }
    return acc;
  }, {});

  return {
    data,
    isConnected,
    isLoading,
    latestSnapshot,
    lastUpdate,
    reconnectAttempts,
    maxReconnectDelay: MAX_DELAY,
    initialReconnectDelay: INITIAL_DELAY,
    manualReconnect,
    latestByVariant,
  };
}
