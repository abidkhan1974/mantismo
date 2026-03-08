import React, { useState, useEffect } from 'react';

export default function Settings() {
  const [policy, setPolicy] = useState(null);
  const [vault, setVault] = useState(null);
  const [apiAddr, setApiAddr] = useState('');

  useEffect(() => {
    fetch('/api/policy').then(r=>r.json()).then(setPolicy).catch(()=>{});
    fetch('/api/vault/stats').then(r=>r.json()).then(setVault).catch(()=>{});
    // Use dynamic host from the browser's location
    setApiAddr(window.location.host);
  }, []);

  return (
    <div className="max-w-xl">
      <h1 className="text-xl font-semibold mb-6">Settings</h1>
      <section className="rounded-lg p-4 mb-4" style={{border:'1px solid var(--border)',background:'var(--card)'}}>
        <h2 className="font-medium mb-3">Policy</h2>
        <div className="text-sm opacity-60 mb-2">Current preset</div>
        <div className="flex gap-2">
          {['paranoid', 'balanced', 'permissive'].map(p => (
            <button key={p} className="px-3 py-1.5 rounded text-sm capitalize"
              style={{
                background: policy?.preset === p ? 'var(--accent)' : 'var(--bg)',
                color: policy?.preset === p ? 'white' : 'var(--fg)',
                border: '1px solid var(--border)',
              }}>
              {p}
            </button>
          ))}
        </div>
      </section>
      <section className="rounded-lg p-4 mb-4" style={{border:'1px solid var(--border)',background:'var(--card)'}}>
        <h2 className="font-medium mb-3">Vault</h2>
        <div className="text-sm">
          Status: <span className="font-medium">{vault?.enabled ? 'Enabled' : 'Disabled'}</span>
        </div>
        {vault?.enabled && (
          <div className="text-sm opacity-60 mt-1">{vault.entries} entries</div>
        )}
      </section>
      <section className="rounded-lg p-4" style={{border:'1px solid var(--border)',background:'var(--card)'}}>
        <h2 className="font-medium mb-3">API Server</h2>
        <div className="text-sm opacity-60">Listening at</div>
        <div className="font-mono text-sm mt-1">{apiAddr || 'loading...'}</div>
      </section>
    </div>
  );
}
