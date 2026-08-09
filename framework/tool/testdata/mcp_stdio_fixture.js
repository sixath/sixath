#!/usr/bin/env node
'use strict';

const readline = require('readline');

const rl = readline.createInterface({
  input: process.stdin,
  crlfDelay: Infinity,
  terminal: false,
});

rl.on('line', (line) => {
  line = String(line || '').trim();
  if (!line) {
    return;
  }
  let req;
  try {
    req = JSON.parse(line);
  } catch {
    return;
  }
  // Notifications have no id.
  if (req.id === undefined || req.id === null) {
    return;
  }
  const res = handle(req);
  process.stdout.write(JSON.stringify(res) + '\n');
});

function handle(req) {
  const base = { jsonrpc: '2.0', id: req.id };
  switch (req.method) {
    case 'initialize':
      return {
        ...base,
        result: {
          protocolVersion: '2025-06-18',
          capabilities: { tools: {} },
          serverInfo: { name: 'mcp-stdio-fixture', version: '1.0.0' },
        },
      };
    case 'ping':
      return { ...base, result: {} };
    case 'tools/list':
      return {
        ...base,
        result: {
          tools: [
            {
              name: 'echo',
              description: 'Echo text back',
              inputSchema: {
                type: 'object',
                properties: {
                  text: { type: 'string', description: 'Text to echo' },
                },
                required: ['text'],
              },
            },
          ],
        },
      };
    case 'tools/call': {
      const params = req.params || {};
      const args = params.arguments || {};
      const text = args.text != null ? String(args.text) : '';
      return {
        ...base,
        result: {
          content: [{ type: 'text', text }],
        },
      };
    }
    default:
      return {
        ...base,
        error: { code: -32601, message: 'Method not found' },
      };
  }
}
