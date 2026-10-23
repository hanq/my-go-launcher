# -*- coding: utf-8 -*-
"""生成 Go Launcher 应用图标（闪电小兽 · 赛博霓虹主题）。

产物：
  build/appicon.png           1024×1024 源图（Wails 主图标）
  build/windows/icon.ico      Windows 多尺寸图标
  build/darwin/icon.icns      macOS 多尺寸图标

用法：python build/make_appicon.py
"""
import io
import struct
import os

from PIL import Image, ImageDraw, ImageFilter

SIZE = 1024

# 赛博霓虹主题色
BG = (10, 14, 26)        # #0a0e1a 深蓝黑
GREEN = (56, 242, 200)   # #38f2c8 荧光青
WHITE = (234, 252, 247)  # 冰白

# 造型参数
STROKE = 210
RAD = STROKE // 2
BOLT = [(520, 140), (320, 470), (720, 470), (520, 780)]
EYES = [(448, 462), (592, 462)]
EYE_R = 34
MOUTH = (494, 500, 546, 542)
FEET = [(356, 786, 486, 902), (538, 786, 668, 902)]

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def draw_icon(size=SIZE):
    img = Image.new("RGB", (size, size), BG)
    d = ImageDraw.Draw(img)

    # 柔和辉光
    glow = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    gd = ImageDraw.Draw(glow)
    cx, cy = int(size * 0.508), int(size * 0.459)
    for r, a in [(430, 26), (360, 34), (300, 40), (250, 44)]:
        gd.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(*GREEN, a))
    glow = glow.filter(ImageFilter.GaussianBlur(30))
    img = Image.alpha_composite(img.convert("RGBA"), glow).convert("RGB")
    d = ImageDraw.Draw(img)

    # 闪电主体
    d.line(BOLT, fill=GREEN, width=STROKE, joint="curve")
    for (x, y) in BOLT:
        d.ellipse([x - RAD, y - RAD, x + RAD, y + RAD], fill=GREEN)

    # 两只脚
    for box in FEET:
        d.ellipse(box, fill=GREEN)

    # 脸
    for (x, y) in EYES:
        d.ellipse([x - EYE_R, y - EYE_R, x + EYE_R, y + EYE_R], fill=WHITE)
    d.arc(MOUTH, start=20, end=160, fill=WHITE, width=14)

    return img


def make_ico(img, out_path):
    sizes = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
    img.save(out_path, format="ICO", sizes=sizes)


def make_icns(img, out_path):
    # 现代 macOS 用 PNG 编码的 ic07/ic08/ic09/ic10
    entries = [
        (b"ic07", 128),
        (b"ic08", 256),
        (b"ic09", 512),
        (b"ic10", 1024),
    ]
    chunks = b""
    for ostype, sz in entries:
        buf = io.BytesIO()
        img.resize((sz, sz), Image.Resampling.LANCZOS).save(buf, format="PNG")
        data = buf.getvalue()
        chunks += ostype + struct.pack(">I", len(data) + 8) + data
    with open(out_path, "wb") as f:
        f.write(b"icns" + struct.pack(">I", 8 + len(chunks)) + chunks)


def ascii_preview(img, w=40):
    small = img.resize((w, w), Image.Resampling.LANCZOS)
    px = small.load()
    rows = []
    for y in range(w):
        row = ""
        for x in range(w):
            r, g, b = px[x, y][:3]
            if g > 150 and r < 170 and b > 110:
                row += "#"
            elif r > 200 and g > 200 and b > 200:
                row += "o"
            else:
                row += " "
        rows.append(row)
    return "\n".join(rows)


if __name__ == "__main__":
    img = draw_icon()
    out_png = os.path.join(ROOT, "build", "appicon.png")
    out_ico = os.path.join(ROOT, "build", "windows", "icon.ico")
    out_icns = os.path.join(ROOT, "build", "darwin", "icon.icns")
    img.save(out_png, format="PNG")
    make_ico(img, out_ico)
    make_icns(img, out_icns)
    print("wrote:", out_png)
    print("wrote:", out_ico)
    print("wrote:", out_icns)
    print("--- 32x32 readability ---")
    print(ascii_preview(img, 32))
