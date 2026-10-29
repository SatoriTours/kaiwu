import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import type { Locator, Page } from '@playwright/test';
import { clickInView } from './view-navigation.ts';

// Unit-only native-driver boundary double; real delivery/API is checked in browsers.
function frameTarget(cssHovered = true, hoverError?: Error, clickError?: Error) {
  let moves = 0, presses = 0, pressedBeforeMove = false;
  const window = { top: {} };
  const element = {
    ownerDocument: { defaultView: window },
    matches: () => cssHovered,
  };
  const target = {
    evaluate: async (fn: (el: typeof element) => unknown) => fn(element),
    hover: async () => {
      moves++;
      if (hoverError) throw hoverError;
    },
    click: async () => {
      pressedBeforeMove ||= moves === 0;
      presses++;
      if (clickError) throw clickError;
    },
  } as unknown as Locator;
  return { target, state: () => ({ moves, presses, pressedBeforeMove }) };
}

describe('opaque-frame pointer staging', () => {
  it('separates a native hover action from the single press', async () => {
    const fixture = frameTarget();
    await clickInView({} as Page, fixture.target);
    assert.deepEqual(fixture.state(), { moves: 1, presses: 1, pressedBeforeMove: false });
  });

  it('does not mistake a stale CSS hover snapshot for a native delivery receipt', async () => {
    for (const staleCSS of [false, true]) {
      const fixture = frameTarget(staleCSS);
      await clickInView({} as Page, fixture.target);
      assert.deepEqual(fixture.state(), { moves: 1, presses: 1, pressedBeforeMove: false });
    }
  });

  it('propagates a failed hover without issuing a press', async () => {
    const fixture = frameTarget(true, new Error('frame detached'));
    await assert.rejects(clickInView({} as Page, fixture.target), /frame detached/);
    assert.deepEqual(fixture.state(), { moves: 1, presses: 0, pressedBeforeMove: false });
  });

  it('propagates a failed click without replaying it', async () => {
    const fixture = frameTarget(true, undefined, new Error('press failed'));
    await assert.rejects(clickInView({} as Page, fixture.target), /press failed/);
    assert.deepEqual(fixture.state(), { moves: 1, presses: 1, pressedBeforeMove: false });
  });
});
