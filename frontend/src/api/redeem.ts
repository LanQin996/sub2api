/**
 * Redeem code API endpoints
 * Handles redeem code redemption for users
 */

import { apiClient } from './client'
import type { PaginatedResponse, RedeemCode, RedeemCodeRequest } from '@/types'

export interface RedeemHistoryItem {
  id: number
  code: string
  type: string
  value: number
  status: string
  used_at: string
  created_at: string
  // Notes from admin for admin_balance/admin_concurrency types
  notes?: string
  // Subscription-specific fields
  group_id?: number
  validity_days?: number
  group?: {
    id: number
    name: string
  }
}

export interface InvitationCodeItem extends RedeemCode {}

/**
 * Redeem a code
 * @param code - Redeem code string
 * @returns Redemption result with updated balance or concurrency
 */
export async function redeem(code: string): Promise<{
  message: string
  type: string
  value: number
  new_balance?: number
  new_concurrency?: number
}> {
  const payload: RedeemCodeRequest = { code }

  const { data } = await apiClient.post<{
    message: string
    type: string
    value: number
    new_balance?: number
    new_concurrency?: number
  }>('/redeem', payload)

  return data
}

/**
 * Get user's redemption history
 * @returns List of redeemed codes
 */
export async function getHistory(): Promise<RedeemHistoryItem[]> {
  const { data } = await apiClient.get<RedeemHistoryItem[]>('/redeem/history')
  return data
}

export async function listInvitationCodes(
  page: number = 1,
  pageSize: number = 20
): Promise<PaginatedResponse<InvitationCodeItem>> {
  const { data } = await apiClient.get<PaginatedResponse<InvitationCodeItem>>(
    '/redeem/invitation-codes',
    {
      params: {
        page,
        page_size: pageSize
      }
    }
  )
  return data
}

export async function generateInvitationCodes(count: number): Promise<InvitationCodeItem[]> {
  const { data } = await apiClient.post<InvitationCodeItem[]>('/redeem/invitation-codes/generate', {
    count
  })
  return data
}

export const redeemAPI = {
  redeem,
  getHistory,
  listInvitationCodes,
  generateInvitationCodes
}

export default redeemAPI
