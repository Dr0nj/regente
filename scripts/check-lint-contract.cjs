// Exercise the actual ESLint config and file traversal after dependency upgrades.
// In particular, a successful lint must not mean that TS/TSX files were omitted.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { createRequire } = require('node:module');
const app = path.resolve(__dirname, '../app');
const appRequire = createRequire(path.join(app, 'package.json'));
const { ESLint } = appRequire('eslint');

async function main() {
  for (const consumer of ['eslint', '@eslint/config-array', '@typescript-eslint/typescript-estree']) {
    const consumerRequire = createRequire(appRequire.resolve(consumer));
    assert.equal(consumerRequire('minimatch/package.json').version, '11.0.1', `${consumer}: review the security override and this contract when upgrading`);
  }
  const dir = await fs.mkdtemp(path.join(app, '.lint-contract-'));
  try {
    const cases = ['root.ts', 'nested/view.tsx', '.hidden/file.ts'];
    for (const file of cases) {
      const target = path.join(dir, file);
      await fs.mkdir(path.dirname(target), { recursive: true });
      await fs.writeFile(target, 'const unusedContractValue: number = 1;\n');
    }
    const eslint = new ESLint({ cwd: app });
    const relative = path.relative(app, dir).split(path.sep).join('/');
    const results = await eslint.lintFiles([`${relative}/**/*.{ts,tsx}`]);
    assert.deepEqual(results.map(r => path.relative(dir, r.filePath).split(path.sep).join('/')).sort(), cases.sort());
    for (const result of results) {
      assert.equal(result.fatalErrorCount, 0, result.filePath);
      assert.ok(result.messages.some(m => m.ruleId === '@typescript-eslint/no-unused-vars' && m.severity === 2), result.filePath);
    }
    const [valid] = await eslint.lintText('export const ContractView = () => <div />;\n', { filePath: path.join(dir, 'valid.tsx') });
    assert.equal(valid.errorCount, 0, JSON.stringify(valid.messages));
    assert.equal(valid.warningCount, 0, JSON.stringify(valid.messages));
    assert.equal(await eslint.isPathIgnored(path.join(app, 'dist/contract.ts')), true);
    await assert.rejects(eslint.lintFiles([`${relative}/missing/**/*.ts`]), /No files matching|No files found/);
    console.log('PASS: ESLint traverses TS/TSX, nested and dot paths, detects invalid code and preserves dist exclusion.');
  } finally {
    // Only the unique temporary directory created above is removed.
    await fs.rm(dir, { recursive: true, force: true });
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
