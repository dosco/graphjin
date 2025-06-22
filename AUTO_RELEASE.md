# Automatic Release System

This repository is configured with automatic version incrementing and releases on every push to the `master` branch.

## How It Works

The `.github/workflows/auto-release.yml` workflow automatically:

1. **Increments the version number** based on commit message conventions
2. **Updates all Go modules** with the new version
3. **Updates package.json** with the new version
4. **Creates Git tags** for all modules
5. **Triggers the existing release pipeline** which builds and publishes the release

## Version Increment Rules

The version increment is determined by your commit message:

- **Major version** (e.g., 3.0.0 → 4.0.0): Include `[major]` in your commit message
- **Minor version** (e.g., 3.0.0 → 3.1.0): Include `[minor]`, `feat:`, or `feature:` in your commit message
- **Patch version** (e.g., 3.0.0 → 3.0.1): Default for all other commits

## Examples

```bash
# These will trigger a patch release (3.0.26 → 3.0.27)
git commit -m "fix: resolve database connection issue"
git commit -m "chore: update dependencies"

# These will trigger a minor release (3.0.26 → 3.1.0)
git commit -m "feat: add new GraphQL subscription feature"
git commit -m "feature: implement caching layer [minor]"

# This will trigger a major release (3.0.26 → 4.0.0)
git commit -m "breaking: remove deprecated API endpoints [major]"
```

## Skipping Releases

To push changes without triggering a release, include `[skip-release]` in your commit message:

```bash
git commit -m "docs: update README [skip-release]"
```

## What Gets Released

When a version tag is created, the existing `.github/workflows/build.yml` workflow runs and:

- Builds binaries for all platforms (Linux, Windows, macOS)
- Creates signed releases with GPG
- Publishes to package managers (Homebrew, Scoop)
- Publishes to npm registry
- Creates Debian/RPM packages

## Manual Override

If you need to manually control the version, you can still:

1. Temporarily disable the auto-release workflow
2. Use the original manual process with `./release.sh <version>`
3. Create tags manually

## Current Version

The current version is automatically read from `package.json`: **3.0.26** 