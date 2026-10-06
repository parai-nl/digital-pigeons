Only approved, publicly distributable web exports belong here. No masters, personal metadata or meeting content. This directory is copied to the public module-assets route `/modules/duif/media/` during build; it is not protected by module login. The build validates declared exports and removes stale output. Configure files through `config/media-manifest.v1.json`; until delivery the original SVG fallback remains active.

The selected native pigeon package is committed here so that a fresh checkout
and the deploy build ship it: per pigeon (biological, individualist, nonverbal)
a model v4 GLB, a rest poster and two portraits, plus three field recordings.
The model, posters and portraits derive from the photo texture by Luc Viatour /
Lucnix.be and are distributed under CC BY-SA 3.0; the own changes (v4 anatomy
and animations, the darker plumage of the biological pigeon, the individualist's
tie) are listed per slot in the manifest's public attribution. The field
recordings are CC0 1.0. See `NOTICE.md` in the repository root.

To bring in a new iteration:

1. place the web exports in `<assets-root>/model-v4/web/` (portraits via
   `tools/design-model/portraits_v4.mjs`);
2. run `node tools/prepare-meeting-media.mjs --update-manifest <assets-root>`
   from the repository root: it records hashes, sizes and camera views, and
   withdraws the approval of every slot whose bytes changed;
3. record a new, honest approval and attribution for those slots;
4. run `node tools/prepare-meeting-media.mjs <assets-root>`: it verifies every
   hash and copies the exports here;
5. remove exports no longer listed and commit.

Models, posters, portraits and field recordings are hash-verified together.
Never place source footage or editable Blender files in this public directory.

The explainer video at the top of the help lives here too: `uitlegvideo.mp4`
(1440×1080, 79.9 s), its poster and English captions (`uitlegvideo-en.vtt`, the
pigeon only). The same video is published in the public showcase under
`site/etalage/media/video/`. The three files form one package: if all three are
absent (a published snapshot may leave out this duplicate), the build leaves the
video section out; a partial package fails the build. To replace it, render a new cut with `node tools/uitlegvideo/render.mjs`
and install it in one step with
`node tools/uitlegvideo/stand/install-video.mjs <video.mp4> --poster <image> --vtt <file> [--approve "names" --date YYYY-MM-DD]`
from the repository root; it checks the encoding, copies the files and updates
the manifest (without `--approve` the package is `temporary`). Copy the same mp4
and vtt to the showcase. See `tools/uitlegvideo/README.md`.
