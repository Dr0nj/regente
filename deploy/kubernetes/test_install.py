import copy
import tempfile
import unittest
from pathlib import Path
import install
import install_resources as model


CONFIG = dict(context="kind-test", namespace="regente-test", environment="prod", domain="regente.example.com",
              storageClass="standard", serverImage="example/server@sha256:" + "a" * 64,
              agentImage="example/agent@sha256:" + "b" * 64, publicIssuer="acme",
              gitSource="https://example.com/workspace.git", agentId="worker-01")


class InstallerContract(unittest.TestCase):
    def test_production_rejects_mutable_images_and_embedded_credentials(self):
        install.validate(CONFIG)
        for key, value in (("serverImage", "example/server:latest"),
                           ("gitSource", "https://user:password@example.com/repo"),
                           ("agentId", "SERVER-AGENT"), ("namespace", "../other")):
            c = {**CONFIG, key: value}
            with self.assertRaises(ValueError):
                install.validate(c)

    def test_storage_bootstrap_and_machine_contract(self):
        initial = model.manifests(CONFIG)
        final = model.manifests(CONFIG, bootstrap=False, git_enabled=True)
        container = lambda items: next(o for o in items if o["kind"] == "Deployment")["spec"]["template"]["spec"]["containers"][0]
        first, last = container(initial), container(final)
        env = {e["name"]: e.get("value") for e in last["env"]}
        self.assertEqual(env["REGENTE_SESSIONS_DIR"], "/data/sessions")
        self.assertEqual(env["REGENTE_WORKSPACE"], "/data/workspace")
        self.assertEqual(first["workingDir"], "/data")
        self.assertTrue(last["securityContext"]["readOnlyRootFilesystem"])
        self.assertFalse(any(e["name"].startswith("REGENTE_BOOTSTRAP_") for e in last["env"]))
        worker = model.worker(CONFIG, "synthetic")
        pod = worker["spec"]["template"]["spec"]
        self.assertFalse(pod["automountServiceAccountToken"])
        self.assertIn("-check-journal", pod["initContainers"][1]["args"])
        self.assertIn(CONFIG["agentId"], pod["containers"][0]["args"])
        self.assertIn(CONFIG["agentId"], pod["initContainers"][1]["args"])

    def test_internal_pki_hostname_and_client_purpose(self):
        keys = install.pki(CONFIG)
        ca = install.x509.load_pem_x509_certificate(keys["ca.crt"].encode())
        for who in ("server", "postgres", "agent"):
            cert = install.x509.load_pem_x509_certificate(keys[who]["tls.crt"].encode())
            cert.verify_directly_issued_by(ca)
        server = install.x509.load_pem_x509_certificate(keys["server"]["tls.crt"].encode())
        names = server.extensions.get_extension_for_class(install.x509.SubjectAlternativeName).value
        self.assertIn("regente-server", names.get_values_for_type(install.x509.DNSName))

    def test_refuse_namespace_without_matching_state(self):
        with tempfile.TemporaryDirectory() as tmp:
            i = install.Installer(CONFIG, tmp)
            i.kube = lambda *a, **kw: b'{"metadata":{"name":"regente-test"}}'
            with self.assertRaisesRegex(RuntimeError, "refusing to adopt"):
                i.initialize()


if __name__ == "__main__":
    unittest.main()
