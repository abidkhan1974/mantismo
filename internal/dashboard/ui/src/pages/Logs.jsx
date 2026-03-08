import React, { useState, useEffect } from 'react';

const DECISIONS = ['', 'allow', 'deny', 'approve'];

export default function Logs() {
  const [logs, setLogs] = useState([]);
  const [decision, setDecision] = useState('');
  const [live, setLive] = useState(false);
  const [expanded, setExpanded] = useState(null);

  useEffect(() => {
    if (live) return;
    const url = `/api/logs?limit=100${decision ? `&decision=${decision}` : ''}`;
    fetch(url).then(r=>r.json()).then(setLogs).catch(()=>{});
  }, [decision, live]);

  useEffect(() => {
    if (!live) return;
    let ws;
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
    ws = new WebSocket(`${proto}://${window.location.host}/api/ws/logs`);
    ws.onmessage = e => {
      try {
        const entry = JSON.parse(e.data);
        setLogs(prev => [entry, ...prev].slice(0, 200));
      } catch(_) {}
    };
    return () => ws && ws.close();
  }, [live]);

  return (
    <div>
      <div className="flex items-center gap-3 mb-4 flex-wrap">
        <h1 className="text-xl font-semibold">Logs</h1>
        <select value={decision} onChange={e=>setDecision(e.target.value)}
          className="text-sm px-2 py-1 rounded border"
          style={{background:'var(--bg)',border:'1px solid var(--border)',color:'var(--fg)'}}>
          {DECISIONS.map(d => <option key={d} value={d}>{d || 'All decisions'}</option>)}
        </select>
        <button onClick={()=>setLive(l=>!l)}
          className="text-sm px-3 py-1 rounded"
          style={{background: live ? '#3b82f6' : 'var(--card)', color: live ? 'white' : 'var(--fg)', border:'1px solid var(--border)'}}>
          {live ? 'Live \u25cf' : 'Historical'}
        </button>
      </div>
      <div className="rounded-lg overflow-hidden text-sm" style={{border:'1px solid var(--border)'}}>
        {logs.length === 0 && <div className="px-4 py-8 text-center opacity-50">No logs</div>}
        {logs.map((e,i) => (
          <div key={i} style={{borderTop: i>0 ? '1px solid var(--border)' : undefined}}>
            <div className="px-4 py-2 flex items-center gap-2 cursor-pointer hover:opacity-80"
              onClick={()=>setExpanded(expanded===i?null:i)}>
              <span className="text-xs font-mono opacity-60 flex-shrink-0">
                {new Date(e.ts).toLocaleTimeString()}
              </span>
              <span className="text-xs px-1.5 py-0.5 rounded flex-shrink-0"
                style={{background: e.policy==='deny'?'#fee2e2':e.policy==='approve'?'#fef9c3':'#dcfce7',
                        color: e.policy==='deny'?'#991b1b':e.policy==='approve'?'#854d0e':'#166534'}}>
                {e.policy || 'allow'}
              </span>
              <span className="truncate">{e.summary || e.method}</span>
            </div>
            {expanded===i && (
              <pre className="px-4 py-2 text-xs overflow-auto max-h-64" style={{background:'var(--bg)'}}>
                {JSON.stringify(e, null, 2)}
              </pre>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
