// DOC-B: contrato da toolchain e receitas correntes, sem varrer historico/planos.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { createRequire } = require('node:module');
const root = path.resolve(__dirname, '..');
const appRequire = createRequire(path.join(root, 'app/package.json'));
// Ja presente no lockfile (toolchain ESLint); nao instala dependencia nova.
const semver = createRequire(appRequire.resolve('@typescript-eslint/typescript-estree'))('semver');
const read = p => fs.readFileSync(path.join(root, p), 'utf8');
const pkg = JSON.parse(read('app/package.json'));
const lock = JSON.parse(read('app/package-lock.json'));
const range = pkg.engines.node;
function validateRange(declared) {
  assert(semver.validRange(declared), 'Invalid documented Node range');
  for (const [name, entry] of Object.entries(lock.packages)) {
    if (!name || !entry.engines?.node) continue;
    assert(semver.subset(declared, entry.engines.node), `${declared} is not covered by ${name}: ${entry.engines.node}`);
  }
}
validateRange(range);
assert.equal(lock.packages[''].engines.node, range, 'Lock root must match package engines');
assert(semver.satisfies(process.versions.node, range), 'Running Node is unsupported');
for (const doc of ['README.md', 'app/README.md', 'deploy/demo/README.md']) {
  const match = read(doc).match(/Node\.js: `([^`]+)`/g) || [];
  assert.deepEqual(match, [`Node.js: \`${range}\``], `${doc}: use one exact current Node declaration`);
}
for (const doc of ['README.md', 'app/README.md', 'server/deploy/README.md', 'server/deploy/install-linux.sh']) {
  const body = read(doc);
  assert(!/VITE_REGENTE_SERVER_URL=@origin npm ci/.test(body), `${doc}: origin must reach build`);
  assert(body.includes('npm ci && VITE_REGENTE_SERVER_URL=@origin npm run build'), `${doc}: canonical POSIX build missing`);
}
// Negativos sao fixtures em memoria; nenhuma doc corrente e modificada.
for (const bad of ['>=18', '>=20', '^22.12.0']) {
  assert.throws(() => validateRange(bad), undefined, `Outdated requirement accepted: ${bad}`);
}
console.log(`PASS: Node ${process.versions.node}, locked engines, docs and same-origin recipes; negative fixtures rejected.`);
