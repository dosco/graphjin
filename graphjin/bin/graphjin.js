#!/usr/bin/env node
const { spawn } = require('child_process');
const path = require('path');

const ext = process.platform === 'win32' ? '.exe' : '';
const binary = path.join(__dirname, 'graphjin' + ext);

const child = spawn(binary, process.argv.slice(2), { stdio: 'inherit' });

child.on('error', (err) => {
  console.error(`Failed to start graphjin: ${err.message}`);
  console.error('Try reinstalling: npm install -g graphjin');
  process.exit(1);
});

child.on('exit', (code) => process.exit(code || 0));
