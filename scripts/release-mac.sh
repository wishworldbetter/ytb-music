#!/bin/sh
# Builds the macOS app on this Mac, one for Apple silicon and Intel, signed
# with the Developer ID of mygo.json, has Apple notarize its disk image and
# staples the ticket to it. With -upload, it adds the disk image to the
# GitHub release of mygo.json's version, a draft made if there is none,
# beside the Windows and Linux apps the release workflow builds.
#
# Notarization goes through asc, with the App Store Connect API key it
# keeps (asc auth login).
set -eu
cd "$(dirname "$0")/.."

version=$(python3 -c 'import json; print(json.load(open("mygo.json"))["version"])')
tag="v$version"

go tool mygo build -platform darwin/universal -skip-notarize
dmg=$(ls build/darwin-universal/*.dmg)
app=$(ls -d build/darwin-universal/*.app)

# Signed with Developer ID, the hardened runtime and a timestamp, as
# notarization asks.
codesign --verify --deep --strict "$app"
codesign --display --verbose=2 "$app" 2>&1 | grep -q "Authority=Developer ID Application" ||
	{ echo "$app is not signed with a Developer ID" >&2; exit 1; }

asc notarization submit --file "$dmg" --wait --timeout 1h
xcrun stapler staple "$dmg"
xcrun stapler validate "$dmg"
spctl --assess --type open --context context:primary-signature --verbose "$dmg"

if [ "${1:-}" = "-upload" ]; then
	gh release view "$tag" > /dev/null 2>&1 ||
		gh release create "$tag" --draft --title "$tag" --notes ""
	gh release upload "$tag" "$dmg" --clobber
fi
echo "$dmg"
