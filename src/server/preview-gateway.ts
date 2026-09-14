import http, { type IncomingMessage, type ServerResponse } from 'node:http'
import https from 'node:https'
import net from 'node:net'

type GatewayState = {
  server: http.Server | null
  upstream: URL | null
  port: number | null
}

const globalGateway = globalThis as typeof globalThis & {
  __productAgentPreviewGateway?: GatewayState
}

const state = (globalGateway.__productAgentPreviewGateway ??= {
  server: null,
  upstream: null,
  port: null,
})

export async function startPreviewGateway(upstream: string, port: number) {
  const upstreamUrl = new URL(upstream)
  assertLoopback(upstreamUrl)

  if (state.server && state.port === port) {
    state.upstream = upstreamUrl
    return gatewayOrigin(port)
  }

  await stopPreviewGateway()
  state.upstream = upstreamUrl

  const server = http.createServer(proxyHttpRequest)
  server.on('upgrade', proxyWebSocketUpgrade)
  state.server = server
  state.port = port

  await new Promise<void>((resolve, reject) => {
    server.once('error', reject)
    server.listen(port, '127.0.0.1', () => {
      server.off('error', reject)
      resolve()
    })
  })

  return gatewayOrigin(port)
}

export async function stopPreviewGateway() {
  const server = state.server
  state.server = null
  state.upstream = null
  state.port = null
  if (!server) return

  await new Promise<void>((resolve) => {
    server.close(() => resolve())
    server.closeAllConnections()
  })
}

export function removeEmbeddingRestrictions(
  headers: http.IncomingHttpHeaders,
): http.OutgoingHttpHeaders {
  const filtered: http.OutgoingHttpHeaders = { ...headers }
  delete filtered['x-frame-options']

  const policy = filtered['content-security-policy']
  if (policy) {
    const sanitized = ([] as string[])
      .concat(policy)
      .flatMap((value) => value.split(';'))
      .map((directive) => directive.trim())
      .filter(
        (directive) =>
          directive && !directive.toLowerCase().startsWith('frame-ancestors'),
      )
      .join('; ')

    if (sanitized) filtered['content-security-policy'] = sanitized
    else delete filtered['content-security-policy']
  }

  return filtered
}

function proxyHttpRequest(request: IncomingMessage, response: ServerResponse) {
  const upstream = state.upstream
  if (!upstream) {
    response.writeHead(503).end('Preview gateway is stopped')
    return
  }

  let target: URL
  try {
    target = resolvePreviewTarget(request.url ?? '/', upstream)
  } catch {
    response.writeHead(400).end('Invalid preview path')
    return
  }
  const transport = target.protocol === 'https:' ? https : http
  const headers = { ...request.headers, host: target.host }
  delete headers.connection

  const proxyRequest = transport.request(
    target,
    { method: request.method, headers },
    (proxyResponse) => {
      response.writeHead(
        proxyResponse.statusCode ?? 502,
        removeEmbeddingRestrictions(proxyResponse.headers),
      )
      proxyResponse.pipe(response)
    },
  )
  proxyRequest.on('error', (error) => {
    if (!response.headersSent) response.writeHead(502)
    response.end(`Preview upstream unavailable: ${error.message}`)
  })
  request.pipe(proxyRequest)
}

function proxyWebSocketUpgrade(
  request: IncomingMessage,
  browserSocket: net.Socket,
  head: Buffer,
) {
  const upstream = state.upstream
  if (!upstream) {
    browserSocket.destroy()
    return
  }

  let requestPath: string
  try {
    requestPath = resolvePreviewTarget(request.url ?? '/', upstream).pathname
    const search = resolvePreviewTarget(request.url ?? '/', upstream).search
    requestPath += search
  } catch {
    browserSocket.destroy()
    return
  }

  const port = Number(upstream.port || (upstream.protocol === 'https:' ? 443 : 80))
  const upstreamSocket = net.connect(port, upstream.hostname, () => {
    const headers = request.rawHeaders
      .reduce<string[]>((lines, value, index, values) => {
        if (index % 2 !== 0) return lines
        const name = value
        const headerValue =
          name.toLowerCase() === 'host' ? upstream.host : values[index + 1]
        lines.push(`${name}: ${headerValue}`)
        return lines
      }, [])
      .join('\r\n')
    upstreamSocket.write(
      `${request.method} ${requestPath} HTTP/${request.httpVersion}\r\n${headers}\r\n\r\n`,
    )
    if (head.length) upstreamSocket.write(head)
    browserSocket.pipe(upstreamSocket).pipe(browserSocket)
  })
  upstreamSocket.on('error', () => browserSocket.destroy())
}

function assertLoopback(url: URL) {
  if (!['localhost', '127.0.0.1', '::1'].includes(url.hostname)) {
    throw new Error('预览网关只允许代理本机目标')
  }
}

export function resolvePreviewTarget(rawPath: string, upstream: URL) {
  if (!rawPath.startsWith('/') || rawPath.startsWith('//')) {
    throw new Error('预览请求必须使用站内路径')
  }
  const parsed = new URL(rawPath, 'http://preview.invalid')
  const target = new URL(upstream.origin)
  target.pathname = parsed.pathname
  target.search = parsed.search
  return target
}

function gatewayOrigin(port: number) {
  return `http://127.0.0.1:${port}`
}
