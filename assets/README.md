# Assets

The app and bot icon: a checklist card with one done row and one pending row, and an amber bell for reminders. It sits on a teal-to-indigo gradient.

| File | Purpose |
| --- | --- |
| `icon.svg` | Hand-written source, `viewBox="0 0 1024 1024"`. Edit this file, then regenerate the PNGs. |
| `icon-512.png` | General use, for example docs and listings. |
| `icon-640.png` | Bot avatar. Upload it with BotFather `/setuserpic`. |
| `icon-1024.png` | High-resolution export. |
| `icon-round-preview.png` | 256px preview with Telegram's circular crop applied, for checking. It is not uploaded anywhere. |
| `social-preview.png` | 1280×640 GitHub social preview. Upload it under the repository's Settings → General → Social preview. |
| `export.sh` | Regenerates every PNG above from `icon.svg`. |

## Design

- Palette:
  - `#147A7A` teal: gradient start, checkbox colour
  - `#0E3B57` deep indigo: gradient end
  - `#F5A524` amber: the bell
  - white: the card
- The background fills the whole square, so Telegram's circular crop looks intentional on both light and dark themes.
- All important shapes sit inside the central 70% circle, so nothing is lost when the avatar is cropped and shown at about 40px.
- The bell has an outline in the background gradient so it reads as a separate shape on top of the card. Its `bgBell` gradient is the inverse of the bell group's transform. If you move, scale or rotate the bell, update `gradientTransform` to match, or the outline will no longer blend into the background.

## Regenerating

You need `inkscape` 1.x and ImageMagick 7 (`magick`) built with Pango. The social preview also needs the Vazirmatn font, for the Farsi word in the tagline. Run:

```sh
assets/export.sh
```

The script runs these commands from inside `assets/`:

```sh
# Square icons, rendered natively at each size
for n in 512 640 1024; do
  inkscape icon.svg --export-type=png --export-filename="icon-$n.png" -w "$n" -h "$n"
done

# Round preview
inkscape icon.svg --export-type=png --export-filename=round.tmp.png -w 256 -h 256
magick round.tmp.png \
  \( -size 256x256 xc:none -fill white -draw "circle 128,128 128,0" \) \
  -compose DstIn -composite icon-round-preview.png

# Social preview
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
rm -f round.tmp.png social-icon.tmp.png social-name.tmp.png social-tag.tmp.png
```

The tagline is rendered through Pango, which shapes the Farsi correctly: the letters are joined and run right to left. Do not switch it to `-annotate` or `label:`, which may render the letters disconnected.

Do not let ImageMagick rasterise `icon.svg` directly. Its built-in SVG renderer handles `<use>` and `gradientTransform` poorly.

Troubleshooting: on some GNOME setups, headless `inkscape` aborts with a GSettings error about `org.gnome.settings-daemon.plugins.xsettings` and the key `antialiasing`. To work around it:

1. Point `GSETTINGS_SCHEMA_DIR` at a copy of the system schemas.
2. Remove that one schema from the copy.
3. Run `glib-compile-schemas` on the copy.
