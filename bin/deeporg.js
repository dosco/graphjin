#!/usr/bin/env node
// Runs the DeepORG binary that bin/install.js downloaded. package.json points
// both commands, `deeporg` and `graphjin` (its old name), at this file.
const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

const ext = process.platform === 'win32' ? '.exe' : '';

// `deeporg` is what a current release ships. `graphjin` is what a release from
// before the rename shipped, and may be what an older install left here.
const binary = ['deeporg', 'graphjin']
  .map((name) => path.join(__dirname, name + ext))
  .find((candidate) => fs.existsSync(candidate));

if (!binary) {
  console.error(`DeepORG binary not found in: ${__dirname}`);
  console.error('Try reinstalling: npm install -g graphjin');
  process.exit(1);
}

const child = spawn(binary, process.argv.slice(2), { stdio: 'inherit' });

child.on('error', (err) => {
  console.error(`Failed to start deeporg: ${err.message}`);
  console.error('Try reinstalling: npm install -g graphjin');
  process.exit(1);
});

child.on('exit', (code, signal) => {
  if (signal) {
    console.error(`deeporg terminated by signal: ${signal}`);
    process.exit(1);
  }
  process.exit(code ?? 0);
});
