import { describe, it, expect } from 'vitest';
import {
  formatNumber,
  formatRate,
  formatLatency,
  formatPercent,
  formatBytes,
  formatRelativeTime,
  formatUtilization,
  getUtilizationColor,
  getErrorRateColor,
} from '../format';

describe('format utilities', () => {
  describe('formatNumber', () => {
    it('should format numbers with specified decimals', () => {
      expect(formatNumber(3.14159, 2)).toBe('3.14');
      expect(formatNumber(3.14159, 4)).toBe('3.1416');
      expect(formatNumber(100, 0)).toBe('100');
    });

    it('should handle null/undefined/NaN', () => {
      expect(formatNumber(null as unknown as number)).toBe('-');
      expect(formatNumber(undefined as unknown as number)).toBe('-');
      expect(formatNumber(NaN)).toBe('-');
    });
  });

  describe('formatRate', () => {
    it('should format small rates', () => {
      expect(formatRate(5.5, 1)).toBe('5.5');
      expect(formatRate(0, 1)).toBe('0.0');
    });

    it('should format large rates with k suffix', () => {
      expect(formatRate(1500, 1)).toBe('1.5k');
      expect(formatRate(10000, 1)).toBe('10.0k');
    });

    it('should handle null/undefined/NaN', () => {
      expect(formatRate(null as unknown as number)).toBe('-');
      expect(formatRate(NaN)).toBe('-');
    });
  });

  describe('formatLatency', () => {
    it('should format milliseconds', () => {
      expect(formatLatency(100)).toBe('100ms');
      expect(formatLatency(50.5)).toBe('51ms');
    });

    it('should format seconds for large values', () => {
      expect(formatLatency(1500)).toBe('1.50s');
      expect(formatLatency(10000)).toBe('10.00s');
    });

    it('should handle null/undefined/NaN', () => {
      expect(formatLatency(null as unknown as number)).toBe('-');
      expect(formatLatency(NaN)).toBe('-');
    });
  });

  describe('formatPercent', () => {
    it('should format percentages', () => {
      expect(formatPercent(5.5, 1)).toBe('5.5%');
      expect(formatPercent(0.125, 2)).toBe('0.13%');
    });

    it('should handle null/undefined/NaN', () => {
      expect(formatPercent(null as unknown as number)).toBe('-');
      expect(formatPercent(NaN)).toBe('-');
    });
  });

  describe('formatBytes', () => {
    it('should format bytes', () => {
      expect(formatBytes(500)).toBe('500.0B');
    });

    it('should format kilobytes', () => {
      expect(formatBytes(1024)).toBe('1.0KB');
      expect(formatBytes(1536)).toBe('1.5KB');
    });

    it('should format megabytes', () => {
      expect(formatBytes(1048576)).toBe('1.0MB');
    });

    it('should handle null/undefined/NaN', () => {
      expect(formatBytes(null as unknown as number)).toBe('-');
      expect(formatBytes(NaN)).toBe('-');
    });
  });

  describe('formatRelativeTime', () => {
    it('should format recent times', () => {
      const now = Date.now();
      expect(formatRelativeTime(now)).toBe('just now');
      expect(formatRelativeTime(now - 500)).toBe('just now');
    });

    it('should format seconds ago', () => {
      const now = Date.now();
      expect(formatRelativeTime(now - 5000)).toBe('5s ago');
      expect(formatRelativeTime(now - 30000)).toBe('30s ago');
    });

    it('should format minutes ago', () => {
      const now = Date.now();
      expect(formatRelativeTime(now - 60000)).toBe('1m ago');
      expect(formatRelativeTime(now - 180000)).toBe('3m ago');
    });

    it('should format hours ago', () => {
      const now = Date.now();
      expect(formatRelativeTime(now - 3600000)).toBe('1h ago');
      expect(formatRelativeTime(now - 7200000)).toBe('2h ago');
    });

    it('should format days ago', () => {
      const now = Date.now();
      expect(formatRelativeTime(now - 86400000)).toBe('1d ago');
      expect(formatRelativeTime(now - 172800000)).toBe('2d ago');
    });
  });

  describe('formatUtilization', () => {
    it('should format ratio as percentage', () => {
      expect(formatUtilization(0.5)).toBe('50%');
      expect(formatUtilization(0.75)).toBe('75%');
      expect(formatUtilization(1.0)).toBe('100%');
    });

    it('should handle null/undefined/NaN', () => {
      expect(formatUtilization(null as unknown as number)).toBe('-');
      expect(formatUtilization(NaN)).toBe('-');
    });
  });

  describe('getUtilizationColor', () => {
    it('should return green for low utilization', () => {
      expect(getUtilizationColor(0.5)).toBe('text-green-400');
      expect(getUtilizationColor(0.69)).toBe('text-green-400');
    });

    it('should return yellow for medium utilization', () => {
      expect(getUtilizationColor(0.7)).toBe('text-yellow-400');
      expect(getUtilizationColor(0.85)).toBe('text-yellow-400');
    });

    it('should return red for high utilization', () => {
      expect(getUtilizationColor(0.9)).toBe('text-red-400');
      expect(getUtilizationColor(1.0)).toBe('text-red-400');
    });
  });

  describe('getErrorRateColor', () => {
    it('should return green for low error rate', () => {
      expect(getErrorRateColor(0)).toBe('bg-green-500/20 text-green-400');
      expect(getErrorRateColor(0.5)).toBe('bg-green-500/20 text-green-400');
    });

    it('should return yellow for medium error rate', () => {
      expect(getErrorRateColor(1)).toBe('bg-yellow-500/20 text-yellow-400');
      expect(getErrorRateColor(2.5)).toBe('bg-yellow-500/20 text-yellow-400');
    });

    it('should return red for high error rate', () => {
      expect(getErrorRateColor(5)).toBe('bg-red-500/20 text-red-400');
      expect(getErrorRateColor(10)).toBe('bg-red-500/20 text-red-400');
    });
  });
});
