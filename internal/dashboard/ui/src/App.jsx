import React, { useState, useEffect, useCallback } from 'react';
import { Routes, Route, Link, useLocation } from 'react-router-dom';
import Home from './pages/Home';
import Sessions from './pages/Sessions';
import Tools from './pages/Tools';
import Logs from './pages/Logs';
import Settings from './pages/Settings';
import ApprovalModal from './components/ApprovalModal';

const NAV = [
  { to: '/', label: 'Dashboard' },
  { to: '/sessions', label: 'Sessions' },
  { to: '/tools', label: 'Tools' },
  { to: '/logs', label: 'Logs' },
  { to: '/settings', label: 'Settings' },
];

export default function App() {
  const location = useLocation();
  const [approvals, setApprovals] = useState([]);

  const handleApprovalResponse = useCallback((id, allowed, scope) => {
    fetch(`/api/approval/${id}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, allowed, grant_scope: scope }),
    }).catch(console.error);
    setApprovals(prev => prev.filter(a => a.id !== id));
  }, []);

  useEffect(() => {
    let ws;
    let reconnectTimer;
    const connect = () => {
      const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
      ws = new WebSocket(`${proto}://${window.location.host}/api/ws/approvals`);
      ws.onmessage = (e) => {
        try {
          const req = JSON.parse(e.data);
          setApprovals(prev => [...prev.filter(a => a.id !== req.id), req]);
        } catch (_) {}
      };
      ws.onclose = () => { reconnectTimer = setTimeout(connect, 3000); };
    };
    connect();
    return () => { ws && ws.close(); clearTimeout(reconnectTimer); };
  }, []);

  return (
    <div className="min-h-screen" style={{background:'var(--bg)',color:'var(--fg)'}}>
      <nav style={{borderBottom:'1px solid var(--border)',background:'var(--card)'}} className="px-4 py-3 flex items-center gap-6">
        <span className="font-bold text-lg" style={{color:'var(--accent)'}}>Mantismo</span>
        {NAV.map(n => (
          <Link key={n.to} to={n.to}
            className="text-sm font-medium hover:opacity-80"
            style={{color: location.hash === `#${n.to}` || (n.to==='/'&&location.hash==='') ? 'var(--accent)' : 'var(--fg)'}}>
            {n.label}
          </Link>
        ))}
      </nav>
      <main className="p-4 max-w-7xl mx-auto">
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/sessions" element={<Sessions />} />
          <Route path="/tools" element={<Tools />} />
          <Route path="/logs" element={<Logs />} />
          <Route path="/settings" element={<Settings />} />
        </Routes>
      </main>
      {approvals.map(a => (
        <ApprovalModal key={a.id} approval={a} onRespond={handleApprovalResponse} />
      ))}
    </div>
  );
}
