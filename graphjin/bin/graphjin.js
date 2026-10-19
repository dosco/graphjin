#!/usr/bin/env node
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

const ext = process.platform === 'win32' ? '.exe' : '';
const binary = path.join(__dirname, 'graphjin' + ext);

if (!fs.existsSync(binary)) {
  console.error(`GraphJin binary not found at: ${binary}`);
  console.error('Try reinstalling: npm install -g graphjin');
  process.exit(1);
}

const child = spawn(binary, process.argv.slice(2), { stdio: 'inherit' });

child.on('error', (err) => {
  console.error(`Failed to start graphjin: ${err.message}`);
  console.error('Try reinstalling: npm install -g graphjin');
  process.exit(1);
});

child.on('exit', (code, signal) => {
  if (signal) {
    console.error(`graphjin terminated by signal: ${signal}`);
    process.exit(1);
  }
  process.exit(code ?? 0);
});
