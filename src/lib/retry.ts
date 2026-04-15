/**
 * Retry with exponential backoff — Phase 7
 *
 * Executes an async function with configurable retries, exponential backoff,
 * and optional jitter. Used by the execution engine for job reliability.
 */

export interface RetryOptions {
  /** Maximum number of attempts (including the first). Default: 3 */
  maxAttempts: number;
  /** Base delay in ms before exponential growth. Default: 1000 */
  baseDelayMs: number;
  /** Maximum delay cap in ms. Default: 30000 */
  maxDelayMs: number;
  /** Add random jitter (0-50% of delay). Default: true */
  jitter: boolean;
  /** Called before each retry with attempt info */
  onRetry?: (attempt: number, error: unknown, nextDelayMs: number) => void;
  /** AbortSignal to cancel outstanding retries */
  signal?: AbortSignal;
}

const DEFAULT_OPTIONS: RetryOptions = {
  maxAttempts: 3,
  baseDelayMs: 1000,
  maxDelayMs: 30_000,
  jitter: true,
};

export interface RetryResult<T> {
  success: boolean;
  value?: T;
  error?: unknown;
  attempts: number;
  totalElapsedMs: number;
}

function computeDelay(attempt: number, opts: RetryOptions): number {
  // Exponential: baseDelay * 2^(attempt-1)
  const exponential = opts.baseDelayMs * Math.pow(2, attempt - 1);
  const capped = Math.min(exponential, opts.maxDelayMs);
  if (!opts.jitter) return capped;
  // Add 0-50% jitter
  const jitterFactor = 1 + Math.random() * 0.5;
  return Math.min(Math.round(capped * jitterFactor), opts.maxDelayMs);
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException("Aborted", "AbortError"));
      return;
    }
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener("abort", () => {
      clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    }, { once: true });
  });
}

/**
 * Execute `fn` with retry + exponential backoff.
 */
export async function withRetry<T>(
  fn: (attempt: number) => Promise<T>,
  options?: Partial<RetryOptions>,
): Promise<RetryResult<T>> {
  const opts: RetryOptions = { ...DEFAULT_OPTIONS, ...options };
  const start = performance.now();
  let lastError: unknown;

  for (let attempt = 1; attempt <= opts.maxAttempts; attempt++) {
    try {
      if (opts.signal?.aborted) {
        throw new DOMException("Aborted", "AbortError");
      }
      const value = await fn(attempt);
      return {
        success: true,
        value,
        attempts: attempt,
        totalElapsedMs: performance.now() - start,
      };
    } catch (err) {
      lastError = err;

      // Don't retry on abort
      if (err instanceof DOMException && err.name === "AbortError") {
        break;
      }

      if (attempt < opts.maxAttempts) {
        const delay = computeDelay(attempt, opts);
        opts.onRetry?.(attempt, err, delay);
        await sleep(delay, opts.signal);
      }
    }
  }

  return {
    success: false,
    error: lastError,
    attempts: opts.maxAttempts,
    totalElapsedMs: performance.now() - start,
  };
}
