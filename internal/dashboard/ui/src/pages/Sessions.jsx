import React, { useState, useEffect } from 'react';

export default function Sessions() {
  const [sessions, setSessions] = useState([]);
  useEffect(() => {
    fetch('/api/sessions').then(r=>r.json()).then(setSessions).catch(()=>{});
  }, []);

  return (
    <div>
      <h1 className="text-xl font-semibold mb-4">Sessions</h1>
      <div className="rounded-lg overflow-hidden" style={{border:'1px solid var(--border)'}}>
        <table className="w-full text-sm">
          <thead style={{background:'var(--card)'}}>
            <tr>
              {['Started', 'Ended', 'Server', 'Calls', 'Blocked'].map(h => (
                <th key={h} className="px-4 py-2 text-left font-medium opacity-60">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sessions.length === 0 && (
              <tr><td colSpan={5} className="px-4 py-8 text-center opacity-50">No sessions</td></tr>
            )}
            {sessions.map(s => (
              <tr key={s.id} style={{borderTop:'1px solid var(--border)'}}>
                <td className="px-4 py-2 font-mono text-xs">{new Date(s.started_at).toLocaleString()}</td>
                <td className="px-4 py-2 font-mono text-xs">{s.ended_at ? new Date(s.ended_at).toLocaleString() : 'active'}</td>
                <td className="px-4 py-2">{s.server_command}</td>
                <td className="px-4 py-2">{s.tool_calls}</td>
                <td className="px-4 py-2" style={{color:s.blocked>0?'#ef4444':undefined}}>{s.blocked}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
