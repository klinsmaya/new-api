import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type Purchase = {
  amount: number
  method: string
  quote: string
  key: string
  money_minor: number
  quota_to_credit: number
}
type State = {
  orders: Record<number, string>
  purchases: Record<number, Purchase | undefined>
  saveOrder: (user: number, order: string) => void
  savePurchase: (user: number, purchase: Purchase | undefined) => void
}
export const useDirectPayStore = create<State>()(
  persist(
    (set) => ({
      orders: {},
      purchases: {},
      saveOrder: (user, order) =>
        set((s) => ({ orders: { ...s.orders, [user]: order } })),
      savePurchase: (user, purchase) =>
        set((s) => ({ purchases: { ...s.purchases, [user]: purchase } })),
    }),
    { name: 'direct-payment-orders-v1' }
  )
)
