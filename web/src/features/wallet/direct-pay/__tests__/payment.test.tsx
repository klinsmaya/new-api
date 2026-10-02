import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { DirectPayment } from '../payment'
import { useDirectPayStore } from '../store'

const i18n = createInstance()
beforeEach(async () => {
  await i18n.init({
    lng: 'en',
    resources: {},
    interpolation: { escapeValue: false },
  })
  useDirectPayStore.setState({ orders: { 7: 'existing-order' }, purchases: {} })
})
afterEach(() => {
  vi.restoreAllMocks()
  useDirectPayStore.setState({ orders: {}, purchases: {} })
})
it.each([
  {
    payment: 'paid',
    settlement: 'credit_pending',
    credited: false,
    expired: false,
  },
  { payment: 'paid', settlement: 'credited', credited: true, expired: false },
  {
    payment: 'pending',
    settlement: 'uncredited',
    credited: false,
    expired: true,
  },
])(
  'restores $payment/$settlement without creating an order or treating redirect as credit',
  async (state) => {
    const post = vi.spyOn(api, 'post')
    vi.spyOn(api, 'get').mockImplementation(async (path) => ({
      data: {
        success: true,
        data: path.endsWith('/methods')
          ? []
          : {
              order_no: 'existing-order',
              money_minor: 123,
              quota_to_credit: 1000,
              payment_state: state.payment,
              settlement_state: state.settlement,
              expires_at: state.expired ? 1 : Date.now() / 1000 + 600,
              environment: 'sandbox',
              checkout_kind: 'redirect',
              checkout_value:
                'https://openapi-sandbox.dl.alipaydev.com/gateway.do',
            },
      },
    }))
    const credited = vi.fn()
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={client}>
          <DirectPayment userId={7} onCredited={credited} />
        </QueryClientProvider>
      </I18nextProvider>
    )
    await screen.findByText('existing-order')
    expect(screen.getByText('Alipay sandbox')).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Go to Alipay' })
    ).not.toBeInTheDocument()
    expect(post).not.toHaveBeenCalled()
    if (state.credited) expect(credited).toHaveBeenCalledOnce()
    else expect(credited).not.toHaveBeenCalled()
    client.clear()
  }
)
