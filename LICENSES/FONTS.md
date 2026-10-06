# Fonts of the showcase

The font files are in `site/etalage/vendor/fonts/`. Both typefaces are licensed
under the SIL Open Font License 1.1; the full license texts are next to this
file. They are not covered by the MIT license of the repository.

| File | Typeface | Copyright | License text |
|---|---|---|---|
| `fustat-latin.woff2` | Fustat (variable, weights 200–800), <https://github.com/Kief-Type-Foundry/Fustat> | Copyright 2024 The Fustat Project Authors | [`OFL-Fustat.txt`](OFL-Fustat.txt) |
| `duif-serif-400-latin.woff2`, `duif-serif-700-latin.woff2`, `duif-serif-800-latin.woff2` | Nanum Myeongjo Regular, Bold and ExtraBold, <https://fonts.google.com/specimen/Nanum+Myeongjo> | Copyright (c) 2010, NHN Corporation | [`OFL-NanumMyeongjo.txt`](OFL-NanumMyeongjo.txt) |

## Modifications

Both fonts were reduced with `pyftsubset` to a Latin subset (Basic Latin,
Latin-1, Latin Extended-A, punctuation and arrows) and stored as WOFF2.

Nanum Myeongjo carries Reserved Font Names. Because the subset is a modified
version, it is named **"Duif Serif"** in the files and in the CSS
(`site/etalage/styles/tokens.css`), as the OFL requires.
