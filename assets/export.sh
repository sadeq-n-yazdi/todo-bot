#!/bin/sh
# Regenerate every PNG in assets/ from icon.svg.
# Needs inkscape (1.x) and ImageMagick 7 (magick) with Pango support,
# plus the Vazirmatn font for the social preview text.
set -eu
cd "$(dirname "$0")"

# Square icon exports, each rendered natively from the SVG.
for n in 512 640 1024; do
  inkscape icon.svg --export-type=png --export-filename="icon-$n.png" -w "$n" -h "$n"
done

# 256px preview with Telegram's circular crop applied.
inkscape icon.svg --export-type=png --export-filename=round.tmp.png -w 256 -h 256
magick round.tmp.png \
  \( -size 256x256 xc:none -fill white -draw "circle 128,128 128,0" \) \
  -compose DstIn -composite icon-round-preview.png

# 1280x640 GitHub social preview: icon on the left, text on the right.
inkscape icon.svg --export-type=png --export-filename=social-icon.tmp.png -w 440 -h 440
magick social-icon.tmp.png \
  \( -size 440x440 xc:none -fill white -draw "roundrectangle 0,0 439,439 96,96" \) \
  -compose DstIn -composite social-icon.tmp.png
magick -background none -density 72 \
  pango:'<span font="Vazirmatn Bold 104" foreground="#0E3B57">todo-bot</span>' \
  social-name.tmp.png
magick -background none -density 72 -define pango:markup=false \
  -font Vazirmatn -pointsize 36 -fill '#33566B' \
  pango:'Todo lists & reminders on Telegram
English & فارسی · Gregorian & Jalali' \
  social-tag.tmp.png
magick -size 1280x640 xc:'#F6F3EC' \
  social-icon.tmp.png -geometry +100+100 -composite \
  social-name.tmp.png -geometry +600+186 -composite \
  social-tag.tmp.png -geometry +606+346 -composite \
  social-preview.png

# PWA icons (docs/WEB-DESIGN.md section 7.5). The art fills the square and keeps
# all important shapes in the central 70% circle, so the same render is also the
# maskable icon (its safe zone is the central 80% circle). They go to assets/ and
# to web/public/icons/, where the web build picks them up. The web icons use 128
# colors with no alpha, to stay inside the size budget of section 8.5.
for spec in icon-192:192 icon-maskable-512:512 apple-touch-icon:180; do
  name=${spec%%:*}
  size=${spec##*:}
  inkscape icon.svg --export-type=png --export-filename="$name.tmp.png" -w "$size" -h "$size"
  magick "$name.tmp.png" -alpha off -strip -dither FloydSteinberg -colors 128 \
    -define png:compression-level=9 "PNG8:$name.png"
  rm -f "$name.tmp.png"
done
mkdir -p ../web/public/icons
cp icon-192.png icon-maskable-512.png apple-touch-icon.png ../web/public/icons/
cp icon-maskable-512.png ../web/public/icons/icon-512.png

rm -f round.tmp.png social-icon.tmp.png social-name.tmp.png social-tag.tmp.png
