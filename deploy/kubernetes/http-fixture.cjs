// Synthetic TLS destination. Its durable ledger is the independent effect witness.
const fs = require('node:fs');
const https = require('node:https');
const server = https.createServer({key: fs.readFileSync('/tls/http-fixture.key'),
  cert: fs.readFileSync('/tls/http-fixture.crt')}, (req, res) => {
  if (req.url === '/health') { res.end('ready'); return; }
  if (req.url === '/ledger') { res.end(fs.existsSync('/data/ledger') ? fs.readFileSync('/data/ledger') : ''); return; }
  const expected = fs.readFileSync('/auth/authorization', 'utf8');
  if (req.headers.authorization !== expected) { res.writeHead(401); res.end('denied'); return; }
  if (req.method !== 'POST') { res.writeHead(405); res.end(); return; }
  const fd = fs.openSync('/data/ledger', 'a');
  fs.writeSync(fd, JSON.stringify({path: req.url, time: Date.now()}) + '\n');
  fs.fsyncSync(fd); fs.closeSync(fd);
  // Echo tests redaction; only synthetic secrets, never real credentials.
  setTimeout(() => {
    res.end('receipt ' + req.headers.authorization);
    fs.appendFileSync('/data/completed', req.url + '\n');
  }, req.url === '/slow' ? 15000 : 0);
});
server.listen(8443, '0.0.0.0');
