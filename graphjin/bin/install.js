const https = require('https');
const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

// Map Node.js platform/arch to Go equivalents
const PLATFORM_MAP = {
  darwin: 'darwin',
  linux: 'linux',
  win32: 'windows',
};

const ARCH_MAP = {
  x64: 'amd64',
  arm64: 'arm64',
};

async function download(url, dest) {
  return new Promise((resolve, reject) => {
    const follow = (url) => {
      https.get(url, (res) => {
        if (res.statusCode === 302 || res.statusCode === 301) {
          follow(res.headers.location);
          return;
        }
        if (res.statusCode !== 200) {
          reject(new Error(`Download failed: ${res.statusCode}`));
          return;
        }
        const file = fs.createWriteStream(dest);
        res.pipe(file);
        file.on('finish', () => {
          file.close();
          resolve();
        });
      }).on('error', reject);
    };
    follow(url);
  });
}

async function extract(tarPath, destDir) {
  // Use tar module for extraction
  const tar = require('tar');
  await tar.x({
    file: tarPath,
    cwd: destDir,
    filter: (path) => path === 'graphjin' || path === 'graphjin.exe',
  });
}

async function install() {
  const pkg = require('../package.json');
  const version = pkg.version;

  const platform = PLATFORM_MAP[process.platform];
  const arch = ARCH_MAP[process.arch];

  if (!platform || !arch) {
    console.error(`Unsupported platform: ${process.platform} ${process.arch}`);
    console.error('Please download manually from: https://github.com/dosco/graphjin/releases');
    process.exit(1);
  }

  const ext = platform === 'windows' ? '.zip' : '.tar.gz';
  const filename = `graphjin_${platform}_${arch}${ext}`;
  const url = `https://github.com/dosco/graphjin/releases/download/v${version}/${filename}`;

  const binDir = __dirname;
  const archivePath = path.join(binDir, filename);
  const binaryName = platform === 'windows' ? 'graphjin.exe' : 'graphjin';
  const binaryPath = path.join(binDir, binaryName);

  // Skip if binary already exists
  if (fs.existsSync(binaryPath)) {
    console.log('GraphJin binary already installed');
    return;
  }

  console.log(`Downloading GraphJin v${version} for ${platform}/${arch}...`);

  try {
    await download(url, archivePath);

    console.log('Extracting...');

    if (platform === 'windows') {
      // For Windows, use PowerShell to extract
      execSync(`powershell -command "Expand-Archive -Path '${archivePath}' -DestinationPath '${binDir}' -Force"`, {
        stdio: 'inherit',
      });
    } else {
      await extract(archivePath, binDir);
    }

    // Set executable permission on Unix
    if (platform !== 'windows') {
      fs.chmodSync(binaryPath, 0o755);
    }

    // Clean up archive
    fs.unlinkSync(archivePath);

    console.log('GraphJin installed successfully!');
  } catch (err) {
    console.error(`Failed to install GraphJin: ${err.message}`);
    console.error('Please download manually from: https://github.com/dosco/graphjin/releases');
    // Clean up on failure
    try {
      fs.unlinkSync(archivePath);
    } catch {}
    process.exit(1);
  }
}

install();
