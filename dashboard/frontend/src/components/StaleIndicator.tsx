interface StaleIndicatorProps {
  lastUpdateMs?: number;
  staleThresholdMs?: number;
  variant?: string;
}

export function StaleIndicator({
  lastUpdateMs,
  staleThresholdMs = 30000, // 30 seconds default
  variant
}: StaleIndicatorProps) {

  if (!lastUpdateMs) {
    return null;
  }

  const now = Date.now();
  const age = now - lastUpdateMs;
  const isStale = age > staleThresholdMs;

  if (!isStale) {
    return null;
  }

  const staleSeconds = Math.floor(age / 1000);
  const staleLabel = staleSeconds < 60
    ? `${staleSeconds}s ago`
    : staleSeconds < 3600
      ? `${Math.floor(staleSeconds / 60)}m ago`
      : `${Math.floor(staleSeconds / 3600)}h ago`;

  return (
    <span className="stale-indicator" title={`Last update: ${staleLabel}`}>
      <svg
        className="w-3 h-3"
        fill="currentColor"
        viewBox="0 0 20 20"
        xmlns="http://www.w3.org/2000/svg"
      >
        <path
          fillRule="evenodd"
          d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z"
          clipRule="evenodd"
        />
      </svg>
      <span className="ml-1">
        {variant ? `${variant}: ` : ''}stale ({staleLabel})
      </span>
    </span>
  );
}
