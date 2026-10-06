/** A load deadline also covers parsing; late decoded GPU assets are disposed. */
export function boundedAssetLoad<T>(
  operation: (signal: AbortSignal) => Promise<T>,
  { loaded, failed, disposeLate, timeoutMs = 10_000 }: {
    loaded: (value: T) => void;
    failed: () => void;
    disposeLate: (value: T) => void;
    timeoutMs?: number;
  },
): () => void {
  const controller = new AbortController();
  let active = true;
  const fail = () => {
    if (!active) return;
    active = false;
    clearTimeout(timer);
    controller.abort();
    failed();
  };
  const timer = setTimeout(fail, timeoutMs);
  operation(controller.signal).then((value) => {
    if (!active) { disposeLate(value); return; }
    active = false;
    clearTimeout(timer);
    loaded(value);
  }, fail);
  return () => {
    if (!active) return;
    active = false;
    clearTimeout(timer);
    controller.abort();
  };
}
