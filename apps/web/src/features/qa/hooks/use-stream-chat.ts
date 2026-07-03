/**
 * Custom hook for SSE streaming chat.
 *
 * Wraps the low-level `streamChat` API with React-friendly state management.
 * Handlers are kept in refs so the SSE callbacks always see the latest props
 * without re-subscribing the stream.
 *
 * Signature: streamChat(sessionId, message, handlers, signal?)
 */

import { useCallback, useEffect, useRef, useState } from 'react'

import type { ChatStreamHandlers } from '@/api/chat'
import { streamChat } from '@/api/chat'

export function useStreamChat(handlers: ChatStreamHandlers) {
  const [isStreaming, setIsStreaming] = useState(false)
  const abortRef = useRef<(() => void) | null>(null)
  const handlersRef = useRef(handlers)

  // Keep handlers current without re-triggering effects
  handlersRef.current = handlers

  // Abort on unmount
  useEffect(() => {
    return () => {
      abortRef.current?.()
    }
  }, [])

  const sendMessage = useCallback((sessionId: string, message: string) => {
    // Cancel any in-flight stream
    abortRef.current?.()

    setIsStreaming(true)

    const { abort } = streamChat(sessionId, message, {
      onMessageCreated: (data) => {
        handlersRef.current.onMessageCreated?.(data)
      },
      onAgentIterationStarted: (data) => {
        handlersRef.current.onAgentIterationStarted?.(data)
      },
      onReasoningStep: (data) => {
        handlersRef.current.onReasoningStep?.(data)
      },
      onReasoningDelta: (data) => {
        handlersRef.current.onReasoningDelta?.(data)
      },
      onToolStarted: (data) => {
        handlersRef.current.onToolStarted?.(data)
      },
      onToolCompleted: (data) => {
        handlersRef.current.onToolCompleted?.(data)
      },
      onToolFailed: (data) => {
        handlersRef.current.onToolFailed?.(data)
      },
      onAnswerDelta: (data) => {
        handlersRef.current.onAnswerDelta?.(data)
      },
      onCitationDelta: (data) => {
        handlersRef.current.onCitationDelta?.(data)
      },
      onAnswerCompleted: (data) => {
        setIsStreaming(false)
        abortRef.current = null
        handlersRef.current.onAnswerCompleted?.(data)
      },
      onError: (data) => {
        if (data.fatal) {
          setIsStreaming(false)
          abortRef.current = null
        }
        handlersRef.current.onError?.(data)
      },
      onAbort: () => {
        setIsStreaming(false)
        abortRef.current = null
        handlersRef.current.onAbort?.()
      },
    })

    abortRef.current = abort
  }, [])

  const abort = useCallback(() => {
    abortRef.current?.()
    abortRef.current = null
    setIsStreaming(false)
  }, [])

  return { sendMessage, abort, isStreaming }
}
