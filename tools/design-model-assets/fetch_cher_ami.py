"""Fetch the Smithsonian's explicitly marked Download3D GLB, no login.

Usage: python3 tools/design-model-assets/fetch_cher_ami.py <output-dir>
Output always outside Git; background Blender inspection is a separate step.
"""
from pathlib import Path
import hashlib
import json
import struct
import subprocess
import sys
import urllib.request

if len(sys.argv) != 2:
    sys.exit('usage: fetch_cher_ami.py <output-dir outside Git>')
OUT = Path(sys.argv[1]).expanduser()
CATALOGUE = 'https://3d-api.si.edu/api/v1.0/content/file/search?q=Cher%20Ami&rows=100'
NAME = 'nmah-af_30714-cher_ami-150k-4096_std.glb'
EXPECTED = '3b489ef83e43afbd0a20237415a161365a1f8b22489f04a94a67e9e5a172b6d0'
OUT.mkdir(parents=True, exist_ok=True)
with urllib.request.urlopen(CATALOGUE, timeout=30) as response:
    catalogue = json.load(response)
(OUT/'cher-ami-file-catalogue.json').write_text(json.dumps(catalogue, indent=2)+'\n')
asset = next(row['content'] for row in catalogue['rows']
             if row['title'] == 'Cher Ami' and row['content'].get('usage') == 'Download3D'
             and row['content']['uri'].endswith('/'+NAME))
url = asset['uri']
if not url.startswith('https://3d-api.si.edu/content/document/3d_package:687b5a18-e20d-481c-bff3-698d506cd69b/resources/'):
    raise RuntimeError('Unexpected origin in public catalogue')
path = OUT/'cher-ami-download.glb'
if not path.exists() or hashlib.sha256(path.read_bytes()).hexdigest() != EXPECTED:
    subprocess.run(['curl','-fLsS','--max-time','180',url,'-o',str(path)],check=True)
data=path.read_bytes()
if hashlib.sha256(data).hexdigest()!=EXPECTED:
    raise RuntimeError('Upstream file changed: review the new source before use')
magic, version, size=struct.unpack('<4sII',data[:12])
if magic!=b'glTF' or version!=2 or size!=len(data):raise RuntimeError('Invalid GLB')
length,chunk_type=struct.unpack('<II',data[12:20]);model=json.loads(data[20:20+length])
manifest={'url':url,'bytes':len(data),'sha256':EXPECTED,'asset':model['asset'],
          'meshes':len(model.get('meshes',[])),'images':len(model.get('images',[])),
          'animations':len(model.get('animations',[])),
          'license':'CC0 — Smithsonian Open Access',
          'source':'https://americanhistory.si.edu/collections/object/nmah_425415',
          'credit':'Smithsonian Institution, National Museum of American History; Cher Ami, AF.30714; War Department, U.S. Signal Corps'}
(OUT/'cher-ami-download-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps(manifest,indent=2))
