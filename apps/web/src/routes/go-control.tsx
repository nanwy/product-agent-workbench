import { createFileRoute } from '@tanstack/react-router'

import { GoControlPage } from '../components/go-control-page'

// 资源级调试页继续保留，方便直接检查 Go DTO；它不再替代默认产品工作台。
export const Route = createFileRoute('/go-control')({ component: GoControlPage })
