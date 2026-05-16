import { useState, useEffect } from 'react';
import { useTheme } from '../contexts/ThemeContext';

interface ReconnectCountdownProps {
  reconnectAttempts: number;
  maxReconnectDelay: number;
  initialReconnectDelay: number;
  onManualReconnect?: () => void;
}

export function ReconnectCountdown({
  reconnectAttempts,
  maxReconnectDelay,
  initialReconnectDelay,
  onManualReconnect,
}: ReconnectCountdownProps) {
  const { theme } = useTheme();
  const [countdown, setCountdown] = useState(0);

  // Calculate the current delay based on exponential backoff
  const currentDelay = Math.min(
    initialReconnectDelay * Math.pow(2, reconnectAttempts),
    maxReconnectDelay
  );

  useEffect(() => {
    // Reset countdown when delay changes
    setCountdown(Math.ceil(currentDelay / 1000));

    const timer = setInterval(() => {
      setCountdown((prev) => Math.max(0, prev - 1));
    }, 1000);

    return () => clearInterval(timer);
  }, [currentDelay, reconnectAttempts]);

  return (
    <div className={`flex items-center gap-2 ${theme === 'dark' ? 'text-slate-300' : 'text-slate-600'}`}>
      <span className="status-indicator status-disconnected">
        <span className="w-2 h-2 rounded-full bg-red-400"></span>
        Disconnected
      </span>
      <span className="text-sm">
        Reconnecting in <span className="reconnect-countdown font-bold">{countdown}s</span>
      </span>
      {onManualReconnect && (
        <button
          onClick={onManualReconnect}
          className={`text-xs px-2 py-1 rounded ${
            theme === 'dark'
              ? 'bg-slate-700 hover:bg-slate-600 text-slate-300'
              : 'bg-slate-200 hover:bg-slate-300 text-slate-700'
          }`}
        >
          Retry Now
        </button>
      )}
    </div>
  );
}
