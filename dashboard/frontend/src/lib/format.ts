/**
 * Formatting utilities for numbers, durations, and dates
 */

/** Format a number with specified decimal places */
export function formatNumber(value: number, decimals = 2): string {
  if (value === null || value === undefined || isNaN(value)) {
    return '-';
  }
  return value.toFixed(decimals);
}

/** Format a rate value (requests/s, tokens/s) */
export function formatRate(value: number, decimals = 1): string {
  if (value === null || value === undefined || isNaN(value)) {
    return '-';
  }

  if (value >= 1000) {
    return `${(value / 1000).toFixed(1)}k`;
  }
  return value.toFixed(decimals);
}

/** Format latency in milliseconds */
export function formatLatency(ms: number): string {
  if (ms === null || ms === undefined || isNaN(ms)) {
    return '-';
  }

  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(2)}s`;
  }
  return `${ms.toFixed(0)}ms`;
}

/** Format percentage */
export function formatPercent(value: number, decimals = 1): string {
  if (value === null || value === undefined || isNaN(value)) {
    return '-';
  }
  return `${value.toFixed(decimals)}%`;
}

/** Format bytes to human readable */
export function formatBytes(bytes: number): string {
  if (bytes === null || bytes === undefined || isNaN(bytes)) {
    return '-';
  }

  const units = ['B', 'KB', 'MB', 'GB'];
  let i = 0;
  let value = bytes;

  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }

  return `${value.toFixed(1)}${units[i]}`;
}

/** Format an estimated cost in USD, retaining useful precision for small windows. */
export function formatCurrencyUSD(value: number): string {
  if (value === null || value === undefined || isNaN(value)) {
    return '-';
  }
  if (value > 0 && value < 0.01) {
    return `$${value.toFixed(4)}`;
  }
  return `$${value.toFixed(2)}`;
}

/** Format timestamp to local time string */
export function formatTime(timestamp: number): string {
  const date = new Date(timestamp);
  return date.toLocaleTimeString('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}

/** Format timestamp to date-time string */
export function formatDateTime(timestamp: number): string {
  const date = new Date(timestamp);
  return date.toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}

/** Format timestamp to relative time (e.g., "2s ago") */
export function formatRelativeTime(timestamp: number): string {
  const now = Date.now();
  const diff = now - timestamp;

  if (diff < 1000) {
    return 'just now';
  } else if (diff < 60 * 1000) {
    return `${Math.floor(diff / 1000)}s ago`;
  } else if (diff < 60 * 60 * 1000) {
    return `${Math.floor(diff / (60 * 1000))}m ago`;
  } else if (diff < 24 * 60 * 60 * 1000) {
    return `${Math.floor(diff / (60 * 60 * 1000))}h ago`;
  } else {
    return `${Math.floor(diff / (24 * 60 * 60 * 1000))}d ago`;
  }
}

/** Format utilization ratio to percentage display */
export function formatUtilization(ratio: number): string {
  if (ratio === null || ratio === undefined || isNaN(ratio)) {
    return '-';
  }
  return `${(ratio * 100).toFixed(0)}%`;
}

/** Get color class based on utilization */
export function getUtilizationColor(ratio: number): string {
  if (ratio >= 0.9) return 'text-red-400';
  if (ratio >= 0.7) return 'text-yellow-400';
  return 'text-green-400';
}

/** Get background color class based on error rate */
export function getErrorRateColor(pct: number): string {
  if (pct >= 5) return 'bg-red-500/20 text-red-400';
  if (pct >= 1) return 'bg-yellow-500/20 text-yellow-400';
  return 'bg-green-500/20 text-green-400';
}
