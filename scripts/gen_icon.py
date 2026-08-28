#!/usr/bin/env python3
"""Generate a minimal lanDrop app icon (dark rounded square + 'L' droplet)."""
import struct, zlib, os

SIZE = 512

def png_chunk(typ, data):
    c = typ + data
    return struct.pack('>I', len(data)) + c + struct.pack('>I', zlib.crc32(c) & 0xffffffff)

def make_png(path, size):
    # Dark background (#212121) with a blue (#3b82f6) rounded-ish "L"
    bg = (0x21, 0x21, 0x21)
    fg = (0x3b, 0x82, 0xf6)
    rows = []
    # simple anti-aliased rounded square + L glyph drawn with distance functions
    import math
    def inside(x, y):
        # rounded square
        r = size * 0.22
        cx, cy = size/2, size/2
        qx, qy = abs(x-cx)-(size/2-r), abs(y-cy)-(size/2-r)
        d = math.hypot(max(qx,0), max(qy,0)) + min(max(qx,qy),0) - r
        if d > 0: return None
        # L glyph: horizontal bar lower-left + vertical bar left, blue
        # in unit coords (0..1)
        ux, uy = x/size, y/size
        if (0.16 <= ux <= 0.62 and 0.58 <= uy <= 0.84) or \
           (0.16 <= ux <= 0.40 and 0.30 <= uy <= 0.84):
            return fg
        return bg
    for y in range(size):
        row = bytearray([0])  # filter type 0
        for x in range(size):
            c = inside(x+0.5, y+0.5)
            row += bytes(c or bg)
        rows.append(bytes(row))
    raw = b''.join(rows)
    png = b'\x89PNG\r\n\x1a\n'
    png += png_chunk(b'IHDR', struct.pack('>IIBBBBB', size, size, 8, 2, 0, 0, 0))
    png += png_chunk(b'IDAT', zlib.compress(raw, 9))
    png += png_chunk(b'IEND', b'')
    with open(path, 'wb') as f:
        f.write(png)

def make_ico(png_path, ico_path, sizes=(16, 32, 48, 64, 128, 256)):
    # ICO header + one PNG-encoded image per size
    images = []
    for s in sizes:
        p = f'/tmp/landrop_{s}.png'
        make_png(p, s)
        with open(p, 'rb') as f:
            images.append((s, f.read()))
    with open(ico_path, 'wb') as f:
        f.write(struct.pack('<HHH', 0, 1, len(images)))
        offset = 6 + 16 * len(images)
        for s, data in images:
            f.write(struct.pack('<BBBBHHII', s if s < 256 else 0, s if s < 256 else 0, 0, 0, 1, 32, len(data), offset))
            offset += len(data)
        for _, data in images:
            f.write(data)
    print(f'wrote {ico_path} ({os.path.getsize(ico_path)} bytes)')

os.makedirs('build', exist_ok=True)
make_png('build/icon.png', 512)
make_ico('build/icon.png', 'build/icon.ico')
