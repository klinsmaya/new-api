import { useMutation, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

type Overview = {
  create_enabled: boolean
  startup_allows_create: boolean
  accounts: {
    account_ref: string
    provider: string
    environment: string
    notify_url: string
  }[]
  orders: {
    order_no: string
    money_minor: number
    payment_state: string
    settlement_state: string
    last_error: string
  }[]
}
export function DirectPaymentAdmin() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['direct-pay-admin'],
    queryFn: async () => {
      const r = await api.get('/api/user/direct-pay/admin')
      return requireServerSuccess(r.data).data as Overview
    },
  })
  const toggle = useMutation({
    mutationFn: async () => {
      const r = await api.put('/api/user/direct-pay/admin', {
        enabled: !query.data?.create_enabled,
      })
      requireServerSuccess(r.data)
      await query.refetch()
    },
  })
  const refresh = useMutation({
    mutationFn: async (order: string) => {
      const r = await api.post(`/api/user/direct-pay/admin/${order}/refresh`)
      requireServerSuccess(r.data)
      await query.refetch()
    },
  })
  return (
    <section
      className='mb-6 space-y-3'
      aria-label={t('Official direct payment')}
    >
      <h3 className='font-semibold'>{t('Official direct payment')}</h3>
      <p>
        {t(
          'Credentials are provided by a mounted secret. Refunds remain disabled. Stopping new orders keeps historical settlement active.'
        )}
      </p>
      <Button
        type='button'
        onClick={() => toggle.mutate()}
        disabled={
          !query.data ||
          toggle.isPending ||
          (!query.data.startup_allows_create && !query.data.create_enabled)
        }
      >
        {query.data?.create_enabled
          ? t('Stop new direct payments')
          : t('Enable direct payments')}
      </Button>
      {query.data?.accounts.map((a) => (
        <p key={a.account_ref}>
          {a.provider} · {a.environment} · {a.account_ref} · {a.notify_url}
        </p>
      ))}
      <ul>
        {query.data?.orders.map((o) => (
          <li key={o.order_no}>
            {o.order_no} · CNY {(o.money_minor / 100).toFixed(2)} ·{' '}
            {o.payment_state} / {o.settlement_state} · {o.last_error}
            <Button
              type='button'
              disabled={refresh.isPending}
              onClick={() => refresh.mutate(o.order_no)}
            >
              {t('Refresh')}
            </Button>
          </li>
        ))}
      </ul>
    </section>
  )
}
