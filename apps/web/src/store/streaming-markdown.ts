/** 将流式末尾未闭合的代码围栏隔离，避免每个 delta 都触发代码块重排与高亮。 */
export function partitionStreamingMarkdown(content: string, streaming: boolean) {
  if (!streaming) return { stable: content, streamingCode: null }
  const fenceCount = content.match(/```/g)?.length ?? 0
  if (fenceCount % 2 === 0) return { stable: content, streamingCode: null }
  const boundary = content.lastIndexOf('```')
  return { stable: content.slice(0, boundary), streamingCode: content.slice(boundary) }
}
