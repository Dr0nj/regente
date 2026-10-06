import hashlib,json,os,shutil,subprocess,tempfile,unittest
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]

class ReleaseTests(unittest.TestCase):
    def test_embedded_verifiers_match_reviewed_source(self):
        source=(ROOT/'scripts/release-verification.sh').read_text().strip()
        for p in ('install.sh','agent/deploy/install-agent.sh','server/deploy/update.sh'):
            text=(ROOT/p).read_text()
            self.assertIn(source,text,p)
            self.assertLess(text.index('verify_release "',text.index('# END RELEASE')),text.index('tar -xzf') if 'tar -xzf' in text else text.index('install -m 0755 "$TMP/agent"'))
        self.assertIn((ROOT/'scripts/release-verification.ps1').read_text().strip(),(ROOT/'agent/deploy/install-agent-windows.ps1').read_text())

    def test_identity_version_and_integrity_fail_closed(self):
        with tempfile.TemporaryDirectory() as t:
            d=Path(t);(d/'gh').write_text('#!/bin/sh\nexit "${GH_FIXTURE_STATUS:-0}"\n');(d/'gh').chmod(0o755)
            payload=d/'input';payload.write_bytes(b'trusted')
            sig=d/'signature';sig.write_text('{}')
            m={'schema':1,'repository':'Dr0nj/regente','version':'v0.2.48','sourceSha':'a'*40,'sourceRef':'refs/heads/main','workflow':'.github/workflows/release.yml','assets':{'fixture':{'bytes':7,'sha256':hashlib.sha256(b'trusted').hexdigest()}}}
            manifest=d/'manifest';manifest.write_text(json.dumps(m))
            temp=d/'work';temp.mkdir()
            env=dict(os.environ,PATH=t+os.pathsep+os.environ['PATH'],REPO='Dr0nj/regente',VERSION='latest',TMP=str(temp),REGENTE_MANIFEST=str(manifest),REGENTE_ATTESTATION=str(sig),BUNDLE_LOCAL=str(payload))
            cmd=['bash','-c','set -euo pipefail; source scripts/release-verification.sh; if verify_release fixture "$TMP/out"; then exit 0; else exit 17; fi']
            def run(**changes): return subprocess.run(cmd,cwd=ROOT,env=dict(env,**changes),stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode
            self.assertEqual(run(),0)
            self.assertEqual(run(GH_FIXTURE_STATUS='1'),17)
            self.assertEqual(run(REPO='attacker/project'),17)
            self.assertEqual(run(VERSION='v0.2.47'),17)
            self.assertEqual(run(VERSION='v0.2.47',PYTHONOPTIMIZE='1'),17)
            payload.write_bytes(b'tamper!')
            self.assertEqual(run(),17)
            self.assertEqual(run(PYTHONOPTIMIZE='1'),17)
            payload.write_bytes(b'trusted')
            m['sourceRef']='refs/heads/codex/malicious';manifest.write_text(json.dumps(m))
            self.assertEqual(run(),17)


    def test_updater_preserves_operator_environment(self):
        # Executa o script real até --help, sem tocar serviço/disco; trap observa
        # atribuições indevidas mesmo quando usage termina o processo com exit.
        env=dict(os.environ,REGENTE_ATTESTATION='operator-proof.json',REGENTE_BACKUP_DIR='operator-backups')
        command="""trap '[[ "$REGENTE_ATTESTATION" == operator-proof.json && "$REGENTE_BACKUP_DIR" == operator-backups ]] || exit 19' EXIT
source server/deploy/update.sh --help"""
        result=subprocess.run([shutil.which('bash') or 'bash','-c',command],cwd=ROOT,env=env,capture_output=True,text=True)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertIn('REGENTE_ATTESTATION',result.stdout)
        self.assertIn('REGENTE_MANIFEST',result.stdout)

if __name__=='__main__': unittest.main()
