import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { renderWithProviders } from '@/test/render'

import { QARetrievalTestPage } from './page'

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('QARetrievalTestPage accessibility smoke', () => {
  it('submits the retrieval form with labelled controls using only the keyboard', async () => {
    const keyboard = userEvent.setup()
    const submittedPayloads: unknown[] = []
    const fetchMock = vi.fn<typeof fetch>(async (input, init) => {
      const request = input instanceof Request ? input.clone() : new Request(input, init)
      const url = new URL(request.url)

      if (request.method === 'POST' && url.pathname.endsWith('/retrieval-test-runs')) {
        submittedPayloads.push(await request.json())
        return jsonResponse({
          data: {
            createdAt: '2026-07-02T09:00:00Z',
            finishedAt: '2026-07-02T09:00:01Z',
            id: 'retrieval-run-1',
            results: [],
            status: 'completed',
          },
          requestId: 'req-retrieval-create',
        })
      }

      if (
        request.method === 'GET' &&
        url.pathname.endsWith('/retrieval-test-runs/retrieval-run-1')
      ) {
        return jsonResponse({
          data: {
            createdAt: '2026-07-02T09:00:00Z',
            finishedAt: '2026-07-02T09:00:01Z',
            id: 'retrieval-run-1',
            results: [],
            status: 'completed',
          },
          requestId: 'req-retrieval-run',
        })
      }

      return new Response(JSON.stringify({ error: { code: 'unexpected_request' } }), {
        headers: { 'Content-Type': 'application/json' },
        status: 500,
      })
    })
    vi.stubGlobal('fetch', fetchMock)

    renderWithProviders(<QARetrievalTestPage />)

    const textboxes = screen.getAllByRole('textbox')
    const queryInput = screen.getByLabelText('Query')
    const topKInput = screen.getByLabelText('Top K')
    const rerankCheckbox = screen.getByRole('checkbox', { name: /rerank/i })

    expect(queryInput).toHaveAccessibleName('Query')
    expect(topKInput).toHaveAccessibleName('Top K')
    expect(rerankCheckbox).toHaveAccessibleName(/rerank/i)

    await keyboard.tab()
    expect(queryInput).toHaveFocus()
    await keyboard.keyboard('transformer oil temperature')
    await keyboard.tab()
    expect(textboxes[1]).toHaveFocus()
    await keyboard.keyboard('kb-a11y')
    await keyboard.tab()
    expect(topKInput).toHaveFocus()
    await keyboard.tab()
    await keyboard.tab()
    expect(rerankCheckbox).toHaveFocus()
    await keyboard.keyboard(' ')
    expect(rerankCheckbox).not.toBeChecked()
    await keyboard.tab()
    await keyboard.tab()
    await keyboard.tab()

    const buttons = screen.getAllByRole('button')
    const submitButton = buttons[buttons.length - 1]
    expect(submitButton).toHaveFocus()
    await keyboard.keyboard('{Enter}')

    await waitFor(() => expect(submittedPayloads).toHaveLength(1))
    expect(submittedPayloads[0]).toMatchObject({
      knowledgeBaseIds: ['kb-a11y'],
      question: 'transformer oil temperature',
      retrieval: {
        enableRerank: false,
        topK: 5,
      },
    })
  })
})
