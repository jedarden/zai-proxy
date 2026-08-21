import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { EstimatedCostPanel } from '../EstimatedCostPanel';
import type { MetricSnapshot } from '../../../lib/types';

function snapshot(
  timestamp: number,
  variant: 'production' | 'canary',
  input: number,
  output: number,
  cacheRead: number,
  cacheWrite: number,
): MetricSnapshot {
  return {
    timestamp,
    variant,
    estimated_cost_usd_input: input,
    estimated_cost_usd_output: output,
    estimated_cost_usd_cache_read: cacheRead,
    estimated_cost_usd_cache_write: cacheWrite,
  } as MetricSnapshot;
}

describe('EstimatedCostPanel', () => {
  const data = [
    snapshot(0, 'production', 10, 10, 10, 10),
    snapshot(0, 'canary', 20, 20, 20, 20),
    snapshot(60_000, 'production', 12, 15, 11, 10),
    snapshot(60_000, 'canary', 23, 26, 20.5, 20),
  ];

  it('shows selected-variant and combined running totals', () => {
    const { rerender } = render(<EstimatedCostPanel data={data} variant="production" />);
    expect(screen.getByText('$8.00')).toBeInTheDocument();

    rerender(<EstimatedCostPanel data={data} variant="both" />);
    expect(screen.getByText('$17.50')).toBeInTheDocument();
    expect(screen.getByText('$5.00')).toBeInTheDocument();
    expect(screen.getByText('$11.00')).toBeInTheDocument();
    expect(screen.getByText('$1.50')).toBeInTheDocument();
  });
});
