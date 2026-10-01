#!/bin/sh
# Build lanDrop Go binaries for Linux + Windows, plus deb and AppImage
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

echo "==> AppImage"
# appimagetool is not part of a stock Go/CI image. Without this guard the
# script aborts under `set -e` with exit 127 and fails the whole CI run.
if ! command -v appimagetool >/dev/null 2>&1; then
  echo "    appimagetool not found - skipping AppImage"
  echo "    get it from https://github.com/AppImage/AppImageKit/releases, then re-run this script"
  ls -lh dist/landrop dist/landrop.exe dist/landrop_1.0.0_amd64.deb
  exit 0
fi
rm -rf /tmp/landrop-appdir
mkdir -p /tmp/landrop-appdir/usr/bin
cp dist/landrop /tmp/landrop-appdir/usr/bin/
cp build/icon.png /tmp/landrop-appdir/ 2>/dev/null || true
cat > /tmp/landrop-appdir/AppRun << 'EOF'
#!/bin/sh
SELF=$(readlink -f "$0")
HERE=${SELF%/*}
exec "$HERE/usr/bin/landrop"
EOF
cat > /tmp/landrop-appdir/landrop.desktop << 'EOF'
[Desktop Entry]
Name=lanDrop
Comment=LAN file sharing & chat
Exec=landrop
Icon=landrop
Terminal=false
Type=Application
Categories=Network;
EOF
chmod +x /tmp/landrop-appdir/AppRun
ARCH=x86_64 appimagetool /tmp/landrop-appdir dist/landrop-linux-amd64.AppImage >/dev/null

ls -lh dist/landrop dist/landrop.exe dist/landrop_1.0.0_amd64.deb dist/landrop-linux-amd64.AppImage
