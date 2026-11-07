/** Keep only the most recent sample; at most one application per animation frame. */
export function latestFrame<T>(apply: (value: T) => void,
  request: (callback: FrameRequestCallback) => number = requestAnimationFrame,
  cancel: (id: number) => void = cancelAnimationFrame) {
  let pending: T | undefined, frame: number | undefined;
  return {
    schedule(value: T) {
      pending = value;
      if (frame !== undefined) return;
      frame = request(() => {
        frame = undefined;
        const value = pending; pending = undefined;
        if (value !== undefined) apply(value);
      });
    },
    cancel() {
      if (frame !== undefined) cancel(frame);
      frame = undefined; pending = undefined;
    },
  };
}
