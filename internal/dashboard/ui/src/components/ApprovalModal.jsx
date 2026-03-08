import React, { useState, useEffect } from 'react';

const SCOPES = [
  { value: 'this_call', label: 'This call only' },
  { value: '5_minutes', label: 'For 5 minutes' },
  { value: '30_minutes', label: 'For 30 minutes' },
  { value: 'session', label: 'This session' },
  { value: 'permanent', label: 'Always' },
];

export default function ApprovalModal({ approval, onRespond }) {
  const [scope, setScope] = useState('this_call');
  const [remaining, setRemaining] = useState(30);

  useEffect(() => {
    const t = setInterval(() => {
      setRemaining(r => {
        if (r <= 1) { onRespond(approval.id, false, 'this_call'); clearInterval(t); return 0; }
        return r - 1;
      });
    }, 1000);
    return () => clearInterval(t);
  }, [approval.id, onRespond]);

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="rounded-lg shadow-xl p-6 max-w-md w-full mx-4" style={{background:'var(--card)',border:'1px solid var(--border)'}}>
        <div className="flex justify-between items-center mb-4">
          <h2 className="text-lg font-semibold">Approval Request</h2>
          <span className="text-sm font-mono px-2 py-1 rounded" style={{background:'var(--bg)'}}>{remaining}s</span>
        </div>
        <div className="mb-4">
          <div className="font-medium mb-1">Tool: <span style={{color:'var(--accent)'}}>{approval.tool_name}</span></div>
          {approval.server_cmd && <div className="text-sm opacity-70 mb-1">Server: {approval.server_cmd}</div>}
          {approval.reason && <div className="text-sm opacity-70 mb-1">Reason: {approval.reason}</div>}
          {approval.arguments && (
            <pre className="text-xs mt-2 p-2 rounded overflow-auto max-h-32" style={{background:'var(--bg)'}}>{approval.arguments}</pre>
          )}
        </div>
        <div className="mb-4">
          <label className="block text-sm font-medium mb-1">Grant scope:</label>
          <select value={scope} onChange={e=>setScope(e.target.value)}
            className="w-full p-2 rounded border" style={{background:'var(--bg)',border:'1px solid var(--border)',color:'var(--fg)'}}>
            {SCOPES.map(s => <option key={s.value} value={s.value}>{s.label}</option>)}
          </select>
        </div>
        <div className="flex gap-3">
          <button onClick={()=>onRespond(approval.id,true,scope)}
            className="flex-1 py-2 rounded font-medium text-white" style={{background:'#22c55e'}}>Allow</button>
          <button onClick={()=>onRespond(approval.id,false,scope)}
            className="flex-1 py-2 rounded font-medium text-white" style={{background:'#ef4444'}}>Deny</button>
        </div>
      </div>
    </div>
  );
}
