import React from 'react';

const COLORS = {
  allow: '#22c55e',
  deny: '#ef4444',
  approve: '#f59e0b',
  default: '#3b82f6',
};

export default function ActivityFeed({ entries }) {
  return (
    <div className="rounded-lg" style={{border:'1px solid var(--border)',background:'var(--card)'}}>
      <div className="px-4 py-3 font-medium" style={{borderBottom:'1px solid var(--border)'}}>Live Activity</div>
      <div className="divide-y" style={{'--tw-divide-opacity':1}}>
        {entries.length === 0 && (
          <div className="px-4 py-6 text-center opacity-50 text-sm">No activity yet</div>
        )}
        {entries.slice(0, 50).map((e, i) => (
          <div key={i} className="px-4 py-2 flex items-center gap-3 text-sm">
            <span className="w-2 h-2 rounded-full flex-shrink-0"
              style={{background: COLORS[e.policy] || COLORS.default}} />
            <span className="opacity-60 text-xs font-mono flex-shrink-0">
              {new Date(e.ts).toLocaleTimeString()}
            </span>
            <span className="truncate">{e.summary || e.method}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
