/**
 * Async concurrency guards shared by App bootstrap refreshes and Stats fetches.
 */

export interface CoalescedRunner<TArg = void> {
  run: (arg: TArg) => Promise<void>;
  reset: () => void;
}

/**
 * Coalesces concurrent triggers into a single active execution while guaranteeing
 * that any trigger arriving while a run is already in flight schedules one
 * follow-up run after the current one finishes (dirty-flag semantics).
 */
export function createCoalescedRunner<TArg = void>(
  task: (arg: TArg) => Promise<void>,
): CoalescedRunner<TArg> {
  let inFlight: Promise<void> | null = null;
  let dirty = false;
  let latestArg: TArg | undefined;
  let runSeq = 0;
  let active = false;

  return {
    run(arg: TArg): Promise<void> {
      latestArg = arg;
      if (inFlight) {
        dirty = true;
        return inFlight;
      }

      const seq = ++runSeq;
      active = true;
      const current = (async () => {
        try {
          do {
            dirty = false;
            const currentArg = latestArg as TArg;
            await task(currentArg);
          } while (dirty && seq === runSeq);
        } finally {
          if (seq === runSeq) {
            active = false;
            inFlight = null;
          }
        }
      })();

      if (active && seq === runSeq) {
        inFlight = current;
      }
      return current;
    },
    reset(): void {
      runSeq += 1;
      active = false;
      dirty = false;
      inFlight = null;
    },
  };
}

export interface LatestRequestToken {
  seq: number;
  signal: AbortSignal;
  isCurrent: () => boolean;
}

export interface LatestRequestGuard {
  begin: () => LatestRequestToken;
  cancelAll: () => void;
}

/**
 * Ensures only the latest async request can commit UI state, aborting superseded
 * requests and invalidating any late-settling responses.
 */
export function createLatestRequestGuard(): LatestRequestGuard {
  let currentSeq = 0;
  let activeController: AbortController | null = null;

  return {
    begin(): LatestRequestToken {
      activeController?.abort();
      const controller = new AbortController();
      activeController = controller;
      const seq = ++currentSeq;
      return {
        seq,
        signal: controller.signal,
        isCurrent: () => seq === currentSeq && !controller.signal.aborted,
      };
    },
    cancelAll(): void {
      currentSeq += 1;
      activeController?.abort();
      activeController = null;
    },
  };
}
