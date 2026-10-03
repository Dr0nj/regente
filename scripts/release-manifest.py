#!/usr/bin/env python3
"""Create a complete release inventory; signing follows mandatory verification."""
import argparse, hashlib, json, os, re
from pathlib import Path
p=argparse.ArgumentParser()
p.add_argument('--dist',default='dist')
p.add_argument('--version',required=True)
p.add_argument('--sha',required=True)
a=p.parse_args()
if not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+',a.version) or not re.fullmatch(r'[0-9a-f]{40}',a.sha):
    raise SystemExit('Invalid version/source SHA')
d=Path(a.dist)
assets={f.name:{'sha256':hashlib.sha256(f.read_bytes()).hexdigest(),'bytes':f.stat().st_size} for f in sorted(d.iterdir()) if f.is_file()}
if not assets or any(v['bytes']==0 for v in assets.values()): raise SystemExit('Empty payload')
(d/'release-manifest.json').write_text(json.dumps({'schema':1,'repository':'Dr0nj/regente','version':a.version,'sourceSha':a.sha,'sourceRef':os.environ['GITHUB_REF'],'workflow':'.github/workflows/release.yml','compatibility':{'schema':32,'minimumSchema':32,'maximumSchema':32,'agentProtocol':2,'mixedVersions':False},'assets':assets},indent=2)+'\n')
