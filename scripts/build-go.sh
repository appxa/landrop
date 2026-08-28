#!/bin/sh
# Build lanDrop Go binaries for Linux + Windows
set -e
cd "$(dirname "$0")/.."
mkdir -p dist

echo "==> linux/amd64"
CGO_ENABLED=0 go build -ldflags "-s -w" -o dist/landrop .

echo "==> windows/amd64"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w" -o dist/landrop.exe .

echo "==> deb"
mkdir -p /tmp/landrop-deb/DEBIAN /tmp/landrop-deb/usr/bin
cp dist/landrop /tmp/landrop-deb/usr/bin/
printf 'Package: landrop\nVersion: 1.0.0\nSection: net\nPriority: optional\nArchitecture: amd64\nMaintainer: appxa <appxa@users.noreply.github.com>\nDescription: LAN file sharing & chat\n Share files and chat with devices on your local network.\n' > /tmp/landrop-deb/DEBIAN/control
dpkg-deb --build /tmp/landrop-deb dist/landrop_1.0.0_amd64.deb >/dev/null

ls -lh dist/landrop dist/landrop.exe dist/landrop_1.0.0_amd64.deb
