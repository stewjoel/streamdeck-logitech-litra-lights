# Windows Build Issues & Solutions

## 🚨 CRITICAL BUILD WARNING 🚨

**DO NOT** use standard `go build` commands or older `just build` configurations for Windows.

### The Problem
When linking against `hidapi` using CGO and TDM-GCC on Windows, the default Go linker produces a binary that Windows rejects as "not a valid application for this OS platform" (Error `0xc1`). 

This appears to be caused by a malformed PE header or section alignment issue when debug symbols are included in the CGO-linked binary.

### The Solution
You **MUST** strip debug symbols and DWARF information from the binary during the build process using the `-ldflags "-s -w"` flag.

### Correct Build Command
```powershell
$env:CGO_ENABLED="1"; $env:CC="gcc"; $env:CGO_LDFLAGS="-static-libgcc -L{{ CWD }}/hidapi/windows -lhidapi"; go build -C go -ldflags "-s -w" -o ../{{ PLUGIN }}/{{ CODE_PATH_WINDOWS }} .
```

### Incorrect / Broken Commands (DO NOT USE)
- `go build .` (Will define `hidapi` symbols but produce an invalid exe)
- `go build -ldflags "-H windowsgui"` (Does not fix the corruption)
- Mixing `x86_64-w64-mingw32-gcc` with native prompts (Use `gcc` from TDM-GCC directly)

### Environment Prerequisites
- **Compiler**: TDM-GCC (64-bit)
- **Library**: `hidapi.dll` (must be present in `hidapi/windows/` and next to the executable at runtime)
- **PATH**: `C:\TDM-GCC-64\bin` must be in your system PATH
