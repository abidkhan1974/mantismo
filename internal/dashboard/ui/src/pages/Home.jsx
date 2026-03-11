import React, { useState, useEffect } from 'react';
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts';
import ActivityFeed from '../components/ActivityFeed';

function StatCard({ label, value, color }) {
  return (
    <div className="rounded-lg p-4" style={{background:'var(--card)',border:'1px solid var(--border)'}}>
      <div className="text-sm opacity-60 mb-1">{label}</div>
      <div className="text-2xl font-bold" style={{color}}>{value ?? '-'}</div>
    </div>
  );
}

export default function Home() {
  const [stats, setStats] = useState(null);
  const [entries, setEntries] = useState([]);

  useEffect(() => {
    fetch('/api/stats').then(r=>r.json()).then(setStats).catch(()=>{});
  }, []);

  useEffect(() => {
    let ws;
    const connect = () => {
      const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
      ws = new WebSocket(`${proto}://${window.location.host}/api/ws/logs`);
      ws.onmessage = e => {
        try { const entry = JSON.parse(e.data); setEntries(prev => [entry, ...prev].slice(0, 100)); } catch(_) {}
      };
      ws.onclose = () => setTimeout(connect, 3000);
    };
    connect();
    return () => ws && ws.close();
  }, []);

  const chartData = stats?.chart_data ?? Array.from({length: 24}, (_, i) => ({
    hour: `${String(i).padStart(2,'0')}:00`,
    calls: 0,
  }));

  return (
    <div>
      <h1 className="text-xl font-semibold mb-4">Dashboard</h1>
      {stats?.active_session ? (
        <div className="mb-4 px-3 py-2 rounded text-sm font-medium text-white" style={{background:'#22c55e'}}>
          Active session: {stats.active_session.server_command}
        </div>
      ) : (
        <div className="mb-4 px-3 py-2 rounded text-sm opacity-60" style={{border:'1px solid var(--border)'}}>
          No active session
        </div>
      )}
      <div className="grid grid-cols-2 gap-3 mb-6 sm:grid-cols-4">
        <StatCard label="Sessions today" value={stats?.sessions_today} />
        <StatCard label="Tool calls" value={stats?.tool_calls_today} />
        <StatCard label="Blocked" value={stats?.blocked_today} color="#ef4444" />
        <StatCard label="Approved" value="-" />
      </div>
      <div className="mb-6 rounded-lg p-4" style={{background:'var(--card)',border:'1px solid var(--border)'}}>
        <div className="text-sm font-medium mb-3">Tool calls (last 24h)</div>
        <ResponsiveContainer width="100%" height={180}>
          <BarChart data={chartData}>
            <XAxis dataKey="hour" tick={{fontSize:10}} interval={5} />
            <YAxis tick={{fontSize:10}} />
            <Tooltip />
            <Bar dataKey="calls" fill="var(--accent)" radius={[2,2,0,0]} />
          </BarChart>
        </ResponsiveContainer>
      </div>
      <ActivityFeed entries={entries} />
    </div>
  );
}
