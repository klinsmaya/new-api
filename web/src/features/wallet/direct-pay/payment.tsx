import { useMutation, useQuery } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { QRCodeSVG } from 'qrcode.react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toIntlLocale } from '@/i18n/languages'
import { api } from '@/lib/api'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { useDirectPayStore, type Purchase } from './store'

export type Order = {
  order_no: string
  environment: string
  money_minor: number
  quota_to_credit: number
  payment_state: string
  settlement_state: string
  expires_at: number
  checkout_kind: string
  checkout_value: string
}
async function read<T>(path: string): Promise<T> {
  const r = await api.get(path)
  return requireServerSuccess(r.data).data as T
}
function canShowCheckout(order: Order, now: number): boolean {
  return (
    order.expires_at > now &&
    order.payment_state === 'pending' &&
    order.settlement_state !== 'credited'
  )
}
export function DirectPayment(props: {
  userId: number
  onCredited: () => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const store = useDirectPayStore()
  const orderNo = store.orders[props.userId] || ''
  const purchase = store.purchases[props.userId]
  const [amount, setAmount] = useState('1')
  const [confirm, setConfirm] = useState(false)
  const [now, setNow] = useState(Date.now() / 1000)
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now() / 1000), 1000)
    return () => clearInterval(timer)
  }, [])
  const methods = useQuery({
    queryKey: ['direct-pay-methods', props.userId],
    queryFn: () =>
      read<{ method: string; environment: string }[]>(
        '/api/user/direct-pay/methods'
      ),
  })
  const order = useQuery({
    queryKey: ['direct-pay-order', props.userId, orderNo],
    queryFn: () => read<Order>(`/api/user/direct-pay/orders/${orderNo}`),
    enabled: !!orderNo,
    refetchInterval: 3000,
  })
  const onCredited = props.onCredited
  useEffect(() => {
    if (order.data?.settlement_state === 'credited') onCredited()
  }, [order.data?.settlement_state, onCredited])
  const quote = useMutation({
    mutationFn: async (method: string) => {
      const r = await api.post('/api/user/direct-pay/quote', {
        amount: Number(amount),
        method,
      })
      const data = requireServerSuccess(r.data).data as Pick<
        Purchase,
        'quote' | 'money_minor' | 'quota_to_credit'
      >
      store.savePurchase(props.userId, {
        ...data,
        amount: Number(amount),
        method,
        key: crypto.randomUUID(),
      })
      setConfirm(true)
    },
  })
  const create = useMutation({
    mutationFn: async () => {
      if (!purchase) return
      const r = await api.post('/api/user/direct-pay/orders', purchase, {
        headers: { 'Idempotency-Key': purchase.key },
      })
      const data = requireServerSuccess(r.data).data as Order
      store.saveOrder(props.userId, data.order_no)
      store.savePurchase(props.userId, undefined)
      setConfirm(false)
    },
    onError: (error) => {
      handleServerError(error)
      if (
        isAxiosError(error) &&
        error.response?.data?.code === 'price_changed'
      ) {
        store.savePurchase(props.userId, undefined)
        setConfirm(false)
      }
    },
  })
  const refresh = useMutation({
    mutationFn: async (action: string) => {
      const r = await api.post(
        `/api/user/direct-pay/orders/${orderNo}/${action}`
      )
      requireServerSuccess(r.data)
      await order.refetch()
    },
  })
  if (!methods.data?.length && !orderNo && !purchase) return null
  const current = order.data
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Official direct payment')}</CardTitle>
      </CardHeader>
      <CardContent className='space-y-4'>
        <p>
          {t(
            'WeChat Native requires scanning from another device. Alipay opens its official checkout.'
          )}
        </p>
        {!orderNo && (
          <>
            <Label htmlFor='directpay-amount'>{t('Topup Amount')}</Label>
            <Input
              id='directpay-amount'
              type='number'
              min={1}
              step={1}
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              disabled={!!purchase}
            />
            <div className='flex flex-wrap gap-2'>
              {methods.data?.map((m) => (
                <Button
                  key={m.method}
                  disabled={
                    !!purchase ||
                    quote.isPending ||
                    !Number.isSafeInteger(Number(amount)) ||
                    Number(amount) <= 0
                  }
                  onClick={() => quote.mutate(m.method)}
                >
                  {m.method === 'wechat_native'
                    ? t('WeChat Native')
                    : t('Alipay PagePay')}{' '}
                  ·{' '}
                  {m.environment === 'sandbox'
                    ? t('Alipay sandbox')
                    : t('Real money')}
                </Button>
              ))}
            </div>
            {purchase && (
              <Button onClick={() => setConfirm(true)}>
                {t('Resume payment request')}
              </Button>
            )}
          </>
        )}
        {order.isError && (
          <p role='alert'>
            {t(
              'Unable to load payment order. Retry without creating another order.'
            )}
          </p>
        )}
        {current && (
          <div className='space-y-3' aria-live='polite'>
            <p className='font-semibold'>
              {current.environment === 'sandbox'
                ? t('Alipay sandbox')
                : t('Real money')}
            </p>
            <p>
              CNY {(current.money_minor / 100).toFixed(2)} · {t('Quota')}:{' '}
              {formatNumber(current.quota_to_credit, locale)}
            </p>
            <p>{current.order_no}</p>
            <p>
              {t('Payment status')}: {current.payment_state} ·{' '}
              {t('Settlement status')}: {current.settlement_state}
            </p>
            {current.payment_state === 'paid' &&
              current.settlement_state !== 'credited' && (
                <p>{t('Paid, credit is processing')}</p>
              )}
            {canShowCheckout(current, now) &&
              current.checkout_kind === 'qr' && (
                <QRCodeSVG
                  value={current.checkout_value}
                  size={220}
                  title={t('WeChat payment QR code')}
                />
              )}
            {canShowCheckout(current, now) &&
              current.checkout_kind === 'redirect' && (
                <Button
                  onClick={() =>
                    window.open(
                      current.checkout_value,
                      '_blank',
                      'noopener,noreferrer'
                    )
                  }
                >
                  {t('Go to Alipay')}
                </Button>
              )}
            {now >= current.expires_at &&
              current.settlement_state !== 'credited' && (
                <p>{t('Checkout expired. Payment verification continues.')}</p>
              )}
            <div className='flex gap-2'>
              <Button
                disabled={refresh.isPending}
                onClick={() => refresh.mutate('refresh')}
              >
                {t('Refresh')}
              </Button>
              <Button
                variant='outline'
                disabled={
                  refresh.isPending || current.settlement_state === 'credited'
                }
                onClick={() => refresh.mutate('close')}
              >
                {t('Request close')}
              </Button>
              {(current.settlement_state === 'credited' ||
                current.payment_state === 'closed') && (
                <Button onClick={() => store.saveOrder(props.userId, '')}>
                  {t('New recharge')}
                </Button>
              )}
            </div>
          </div>
        )}
        <ConfirmDialog
          open={confirm}
          onOpenChange={setConfirm}
          title={t('Confirm Payment')}
          desc={`CNY ${((purchase?.money_minor || 0) / 100).toFixed(2)} · ${t('Quota')}: ${formatNumber(purchase?.quota_to_credit || 0, locale)}`}
          handleConfirm={() => create.mutate()}
          isLoading={create.isPending}
        />
      </CardContent>
    </Card>
  )
}
