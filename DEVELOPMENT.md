# Development & Releasing

This document contains information for developers and maintainers of the Stream Deck Litra Beam LX plugin.

## Prerequisites

- [Go](https://go.dev/dl/) (check `go.mod` for version).
- [just](https://github.com/casey/just) command runner.
- [Node.js](https://nodejs.org/) (for `npx streamdeck` tools).

## How do I contribute?

Pull requests are welcome! The codebase is split into two main parts:

1. `go/`: The Go backend for HID communication.
2. `ca.michaelabon.logitech-litra-lights.sdPlugin/`: Property Inspector (UI) files and manifest.

## Building and Testing

Refer to [BUILD_INSTRUCTIONS.md](./BUILD_INSTRUCTIONS.md) for critical information regarding Windows builds and CGO limitations.

To build and link the plugin locally:
```powershell
just build link
```

## How to Create a New Release

To create an official release for GitHub:

1. **Tag the Commit**: Mark the state of the code with a version tag.
   ```powershell
   git tag -a v2.1.0 -m "Release version 2.1.0"
   ```
2. **Push Up**: Send the tags to GitHub.
   ```powershell
   git push origin main --tags
   ```
3. **Draft on GitHub**:
   - Go to your repository on GitHub.com.
   - Click on **Releases** > **Draft a new release**.
   - Select the tag you just pushed.
   - Add a title and description (you can copy from `CHANGELOG.md`).
   - Click **Publish release**.
