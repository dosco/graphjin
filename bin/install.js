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

// The command is `deeporg`. A release from before the rename holds a
// `graphjin` binary instead; bin/deeporg.js runs whichever one is here.
const BINARY_NAMES = ['deeporg', 'graphjin'];

function binaryFiles(platform) {
  const ext = platform === 'windows' ? '.exe' : '';
  return BINARY_NAMES.map((name) => name + ext);
}

async function extract(tarPath, destDir, wanted) {
  // Use tar module for extraction
  const tar = require('tar');
  await tar.x({
    file: tarPath,
    cwd: destDir,
    filter: (path) => wanted.includes(path),
  });
}

async function install() {
  const pkg = require('../package.json');
  const version = pkg.version;

  const platform = PLATFORM_MAP[process.platform];
  const arch = ARCH_MAP[process.arch];

  if (!platform || !arch) {
    console.error(`Unsupported platform: ${process.platform} ${process.arch}`);
    console.error('Please download manually from: https://github.com/dosco/deeporg/releases');
    process.exit(1);
  }

  const binDir = __dirname;
  const wanted = binaryFiles(platform);
  const installed = () => wanted.map((name) => path.join(binDir, name)).find((file) => fs.existsSync(file));
  const releaseBase = `https://github.com/dosco/deeporg/releases/download/v${version}`;

  // Newest naming first. Since the command was renamed the archive is
  // deeporg_<version>_<os>_<arch>; releases up to v3.21.21 were
  // graphjin_<version>_<os>_<arch>, and much older ones had no version in the
  // name. Keep every form so this script still installs an older tag.
  const filenames = platform === 'windows'
    ? [
        `deeporg_${version}_${platform}_${arch}.tar.gz`,
        `deeporg_${version}_${platform}_${arch}.zip`,
        `graphjin_${version}_${platform}_${arch}.tar.gz`,
        `graphjin_${version}_${platform}_${arch}.zip`,
        `graphjin_${platform}_${arch}.tar.gz`,
        `graphjin_${platform}_${arch}.zip`,
      ]
    : [
        `deeporg_${version}_${platform}_${arch}.tar.gz`,
        `graphjin_${version}_${platform}_${arch}.tar.gz`,
        `graphjin_${platform}_${arch}.tar.gz`,
      ];
  const candidates = filenames.map((filename) => ({
    filename,
    url: `${releaseBase}/${filename}`,
  }));

  // Skip if binary already exists
  if (installed()) {
    console.log('DeepORG binary already installed');
    return;
  }

  console.log(`Downloading DeepORG v${version} for ${platform}/${arch}...`);

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
      await extract(archivePath, binDir, wanted);
    }

    const binaryPath = installed();
    if (!binaryPath) {
      throw new Error(`no ${wanted.join(' or ')} in ${selectedFilename}`);
    }

    // Set executable permission on Unix
    if (platform !== 'windows') {
      fs.chmodSync(binaryPath, 0o755);
    }

    // Clean up archive
    fs.unlinkSync(archivePath);

    console.log('DeepORG installed successfully! Run it as `deeporg` (or `graphjin`, its old name).');
  } catch (err) {
    console.error(`Failed to install DeepORG: ${err.message}`);
    console.error('Please download manually from: https://github.com/dosco/deeporg/releases');
    // Clean up on failure
    try {
      fs.unlinkSync(archivePath);
    } catch {}
    process.exit(1);
  }
}

install();
