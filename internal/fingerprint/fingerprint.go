// Package fingerprint detects changes to MCP tool descriptions between sessions
// (rug-pull defense). It hashes each tool's name, description, and inputSchema
// and alerts when they change from the stored baseline.
package fingerprint
