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

async function downloadWithFallback(candidates, binDir) {
  let lastErr;

  for (const candidate of candidates) {
    const archivePath = path.join(binDir, candidate.filename);

    try {
      await download(candidate.url, archivePath);
      return {
        archivePath,
        filename: candidate.filename,
      };
    } catch (err) {
      lastErr = err;
      try {
        fs.unlinkSync(archivePath);
      } catch {}
    }
  }

  throw lastErr || new Error('No download candidates available');
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

  const binDir = __dirname;
  const binaryName = platform === 'windows' ? 'graphjin.exe' : 'graphjin';
  const binaryPath = path.join(binDir, binaryName);
  const releaseBase = `https://github.com/dosco/graphjin/releases/download/v${version}`;

  // New releases include version in the archive name. Keep legacy fallback
  // names to remain backward compatible with older tags.
  const filenames = platform === 'windows'
    ? [
        `graphjin_${version}_${platform}_${arch}.tar.gz`,
        `graphjin_${version}_${platform}_${arch}.zip`,
        `graphjin_${platform}_${arch}.tar.gz`,
        `graphjin_${platform}_${arch}.zip`,
      ]
    : [
        `graphjin_${version}_${platform}_${arch}.tar.gz`,
        `graphjin_${platform}_${arch}.tar.gz`,
      ];
  const candidates = filenames.map((filename) => ({
    filename,
    url: `${releaseBase}/${filename}`,
  }));

  // Skip if binary already exists
  if (fs.existsSync(binaryPath)) {
    console.log('GraphJin binary already installed');
    return;
  }

  console.log(`Downloading GraphJin v${version} for ${platform}/${arch}...`);

  let archivePath = '';
  let selectedFilename = '';

  try {
    const selected = await downloadWithFallback(candidates, binDir);
    archivePath = selected.archivePath;
    selectedFilename = selected.filename;

    console.log('Extracting...');

    if (platform === 'windows' && selectedFilename.endsWith('.zip')) {
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
