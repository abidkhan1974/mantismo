import React, { useState, useEffect } from 'react';

export default function Tools() {
  const [tools, setTools] = useState([]);
  useEffect(() => {
    fetch('/api/tools').then(r=>r.json()).then(setTools).catch(()=>{});
  }, []);

  return (
    <div>
      <h1 className="text-xl font-semibold mb-4">Tools</h1>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {tools.length === 0 && (
          <div className="col-span-full text-center py-8 opacity-50 text-sm">No tools seen yet</div>
        )}
        {tools.map(t => (
          <div key={t.name} className="rounded-lg p-4" style={{border:'1px solid var(--border)',background:'var(--card)'}}>
            <div className="flex items-center justify-between mb-2">
              <span className="font-medium text-sm">{t.name}</span>
              <span className="text-xs px-2 py-0.5 rounded"
                style={{background: t.changed ? '#fef9c3' : '#dcfce7', color: t.changed ? '#854d0e' : '#166534'}}>
                {t.changed ? 'Changed' : 'OK'}
              </span>
            </div>
            <div className="text-xs opacity-60">{t.server_cmd || 'native'}</div>
            {t.changed && (
              <button className="mt-2 text-xs px-2 py-1 rounded" style={{background:'var(--accent)',color:'white'}}>
                Acknowledge
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
