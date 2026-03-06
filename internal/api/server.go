// Package api implements the internal REST + WebSocket API server.
// This is the central nervous system: every feature Mantismo offers is exposed
// through this server. The CLI, web dashboard, and future Tauri/mobile apps
// are all thin clients that call this API.
package api
