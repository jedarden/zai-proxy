/**
 * Structured JSON logging utility for production.
 * In development mode, logs are formatted for readability.
 * In production, logs are structured JSON for log aggregation systems.
 */

export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

interface LogEntry {
  timestamp: string;
  level: LogLevel;
  message: string;
  context?: Record<string, unknown>;
  component?: string;
}

const isProduction = import.meta.env.PROD;

/**
 * Format a log entry for output
 */
function formatEntry(entry: LogEntry): string {
  if (isProduction) {
    // Structured JSON for production log aggregation
    return JSON.stringify(entry);
  }

  // Human-readable format for development
  const timestamp = new Date(entry.timestamp).toLocaleTimeString();
  const level = entry.level.toUpperCase().padEnd(5);
  const context = entry.context ? ` ${JSON.stringify(entry.context)}` : '';
  const component = entry.component ? `[${entry.component}]` : '';

  return `${timestamp} ${level} ${component} ${entry.message}${context}`;
}

/**
 * Create a logger instance for a specific component
 */
export function createLogger(component: string) {
  const log = (level: LogLevel, message: string, context?: Record<string, unknown>) => {
    const entry: LogEntry = {
      timestamp: new Date().toISOString(),
      level,
      message,
      context,
      component,
    };

    const formatted = formatEntry(entry);

    switch (level) {
      case 'error':
        console.error(formatted);
        break;
      case 'warn':
        console.warn(formatted);
        break;
      case 'debug':
        // Only log debug in development
        if (!isProduction) {
          console.debug(formatted);
        }
        break;
      default:
        console.log(formatted);
    }
  };

  return {
    debug: (message: string, context?: Record<string, unknown>) => log('debug', message, context),
    info: (message: string, context?: Record<string, unknown>) => log('info', message, context),
    warn: (message: string, context?: Record<string, unknown>) => log('warn', message, context),
    error: (message: string, context?: Record<string, unknown>) => log('error', message, context),
  };
}

/**
 * Global logger for general application logging
 */
export const logger = createLogger('app');

/**
 * SSE-specific logger
 */
export const sseLogger = createLogger('sse');

/**
 * Metrics logger for data pipeline debugging
 */
export const metricsLogger = createLogger('metrics');
