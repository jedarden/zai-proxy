import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useLiveMetrics } from '../useLiveMetrics';

// Mock EventSource
class MockEventSource {
  static instances: MockEventSource[] = [];

  url: string;
  readyState = 0; // CONNECTING
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }

  close() {
    this.readyState = 2; // CLOSED
  }

  simulateOpen() {
    this.readyState = 1; // OPEN
    this.onopen?.(new Event('open'));
  }

  simulateMessage(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent);
  }

  simulateError() {
    this.onerror?.(new Event('error'));
  }

  static reset() {
    MockEventSource.instances = [];
  }
}

vi.stubGlobal('EventSource', MockEventSource);

describe('useLiveMetrics (SSE)', () => {
  beforeEach(() => {
    MockEventSource.reset();
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('should create an EventSource connection', () => {
    renderHook(() => useLiveMetrics({ sseUrl: 'http://localhost:8080/api/events' }));

    expect(MockEventSource.instances.length).toBe(1);
    expect(MockEventSource.instances[0].url).toBe('http://localhost:8080/api/events');
  });

  it('should mark isConnected on open', async () => {
    const { result } = renderHook(() =>
      useLiveMetrics({ sseUrl: 'http://localhost:8080/api/events' }),
    );

    expect(result.current.isConnected).toBe(false);

    await act(async () => {
      MockEventSource.instances[0].simulateOpen();
    });

    expect(result.current.isConnected).toBe(true);
  });

  it('should update data when metrics message arrives', async () => {
    const { result } = renderHook(() =>
      useLiveMetrics({ sseUrl: 'http://localhost:8080/api/events' }),
    );

    await act(async () => {
      MockEventSource.instances[0].simulateOpen();
    });

    await act(async () => {
      MockEventSource.instances[0].simulateMessage({
        type: 'metrics',
        data: {
          timestamp: 1000,
          variant: 'production',
          req_rate: 2.5,
          requests_2xx: 100,
          requests_4xx: 0,
          requests_5xx: 0,
          tokens_input: 0,
          tokens_output: 0,
          concurrent_requests: 1,
          max_workers: 10,
          rate_limit_rps: 100,
          rate_limit_rejections: 0,
          rate_limit_adj_increase: 0,
          rate_limit_adj_decrease: 0,
          upstream_errors: 0,
          retry_attempts: 0,
          latency_p50: 150,
          latency_p95: 200,
          latency_p99: 250,
          request_size_avg: 0,
          response_size_avg: 0,
          token_rate_in: 0,
          token_rate_out: 0,
          error_rate_pct: 0,
          worker_utilization: 0.75,
        },
      });
    });

    expect(result.current.data.length).toBe(1);
    expect(result.current.data[0].req_rate).toBe(2.5);
    expect(result.current.isLoading).toBe(false);
  });

  it('should reconnect on error with exponential backoff', async () => {
    renderHook(() =>
      useLiveMetrics({ sseUrl: 'http://localhost:8080/api/events' }),
    );

    await act(async () => {
      MockEventSource.instances[0].simulateOpen();
    });

    await act(async () => {
      MockEventSource.instances[0].simulateError();
    });

    // After 1s delay, a new EventSource should be created
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });

    expect(MockEventSource.instances.length).toBe(2);
  });
});
