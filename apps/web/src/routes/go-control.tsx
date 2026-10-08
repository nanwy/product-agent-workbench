import { createFileRoute } from '@tanstack/react-router'

import { GoControlPage } from '../components/go-control-page'

// 迁移期路由只是一层薄适配；步骤 20 的独立 SPA 会直接复用 GoControlPage。
export const Route = createFileRoute('/go-control')({ component: GoControlPage })
