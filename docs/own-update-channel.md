# Running your own update channel

A TECHO5 updates itself from a **signed manifest** at a URL compiled into its daemon, and only believes a
manifest signed by the ed25519 key compiled in beside it. The update installs as root, so neither is a
setting: a text field would be a way to replace the device's software. To publish your own releases you
change both in the code, build, and install once by hand; after that the device follows your releases.

What the device does: it fetches `<releases>/latest/download/manifest.json` (the **stable** channel) or the
release tagged `dev` (the **dev** channel), checks `manifest.json.sig` against its key, and offers the version
to Home Assistant's update card. On install it downloads the root filesystem the manifest names, checks its
SHA-256 and size, writes it to the spare slot, boots it on trial, and falls back if it doesn't settle.

> **A device built for one channel cannot be updated onto another.** A Show running the project's daemon
> never accepts your manifests. Your first install is manual (below); every update after it is over the air.

## 1. A signing key

Make it once and keep it off every repository. **If you lose it, every device on your channel is stranded**,
because they only trust that key. Back it up.

Save this as `main.go` next to a `go.mod` saying `module keygen` and `go 1.21`, and run `go run .`:

```go
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
)

func main() {
	seed := make([]byte, ed25519.SeedSize)
	rand.Read(seed)
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	os.WriteFile("techo5-release.key", []byte(base64.StdEncoding.EncodeToString(seed)+"\n"), 0o600)
	fmt.Println("public key:", base64.StdEncoding.EncodeToString(pub))
}
```

`techo5-release.key` is the private key (base64 of the 32-byte seed); the printed public key goes into the
code. The signing key is not a GitHub Actions secret: the release script reads it from `TECHO5_SIGN_KEY` on
your own machine.

## 2. Put your key and repository in the code

| File | Change |
|---|---|
| `echod/internal/update/trust.go` | `releaseKey` → your public key |
| `echod/internal/update/releases_cronos.go` | `releases` → `https://github.com/<you>/techo5/releases` |
| `tools/techo5lib.py` | `RELEASE_KEY` → your public key (the installers check the same signature) |
| `tools/install-show.py` | `REPO = '<you>/techo5'` |
| `tools/release.ps1` | `$repo = '<you>/techo5'` |
| `tools/linux/deploy-rootfs.sh` | `--repo <you>/techo5` in the attestation check |

The Go module path (`github.com/HuskerMinion/techo5/...`) is only a name and can stay; the build's `-X` flags
refer to it. The Echo Dot and Echo Spot have their own `releases_dot.go` and `releases_spot.go`, pointing at
`techo5-dot` and `techo5-spot`; they are separate repositories and are not affected.

## 3. Make a release

1. Tag and push. The [build workflow](../.github/workflows/build.yml) builds `echod-arm` on GitHub's runners
   and attests it came from that tag:

   ```
   git tag v0.1.0 && git push origin v0.1.0
   gh run download -n techo5-v0.1.0 -D bin
   ```

2. Build the root filesystem with that daemon (Linux, or on the Show itself from macOS, see
   [building.md](building.md) section 4):

   ```
   PREBUILT_DAEMON=bin/echod-arm bash tools/linux/deploy-rootfs.sh --out build/rootfs.tar.gz --version v0.1.0
   ```

3. Sign and publish with `tools/release.ps1` (PowerShell; on macOS `brew install powershell`, then `pwsh`):

   ```
   $env:TECHO5_SIGN_KEY = "techo5-release.key"
   ./tools/release.ps1 -Version v0.1.0 -Notes "..." -PrebuiltArm bin/echod-arm -PrebuiltArmDot bin/echod-arm-dot -Rootfs build/rootfs.tar.gz
   ```

   It writes `manifest.json` and `manifest.json.sig` (with `go run ./cmd/mkmanifest`), creates the GitHub
   release, and moves the `dev` release forward. A version with a suffix (`v0.1.0-rc.1`) is published as a
   prerelease, which is never "latest", so a stable device does not see it.

Versions have to be something Home Assistant can rank: dotted numerals, optionally `-rc.1` and similar.

## 4. Move a Show onto your channel (once)

With SSH on and your key:

```
HOST=192.168.1.181 bash tools/linux/deploy-rootfs.sh --version v0.1.0 --install
```

This builds and installs into the spare slot, where it boots on trial and falls back if it doesn't settle.
Once it has settled, the Show follows your releases: publish `v0.1.1` and Home Assistant's update card offers
it. A Show with nothing installed yet is installed with `tools/install-show.py`, which now reads your
repository and key.

## Testing a build without a release

To try a daemon on a running Show, bind it over the installed one until the next reboot
([building.md](building.md) section 3; the Show has no sftp, so stream it with
`ssh root@<address> 'cat > /tmp/echod-test' < bin/echod-arm`). That is not an update: nothing is written to a
slot, and a reboot returns to what was installed.
