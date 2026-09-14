import http, { type IncomingMessage, type ServerResponse } from 'node:http'
import https from 'node:https'
import net from 'node:net'

type GatewayState = {
  server: http.Server | null
  upstream: URL | null
  port: number | null
}

// 独立 localhost 端口保留目标应用的 /assets 等绝对路径语义。
// 状态放到 globalThis，避免 Vite 热更重新加载模块时重复监听同一端口。
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

  // 同端口已由本工作台持有时只更新上游，不打断 iframe/HMR 连接。
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

  try {
    await new Promise<void>((resolve, reject) => {
      server.once('error', reject)
      server.listen(port, '127.0.0.1', () => {
        server.off('error', reject)
        resolve()
      })
    })
  } catch (error) {
    // 监听失败时清掉单例引用，使修复端口冲突后可以直接重试。
    if (state.server === server) {
      state.server = null
      state.upstream = null
      state.port = null
    }
    throw error
  }

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
  // 只移除阻止开发期 iframe 的两类策略；其他 CSP、Cookie 和业务响应头原样保留。
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
  // 上游 host 必须改成目标应用，但不转发 hop-by-hop connection 头。
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
  // Vite/Next 的 HMR 使用 WebSocket；只代理 HTTP 会导致首屏能看、代码修改不刷新。
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
  // 拒绝 //host 形式的 protocol-relative URL，防止请求逃离已校验的 loopback 上游。
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
