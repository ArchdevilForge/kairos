import assert from 'node:assert/strict';
import test from 'node:test';
import { createCoalescedRunner, createLatestRequestGuard } from '../src/utils/asyncGuards.ts';

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

test('createCoalescedRunner runs a follow-up fetch when triggered during an in-flight request', async () => {
  const calls = [];
  const gates = [deferred(), deferred()];
  const applied = [];

  const runner = createCoalescedRunner(async (label) => {
    const idx = calls.length;
    calls.push(idx);
    await gates[idx].promise;
    applied.push(`${idx}:${label}`);
  });

  // First bootstrap starts (pre-sync snapshot).
  const first = runner.run('pre-sync');
  // While first is in flight, account_updated triggers a post-sync refresh.
  const second = runner.run('post-sync');

  assert.equal(calls.length, 1, 'should not run two overlapping fetches');

  // First (stale) fetch finishes -> runner must automatically launch follow-up fetch.
  gates[0].resolve();
  await Promise.resolve();
  await Promise.resolve();

  assert.equal(calls.length, 2, 'must schedule follow-up fetch after dirty trigger');

  gates[1].resolve();
  await Promise.all([first, second]);

  assert.deepEqual(applied, ['0:pre-sync', '1:post-sync']);
});

test('createCoalescedRunner executes subsequent sequential runs after prior run settles', async () => {
  const seen = [];
  const runner = createCoalescedRunner(async (label) => {
    await Promise.resolve();
    seen.push(label);
  });

  await runner.run('initial');
  await runner.run('after-sync');
  await runner.run('after-second-sync');

  assert.deepEqual(seen, ['initial', 'after-sync', 'after-second-sync']);
});

test('createCoalescedRunner recovers cleanly after a failing run', async () => {
  let attempts = 0;
  const runner = createCoalescedRunner(async (label) => {
    attempts += 1;
    if (attempts === 1) {
      throw new Error(`boom:${label}`);
    }
  });

  await assert.rejects(() => runner.run('fail'), /boom:fail/);
  await runner.run('recover');
  assert.equal(attempts, 2);
});

test('createLatestRequestGuard prevents stale out-of-order response from overwriting newer stats', async () => {
  const guard = createLatestRequestGuard();
  const req1 = deferred();
  const req2 = deferred();

  let committed = null;
  let loading = false;

  const startFetch = async (d) => {
    const token = guard.begin();
    loading = true;
    try {
      const res = await d;
      if (!token.isCurrent()) return;
      committed = res;
    } finally {
      if (token.isCurrent()) {
        loading = false;
      }
    }
  };

  const p1 = startFetch(req1.promise);
  const p2 = startFetch(req2.promise);

  // Newer post-sync response arrives FIRST.
  req2.resolve({ version: 2 });
  await p2;
  assert.deepEqual(committed, { version: 2 });
  assert.equal(loading, false);

  // Older pre-sync response arrives LATE -> must be ignored.
  req1.resolve({ version: 1 });
  await p1;
  assert.deepEqual(committed, { version: 2 }, 'stale response must not overwrite newer state');
  assert.equal(loading, false);
});
