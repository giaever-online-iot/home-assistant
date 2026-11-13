package main

import (
	"strconv"
	"strings"
)

// parsePluginVersion reads a custom integration's manifest version
// ("1.4.0", "1.4", "1.5.0b1"). There must be 2 or 3 dot-separated fields;
// every field but the last is all ASCII digits, the last starts with at least
// one digit and any non-digit suffix is dropped. A missing third field is 0.
func parsePluginVersion(raw string) (v [3]int, ok bool) {
	fields := strings.Split(strings.TrimSpace(raw), ".")
	if len(fields) < 2 || len(fields) > 3 {
		return [3]int{}, false
	}
	last := len(fields) - 1
	for i, f := range fields {
		if i == last {
			n := 0
			for n < len(f) && f[n] >= '0' && f[n] <= '9' {
				n++
			}
			f = f[:n]
		}
		if !allDigits(f) {
			return [3]int{}, false
		}
		x, err := strconv.Atoi(f)
		if err != nil { // overflow
			return [3]int{}, false
		}
		v[i] = x
	}
	return v, true
}

// comparePluginVersion compares numerically, field by field: -1, 0 or 1.
func comparePluginVersion(a, b [3]int) int {
	for i := range a {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}

// The pinned hass_ingress release (D1). To bump the pin:
//  1. gh api repos/lovelylain/hass_ingress/releases/tags/<v> --jq '.assets[] | select(.name=="ingress.zip") | {size, digest, browser_download_url}'
//  2. Download the asset yourself; sha256sum and wc -c must match the API's digest and size.
//  3. unzip -l: manifest.json at the root, with "version": "<v>".
//  4. Diff the release's CONFIG_SCHEMA against what ingress.Render emits; check the README's minimum HA version.
//  5. Update these four constants. The texts are built from hassIngressPinVersion, so only test expectations change.
//  6. HASS_INGRESS_PIN_ONLINE=1 go test ./cmd/launcher -run TestHassIngressPinOnline
//  7. Run the device acceptance (A1-A5) on one box.
const (
	hassIngressPinVersion = "1.4.0"
	hassIngressPinURL     = "https://github.com/lovelylain/hass_ingress/releases/download/1.4.0/ingress.zip"
	hassIngressPinSize    = 61359
	hassIngressPinSHA256  = "7b2255fa3f25b97d465c17336cae8188bcc4dfefd52677682f084c1e39a54cac"
)

// The install script, in segments. @NAME@ placeholders are filled, shell-quoted,
// by buildHassIngressScript. Nothing under @CC@ is written before hiSwap (D2).
const (
	hiHeader = `set -eu
work=$(mktemp -d @WORK@)
stage=""
cleanup() { rm -rf "$work"; [ -z "$stage" ] || rm -rf "$stage"; }
trap cleanup EXIT
`
	hiDownload = `wget -qO "$work/ingress.zip" @URL@ || { echo "hass-ingress: download failed: "@URL@ >&2; exit 1; }
`
	hiSize = `size=$(wc -c < "$work/ingress.zip")
[ "$size" -eq @SIZE@ ] || { echo "hass-ingress: size mismatch: got $size bytes, want @SIZE@; nothing changed" >&2; exit 1; }
`
	hiSHA = `echo @SHA@"  $work/ingress.zip" | sha256sum -c - >/dev/null 2>&1 \
  || { echo "hass-ingress: sha256 mismatch for "@URL@"; nothing changed" >&2; exit 1; }
`
	hiUnpack = `mkdir "$work/ingress"
unzip -q "$work/ingress.zip" -d "$work/ingress"
[ -f "$work/ingress/manifest.json" ] || { echo "hass-ingress: manifest.json not at the zip root; nothing changed" >&2; exit 1; }
`
	hiSwap = `mkdir -p @CC@
stage=$(mktemp -d @STAGE@)
cp -r "$work/ingress" "$stage/new"
if [ -e @DEST@ ]; then
  mv @DEST@ "$stage/old"
fi
if ! mv "$stage/new" @DEST@; then
  [ ! -e "$stage/old" ] || mv "$stage/old" @DEST@
  echo "hass-ingress: could not move the new copy into place; previous copy restored" >&2; exit 1
fi
`
)

// shellQuote single-quotes s for bash, so a path with a space or a quote stays one word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// hassIngressScript is the production install script: the pinned release,
// verified in tmpDir, then swapped into configDir. It runs inside the HA
// container as `bash -c <script>`, with ("/config", "/tmp").
func hassIngressScript(configDir, tmpDir string) string {
	return buildHassIngressScript(configDir, tmpDir, hassIngressPinURL, hassIngressPinSize, hassIngressPinSHA256)
}

// buildHassIngressScript takes the source as arguments so the script test can
// run the real script against a local server (D9).
func buildHassIngressScript(configDir, tmpDir, url string, size int64, sha256 string) string {
	r := strings.NewReplacer(
		"@WORK@", shellQuote(tmpDir+"/hass-ingress.XXXXXX"),
		"@URL@", shellQuote(url),
		"@SIZE@", strconv.FormatInt(size, 10),
		"@SHA@", shellQuote(sha256),
		"@CC@", shellQuote(configDir+"/custom_components"),
		"@STAGE@", shellQuote(configDir+"/.snap-hass-ingress.XXXXXX"),
		"@DEST@", shellQuote(configDir+"/custom_components/ingress"),
	)
	return r.Replace(hiHeader + hiDownload + hiSize + hiSHA + hiUnpack + hiSwap)
}
