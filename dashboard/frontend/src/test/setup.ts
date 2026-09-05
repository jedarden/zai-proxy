import '@testing-library/jest-dom'

// jsdom has no ResizeObserver and no layout engine: every element measures
// 0x0, so recharts' ResponsiveContainer would never lay out a chart. Answer
// each observe() with a fixed viewport, synchronously, so charts render
// exactly once per render() call and assertions can inspect them right away.
class ResizeObserverMock implements ResizeObserver {
  private callback: ResizeObserverCallback;

  constructor(callback: ResizeObserverCallback) {
    this.callback = callback;
  }

  observe(): void {
    this.callback(
      [
        {
          contentRect: { width: 1024, height: 480 },
        } as unknown as ResizeObserverEntry,
      ],
      this,
    );
  }

  unobserve(): void {}

  disconnect(): void {}
}

globalThis.ResizeObserver = ResizeObserverMock;
