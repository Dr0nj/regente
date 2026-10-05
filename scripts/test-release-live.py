#!/usr/bin/env python3
"""Verify the actual signature/payload and prove tamper refusal before publication."""
import hashlib,json,subprocess,sys,tempfile,os
from pathlib import Path
dist=Path(sys.argv[1]).resolve()
m=json.loads((dist/'release-manifest.json').read_text())
args=['gh','attestation','verify',str(dist/'release-manifest.json'),'--bundle',str(dist/'release-manifest.sigstore.json'),'--repo','Dr0nj/regente','--signer-workflow','Dr0nj/regente/.github/workflows/release.yml','--source-digest',m['sourceSha'],'--source-ref',m['sourceRef'],'--deny-self-hosted-runners']
subprocess.run(args,check=True,env={k:v for k,v in os.environ.items() if k not in ('GH_TOKEN','GITHUB_TOKEN')})
for name,a in m['assets'].items():
    p=dist/name
    assert p.name==name and p.stat().st_size==a['bytes'] and hashlib.sha256(p.read_bytes()).hexdigest()==a['sha256'],name
sbom=json.loads((dist/'release-sbom.spdx.json').read_text())
assert sbom['spdxVersion']=='SPDX-2.3' and sbom['packages'],'Empty SBOM'
with tempfile.TemporaryDirectory() as t:
    p=Path(t)/'tampered.json'
    m['version']='v999.999.999'
    p.write_text(json.dumps(m))
    bad=args.copy();bad[3]=str(p)
    assert subprocess.run(bad,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0,'Tampered manifest accepted'
    # Exercise the installer verifier itself, including integrity failure in an if-context.
    asset='regente-server_linux_amd64.tar.gz'
    payload=Path(t)/'bad.tar.gz';payload.write_bytes((dist/asset).read_bytes()+b'tampered')
    env=__import__('os').environ.copy()
    env.update(REPO='Dr0nj/regente',VERSION='latest',TMP=t,REGENTE_MANIFEST=str(dist/'release-manifest.json'),REGENTE_ATTESTATION=str(dist/'release-manifest.sigstore.json'),BUNDLE_LOCAL=str(payload))
    command='set -euo pipefail; source scripts/release-verification.sh; if verify_release "$1" "$2"; then exit 91; fi'
    result=subprocess.run(['bash','-c',command,'verify',asset,str(Path(t)/'output')],env=env,capture_output=True,text=True)
    assert result.returncode==0 and 'Release payload integrity failed' in result.stderr,'Installer did not reject modified payload for integrity'
print('I15 REAL SIGNATURE + ALL ASSETS + MODIFIED MANIFEST/PAYLOAD REFUSED')
