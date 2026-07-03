import { apiClient } from '../client'
import type { Account, PaginatedResponse } from '@/types'

export interface ApproveContributionRequest {
  group_ids: number[]
  concurrency?: number
  priority?: number
}

export async function listPendingContributions(
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<Account>> {
  const { data } = await apiClient.get<PaginatedResponse<Account>>('/admin/account-contributions', {
    params: { page, page_size: pageSize }
  })
  return data
}

export async function approveContribution(
  id: number,
  payload: ApproveContributionRequest
): Promise<Account> {
  const { data } = await apiClient.post<Account>(
    `/admin/account-contributions/${id}/approve`,
    payload
  )
  return data
}

export async function rejectContribution(id: number): Promise<Account> {
  const { data } = await apiClient.post<Account>(`/admin/account-contributions/${id}/reject`)
  return data
}

export const adminAccountContributionsAPI = {
  listPending: listPendingContributions,
  approve: approveContribution,
  reject: rejectContribution
}

export default adminAccountContributionsAPI
