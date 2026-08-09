import { memo, useMemo, useState, useTransition, type ComponentPropsWithoutRef } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import 'highlight.js/styles/github.css'
import './MarkdownContent.css'

const markdownLink = ({ href, children }: ComponentPropsWithoutRef<'a'>) => (
  <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>
)

const markdownComponents = { a: markdownLink }

/** 超过则不做代码高亮（rehype-highlight 全量跑 highlight.js，大文档会卡死主线程） */
const MAX_CHARS_FULL_HIGHLIGHT = 12_000
/** 超过则不做 Markdown 解析，仅纯文本（remark/react-markdown 大文档同样极重） */
const MAX_CHARS_MARKDOWN = 36_000

interface MarkdownContentProps {
  children: string
  /** 流式时显示光标；流式阶段不做 Markdown/语法高亮，避免主线程卡死 */
  showCursor?: boolean
}

function PlainHugeBody({ text, charCount }: { text: string; charCount: number }) {
  const [tryMarkdown, setTryMarkdown] = useState(false)
  const [isPending, startTransition] = useTransition()

  if (!tryMarkdown) {
    return (
      <>
        <p className="markdown-huge-banner">
          正文约 {charCount.toLocaleString()} 字符，为避免页面卡死已用纯文本显示。若需排版可点下方按钮（仍可能较慢）。
        </p>
        <button type="button" className="markdown-huge-retry" disabled={isPending} onClick={() => startTransition(() => setTryMarkdown(true))}>
          {isPending ? '渲染中…' : '尝试 Markdown 排版（无代码高亮）'}
        </button>
        <div className="markdown-content-streaming markdown-huge-plain">{text}</div>
      </>
    )
  }

  return (
    <div className="markdown-huge-plain">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[]} components={markdownComponents}>
        {text}
      </ReactMarkdown>
    </div>
  )
}

export const MarkdownContent = memo(function MarkdownContent({ children, showCursor }: MarkdownContentProps) {
  const text = children || ''
  const len = text.length

  const rehypePlugins = useMemo(
    () => (len > MAX_CHARS_FULL_HIGHLIGHT ? [] : [rehypeHighlight]),
    [len]
  )

  if (showCursor) {
    return (
      <div className="markdown-content markdown-content-streaming">
        <span className="markdown-stream-fallback">{text}</span>
        <span className="markdown-cursor" aria-hidden />
      </div>
    )
  }

  if (len > MAX_CHARS_MARKDOWN) {
    return (
      <div className="markdown-content markdown-content-huge">
        <PlainHugeBody text={text} charCount={len} />
      </div>
    )
  }

  return (
    <div className="markdown-content">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={rehypePlugins} components={markdownComponents}>
        {text}
      </ReactMarkdown>
    </div>
  )
})
