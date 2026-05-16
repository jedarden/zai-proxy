import { useTheme } from '../contexts/ThemeContext';

interface PanelSkeletonProps {
  height?: number;
}

export function PanelSkeleton({ height = 180 }: PanelSkeletonProps) {
  const { theme } = useTheme();

  return (
    <div className={`panel ${theme === 'dark' ? 'bg-slate-800 border-slate-700' : 'bg-white border-slate-200'}`}>
      <div className="flex items-start justify-between mb-2">
        <div className="skeleton h-4 w-24"></div>
        <div className="skeleton h-8 w-16"></div>
      </div>
      <div
        className="skeleton w-full"
        style={{ height: `${height}px` }}
      ></div>
    </div>
  );
}

export function DashboardSkeleton() {
  return (
    <div className="dashboard-grid">
      <PanelSkeleton />
      <PanelSkeleton />
      <PanelSkeleton />
      <PanelSkeleton />
      <PanelSkeleton />
      <PanelSkeleton />
    </div>
  );
}
