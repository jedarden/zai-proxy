import { cloneElement, isValidElement } from 'react';
import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { RateLimitPanel } from '../RateLimitPanel';
import type { MetricSnapshot, QuotaState } from '../../../lib/types';

// jsdom gives ResponsiveContainer a zero box, so recharts bails before
// rendering. Clone the chart with the size the container would inject.
vi.mock('recharts', async (importOriginal) => {
  const original = await importOriginal<typeof import('recharts')>();
  return {
    ...original,
    ResponsiveContainer: ({ children }: { children: React.ReactNode }) =>
      isValidElement(children)
        ? cloneElement(children as React.ReactElement<{ width?: number; height?: number }>, {
            width: 600,
            height: 180,
          })
        : null,
  };
});

function snapshot(
  timestamp: number,
  variant: 'production' | 'canary',
  quota?: QuotaState,
): MetricSnapshot {
  return {
    timestamp,
    variant,
    rate_limit_rps: 10,
    rate_limit_rejections: 0,
    rate_limit_adj_increase: 0,
    rate_limit_adj_decrease: 0,
    quota,
  } as MetricSnapshot;
}

function quotaState(overrides: Partial<QuotaState> = {}): QuotaState {
  return {
    five_hour: {
      limit_type: 'credit_limit',
      usage_ratio: 0.423,
      remaining_ratio: 0.577,
      reset_time_unix: Math.floor(Date.now() / 1000) + 3 * 3600 + 12 * 60,
    },
    weekly: {
      limit_type: 'credit_limit',
      usage_ratio: 0.12,
      remaining_ratio: 0.88,
    },
    sample_age_seconds: 42,
    enforcement: false,
    ...overrides,
  };
}

describe('RateLimitPanel quota summary', () => {
  it('shows usage, remaining allowance, reset countdown, and freshness', () => {
    render(<RateLimitPanel data={[snapshot(0, 'production', quotaState())]} variant="production" />);

    const fiveHour = screen.getByTestId('quota-production-five_hour');
    expect(fiveHour).toHaveTextContent('42%'); // usage
    expect(fiveHour).toHaveTextContent('left 58%'); // remaining allowance
    expect(fiveHour).toHaveTextContent(/resets in \d+h \d+m/); // countdown

    const weekly = screen.getByTestId('quota-production-weekly');
    expect(weekly).toHaveTextContent('12%');
    // Weekly advertised no reset time: explicit dash, not a fabricated one.
    expect(weekly).toHaveTextContent('resets —');

    const freshness = screen.getByTestId('quota-production-freshness');
    expect(freshness).toHaveTextContent('sample 42s old');
    expect(freshness).not.toHaveTextContent('stale');

    expect(screen.getByTestId('quota-production-mode')).toHaveTextContent('Observe-only');
  });

  it('marks an old sample explicitly as stale', () => {
    const stale = quotaState({ sample_age_seconds: 20 * 60 });
    render(<RateLimitPanel data={[snapshot(0, 'production', stale)]} variant="production" />);

    expect(screen.getByTestId('quota-production-freshness')).toHaveTextContent('(stale)');
  });

  it('reports a missing sample and missing window explicitly', () => {
    const partial = quotaState({
      five_hour: undefined,
      sample_age_seconds: undefined,
    });
    render(<RateLimitPanel data={[snapshot(0, 'production', partial)]} variant="production" />);

    expect(screen.getByTestId('quota-production-freshness')).toHaveTextContent('no sample yet');
    // A window with no state renders explicit dashes rather than disappearing.
    const fiveHour = screen.getByTestId('quota-production-five_hour');
    expect(fiveHour).toHaveTextContent('—');
    expect(fiveHour).toHaveTextContent('resets —');
  });

  it('says so when a variant exports no quota telemetry at all', () => {
    render(<RateLimitPanel data={[snapshot(0, 'production')]} variant="production" />);

    expect(screen.getByTestId('quota-production-no-data')).toHaveTextContent(
      'production: quota telemetry unavailable',
    );
  });

  it('shows the enforcement state and an open gate', () => {
    const enforcing = quotaState({ enforcement: true, gate_open: true });
    render(<RateLimitPanel data={[snapshot(0, 'production', enforcing)]} variant="production" />);

    expect(screen.getByTestId('quota-production-mode')).toHaveTextContent('Enforcement');
    expect(screen.getByTestId('quota-production-gate')).toHaveTextContent('Gate closed');
  });

  it('summarizes both variants independently in combined mode', () => {
    const data = [
      snapshot(0, 'production', quotaState()),
      snapshot(0, 'canary', quotaState({ enforcement: true })),
      snapshot(60_000, 'production', quotaState()),
      snapshot(60_000, 'canary', quotaState({ enforcement: true })),
    ];
    render(<RateLimitPanel data={data} variant="both" />);

    expect(screen.getByTestId('quota-production-mode')).toHaveTextContent('Observe-only');
    expect(screen.getByTestId('quota-canary-mode')).toHaveTextContent('Enforcement');
    expect(screen.getByTestId('quota-production-five_hour')).toBeInTheDocument();
    expect(screen.getByTestId('quota-canary-five_hour')).toBeInTheDocument();
  });

  it('keeps the historical chart contract when quota data is absent', () => {
    render(
      <RateLimitPanel
        data={[
          snapshot(0, 'production'),
          snapshot(60_000, 'production'),
        ]}
        variant="production"
      />,
    );

    // The existing rate/adjustment/rejection chart still renders.
    const chart = document.querySelector('.recharts-wrapper');
    expect(chart).not.toBeNull();
    expect(document.querySelectorAll('.recharts-line').length).toBeGreaterThanOrEqual(2);
    expect(document.querySelector('.recharts-bar')).not.toBeNull();
  });
});
