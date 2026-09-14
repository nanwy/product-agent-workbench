import http from 'node:http'

const server = http.createServer((request, response) => {
  if (request.url === '/health') {
    response.writeHead(200, { 'content-type': 'application/json' })
    response.end('{"status":"ok"}')
    return
  }

  response.writeHead(200, {
    'content-type': 'text/html; charset=utf-8',
    'content-security-policy': "default-src 'self'; frame-ancestors 'none'; script-src 'self'",
    'x-frame-options': 'DENY',
    'x-fixture-header': 'preserved',
  })
  response.end(`<!doctype html><html><body><h1>Fixture target</h1><p>${request.url}</p></body></html>`)
})

server.listen(4173, '127.0.0.1')

for (const signal of ['SIGTERM', 'SIGINT']) {
  process.on(signal, () => server.close(() => process.exit(0)))
}
