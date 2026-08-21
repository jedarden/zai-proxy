import { useState, useMemo } from 'react';
import { StatusBar } from './StatusBar';
import { TimeRangeSelector } from './TimeRangeSelector';
import { VariantToggle } from './VariantToggle';
import { ThemeToggle } from './ThemeToggle';
import { DashboardSkeleton } from './LoadingSkeleton';
import { PanelErrorBoundary } from './ErrorBoundary';
import {
  RequestRatePanel,
  LatencyPanel,
  TokenPanel,
  EstimatedCostPanel,
  ConcurrencyPanel,
  RateLimitPanel,
  ErrorPanel,
} from './panels';
import { useLiveMetrics } from '../hooks/useLiveMetrics';
import { useHistoryFetcher } from '../hooks/useMetricsHistory';
import { useTheme } from '../contexts/ThemeContext';
import type { TimeRange, VariantFilter } from '../lib/types';

interface DashboardProps {
  sseUrl?: string;
}

// Get SSE URL based on current location
function getSseUrl(): string {
  return `${window.location.protocol}//${window.location.host}/api/events`;
}

export function Dashboard({ sseUrl = getSseUrl() }: DashboardProps) {
  const [timeRange, setTimeRange] = useState<TimeRange>('1h');
  const [variantFilter, setVariantFilter] = useState<VariantFilter>('production');
  const { theme } = useTheme();

  // Create history fetcher for backfill
  const historyFetcher = useHistoryFetcher(timeRange, variantFilter);

  // Live metrics from SSE with enhanced properties
  const {
    data: liveData,
    isConnected,
    isLoading,
    latestSnapshot,
    lastUpdate,
    reconnectAttempts,
    maxReconnectDelay,
    initialReconnectDelay,
    manualReconnect,
    latestByVariant,
  } = useLiveMetrics({
    sseUrl,
    historyFetch: historyFetcher,
    bufferSize: 1000,
  });

  // Filter data by current variant selection (for single variant mode)
  // In 'both' mode, panels handle their own data grouping
  const filteredData = useMemo(() => {
    if (variantFilter === 'both') {
      return liveData;
    }
    return liveData.filter((s) => s.variant === variantFilter);
  }, [liveData, variantFilter]);

  // Get latest snapshot for current variant
  const currentSnapshot = useMemo(() => {
    if (!latestSnapshot) return null;
    if (variantFilter === 'both') return latestSnapshot;
    return latestSnapshot.variant === variantFilter ? latestSnapshot : null;
  }, [latestSnapshot, variantFilter]);

  // Show loading skeleton during initial data fetch
  if (isLoading && liveData.length === 0) {
    return (
      <div className={`min-h-screen flex flex-col ${theme === 'dark' ? 'bg-slate-900' : 'bg-slate-100'}`}>
        {/* Header */}
        <header className={`${theme === 'dark' ? 'bg-slate-800 border-slate-700' : 'bg-white border-slate-200'} border-b px-4 py-3`}>
          <div className="flex items-center justify-between">
            <h1 className={`text-xl font-bold ${theme === 'dark' ? 'text-slate-100' : 'text-slate-800'}`}>
              ZAI Proxy Dashboard
            </h1>
            <div className="flex items-center gap-4">
              <VariantToggle value={variantFilter} onChange={setVariantFilter} />
              <TimeRangeSelector value={timeRange} onChange={setTimeRange} />
              <ThemeToggle />
            </div>
          </div>
        </header>

        {/* Status Bar - disconnected state */}
        <StatusBar
          isConnected={false}
          latestSnapshot={null}
          variant={variantFilter}
        />

        {/* Loading Skeleton */}
        <main className="flex-1 p-4">
          <DashboardSkeleton />
        </main>
      </div>
    );
  }

  return (
    <div className={`min-h-screen flex flex-col ${theme === 'dark' ? 'bg-slate-900' : 'bg-slate-100'}`}>
      {/* Header */}
      <header className={`${theme === 'dark' ? 'bg-slate-800 border-slate-700' : 'bg-white border-slate-200'} border-b px-4 py-3`}>
        <div className="flex items-center justify-between">
          <h1 className={`text-xl font-bold ${theme === 'dark' ? 'text-slate-100' : 'text-slate-800'}`}>
            ZAI Proxy Dashboard
          </h1>
          <div className="flex items-center gap-4">
            <VariantToggle value={variantFilter} onChange={setVariantFilter} />
            <TimeRangeSelector value={timeRange} onChange={setTimeRange} />
            <ThemeToggle />
          </div>
        </div>
      </header>

      {/* Status Bar with enhanced props */}
      <StatusBar
        isConnected={isConnected}
        latestSnapshot={currentSnapshot}
        variant={variantFilter}
        reconnectAttempts={reconnectAttempts}
        maxReconnectDelay={maxReconnectDelay}
        initialReconnectDelay={initialReconnectDelay}
        onManualReconnect={manualReconnect}
        lastUpdate={lastUpdate}
        latestByVariant={latestByVariant}
      />

      {/* Main Content - responsive grid with error boundaries */}
      <main className="flex-1 p-4">
        <div className="dashboard-grid">
          {/* Row 1: Request Rate & Latency */}
          <PanelErrorBoundary panelName="Request Rate">
            <RequestRatePanel data={filteredData} variant={variantFilter} height={180} />
          </PanelErrorBoundary>
          <PanelErrorBoundary panelName="Latency">
            <LatencyPanel data={filteredData} variant={variantFilter} height={180} />
          </PanelErrorBoundary>

          {/* Row 2: Token Throughput & Estimated Cost */}
          <PanelErrorBoundary panelName="Tokens">
            <TokenPanel data={filteredData} variant={variantFilter} height={180} />
          </PanelErrorBoundary>
          <PanelErrorBoundary panelName="Estimated Cost">
            <EstimatedCostPanel data={filteredData} variant={variantFilter} />
          </PanelErrorBoundary>

          {/* Row 3: Concurrency & Rate Limiter */}
          <PanelErrorBoundary panelName="Concurrency">
            <ConcurrencyPanel data={filteredData} variant={variantFilter} height={180} />
          </PanelErrorBoundary>
          <PanelErrorBoundary panelName="Rate Limiter">
            <RateLimitPanel data={filteredData} variant={variantFilter} height={180} />
          </PanelErrorBoundary>

          {/* Row 4: Errors */}
          <PanelErrorBoundary panelName="Errors">
            <ErrorPanel data={filteredData} variant={variantFilter} height={180} />
          </PanelErrorBoundary>
        </div>
      </main>
    </div>
  );
}
