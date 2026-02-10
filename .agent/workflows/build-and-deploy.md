---
description: Build, link, and deploy the Stream Deck plugin on Windows
---

# Build & Deploy Workflow

## Prerequisites
- TDM-GCC-64 installed and in PATH (`C:\TDM-GCC-64\bin\gcc.exe`)
- Go 1.24+ installed
- `just` command runner installed
- Stream Deck software installed

## Full Build & Deploy

### 1. Clean old build artifacts
// turbo
```
just clean
```

### 2. Build the plugin
// turbo
```
just build
```

**🚨 CRITICAL:** The build MUST use `-ldflags "-s -w"` to strip debug symbols. Without this flag, TDM-GCC + CGO produces an invalid PE executable that Windows rejects with "not a valid application for this OS platform". This is already configured in the justfile. **DO NOT** remove it or use manual `go build` commands without it.

### 3. Copy plugin to Stream Deck plugins folder (no symlinks!)
// turbo
```
just link
```

**🚨 IMPORTANT:** This uses `Copy-Item`, NOT symlinks/junctions. Symlinks caused file locking and permission issues previously. After running `just link`, the files in AppData are independent copies, so you must re-run `just link` after every `just build`.

### 4. Restart Stream Deck to pick up changes
```
taskkill /IM "StreamDeck.exe" /F; Start-Sleep -s 2; Start-Process "C:\Program Files\Elgato\StreamDeck\StreamDeck.exe"
```

Or alternatively:
```
just restart
```

### 5. Verify plugin is running
// turbo
```
tasklist /FI "IMAGENAME eq streamdeck-logitech-litra-lights.exe"
```

## ❌ Commands That DO NOT Work (Never Use These)

| Command | Why It Fails |
|---------|-------------|
| `go build` without `-ldflags "-s -w"` | Produces invalid PE executable on Windows with TDM-GCC |
| `go build` with `CC=x86_64-w64-mingw32-gcc` | Cross-compiler not needed on Windows, may cause issues |
| `mklink /D` or `New-Item -ItemType Junction` for linking | Symlinks cause file locking and permission issues |
| `rm -f` in PowerShell | `-f` is ambiguous in PowerShell; use `Remove-Item -Force` |
| `&&` in PowerShell | Not a valid separator in older PowerShell; use `;` |

## Quick One-Liner (Full Rebuild + Deploy)
```powershell
just clean; just build; just link; taskkill /IM "StreamDeck.exe" /F; Start-Sleep -s 2; Start-Process "C:\Program Files\Elgato\StreamDeck\StreamDeck.exe"
```
