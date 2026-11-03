# Kubernetes application installation

Status: IN DEVELOPMENT AND TESTING (EM DESENVOLVIMENTO E TESTES). Do not present
this installer or its images as production-qualified. Current priority is native
Linux server testing on a VPS; further Kubernetes expansion is paused.

For an end-user installation, start with INSTALL.md and install.py. Assume an
existing cluster. lab.py and the phase/cell scripts are engineering qualifications,
not a replacement production installer. Never assemble a production Deployment by
copying isolated fragments out of them.

Default supported executor is a separate worker pod; scripts execute there.
Use one immutable release for both images. Preserve the installer state, namespace
ownership, exact agentId/environment/capabilities, certificate/token binding and
journal across retries. Never delete a journal to resolve an identity mismatch.

Writable storage includes workspace, sessions, audit key/public key, archives,
temporary files and worker journal. Do not disable read-only root to hide a missing
mount. Bootstrap cleanup removes both user/password refs before Secret keys.

Completion requires verified TLS, login after bootstrap cleanup, real Git sync,
opening a Design folder, a durable job receipt and one effect. Online/readyz alone
are insufficient. Reconcile a second time and restart server/worker in an isolated
qualification. Never run smoke jobs from the user's existing workspace by guessing
that they are harmless. Explain WAIT gates; do not force imported jobs in bulk.

An existing manual installation requires backup and a migration plan; the fresh
installer intentionally refuses adoption. Report public ingress, renewal, backup,
HA and capacity limits honestly. Never commit state.json, Secret values, tokens,
private keys or raw diagnostic dumps containing them.
