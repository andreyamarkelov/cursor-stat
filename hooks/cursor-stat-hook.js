#!/usr/bin/env node
// cursor-stat live hook — POST event to local cursor-stat server (127.0.0.1:23556)
'use strict';

const http = require('http');

const PORT = Number(process.env.CURSOR_STAT_HOOK_PORT || 23556);

function readStdin() {
  return new Promise((resolve) => {
    let data = '';
    process.stdin.setEncoding('utf8');
    process.stdin.on('data', (chunk) => { data += chunk; });
    process.stdin.on('end', () => {
      try { resolve(JSON.parse(data || '{}')); }
      catch { resolve({}); }
    });
  });
}

function postEvent(body) {
  return new Promise((resolve) => {
    const req = http.request({
      hostname: '127.0.0.1',
      port: PORT,
      path: '/event',
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      timeout: 100,
    }, (res) => {
      res.resume();
      resolve();
    });
    req.on('error', () => resolve());
    req.on('timeout', () => { req.destroy(); resolve(); });
    req.write(JSON.stringify(body));
    req.end();
  });
}

readStdin().then(async (payload) => {
  await postEvent(payload);
  if (payload.hook_event_name === 'beforeSubmitPrompt') {
    process.stdout.write(JSON.stringify({ continue: true }) + '\n');
  } else {
    process.stdout.write('{}\n');
  }
  process.exit(0);
});
