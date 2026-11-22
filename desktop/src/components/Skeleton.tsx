export function SkeletonBlock({ className = '' }: { className?: string }) {
  return <div aria-hidden="true" className={`kairos-shimmer rounded-[6px] ${className}`} />;
}

export function AppShellSkeleton({ label }: { label: string }) {
  return (
    <div
      className="flex h-full w-full flex-col overflow-hidden"
      style={{ backgroundColor: 'var(--bg-canvas)', color: 'var(--text-secondary)' }}
      role="status"
      aria-live="polite"
    >
      {/* Top bar skeleton */}
      <div
        className="flex h-[40px] shrink-0 items-center justify-between border-b px-3"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-hairline)',
        }}
      >
        <div className="flex items-center gap-2.5">
          <SkeletonBlock className="h-4 w-14" />
          <SkeletonBlock className="h-6 w-36" />
          <SkeletonBlock className="h-6 w-28" />
        </div>
        <SkeletonBlock className="hidden h-5 w-40 md:block" />
        <div className="flex items-center gap-1.5">
          <SkeletonBlock className="h-6 w-12" />
          <SkeletonBlock className="h-6 w-6" />
          <SkeletonBlock className="h-6 w-6" />
        </div>
      </div>

      {/* Chart toolbar skeleton */}
      <div
        className="flex h-[36px] shrink-0 items-center justify-between border-b px-3"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-hairline)',
        }}
      >
        <div className="flex items-center gap-2">
          <SkeletonBlock className="h-5 w-44" />
          <SkeletonBlock className="hidden h-4 w-60 sm:block" />
        </div>
        <div className="flex items-center gap-1.5">
          <SkeletonBlock className="h-5 w-12" />
          <SkeletonBlock className="h-5 w-20" />
        </div>
      </div>

      {/* Main canvas subtle loading state */}
      <div className="relative flex min-h-0 flex-1 items-center justify-center p-6">
        <div className="flex flex-col items-center gap-3">
          <div className="flex items-end gap-1.5">
            <span
              className="kairos-shimmer inline-block h-5 w-1.5 rounded-full"
              style={{ animationDelay: '0ms' }}
            />
            <span
              className="kairos-shimmer inline-block h-9 w-1.5 rounded-full"
              style={{ animationDelay: '120ms' }}
            />
            <span
              className="kairos-shimmer inline-block h-6 w-1.5 rounded-full"
              style={{ animationDelay: '240ms' }}
            />
            <span
              className="kairos-shimmer inline-block h-11 w-1.5 rounded-full"
              style={{ animationDelay: '360ms' }}
            />
            <span
              className="kairos-shimmer inline-block h-7 w-1.5 rounded-full"
              style={{ animationDelay: '480ms' }}
            />
          </div>
          <span className="text-[12px] tracking-wide" style={{ color: 'var(--text-secondary)' }}>
            {label}
          </span>
        </div>
      </div>
    </div>
  );
}

export function StatsSkeleton() {
  return (
    <div
      className="h-full w-full overflow-y-auto"
      style={{ backgroundColor: 'var(--bg-canvas)' }}
      aria-busy="true"
    >
      <div className="mx-auto max-w-5xl space-y-5 p-3 sm:p-6">
        <div className="flex items-center justify-between">
          <div className="space-y-1.5">
            <SkeletonBlock className="h-6 w-48" />
            <SkeletonBlock className="h-3.5 w-72" />
          </div>
          <SkeletonBlock className="h-8 w-20" />
        </div>

        <div className="grid grid-cols-1 gap-2.5 min-[420px]:grid-cols-2 sm:gap-3 lg:grid-cols-4">
          {[0, 1, 2, 3].map((idx) => (
            <div
              key={idx}
              className="kairos-card rounded-[8px] p-3.5"
            >
              <SkeletonBlock className="h-3 w-20" />
              <SkeletonBlock className="mt-2.5 h-6 w-28" />
              <SkeletonBlock className="mt-2 h-3 w-36" />
            </div>
          ))}
        </div>

        <div className="kairos-card rounded-[8px] p-4">
          <div className="mb-3 flex items-center justify-between">
            <SkeletonBlock className="h-5 w-32" />
            <SkeletonBlock className="h-6 w-36" />
          </div>
          <SkeletonBlock className="h-[240px] w-full" />
        </div>

        <div className="kairos-card rounded-[8px] p-4 space-y-2.5">
          <SkeletonBlock className="h-5 w-36" />
          {[0, 1, 2, 3, 4].map((idx) => (
            <SkeletonBlock key={idx} className="h-4 w-full" />
          ))}
        </div>
      </div>
    </div>
  );
}
