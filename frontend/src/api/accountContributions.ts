import { apiClient } from './client'
import type { Account, ContributorRewardLog, PaginatedResponse } from '@/types'

export interface ContributionAuthURLRequest {
  proxy_id?: number | null
  redirect_uri?: string
}

export interface ContributionAuthURLResult {
  auth_url: string
  session_id: string
}

export interface SubmitOpenAIContributionRequest {
  session_id: string
  code: string
  state: string
  redirect_uri?: string
  proxy_id?: number | null
  name?: string
}

export async function generateOpenAIContributionAuthURL(
  payload: ContributionAuthURLRequest = {}
): Promise<ContributionAuthURLResult> {
  const { data } = await apiClient.post<ContributionAuthURLResult>(
    '/account-contributions/openai/auth-url',
    payload
  )
  return data
}

export async function submitOpenAIContribution(
  payload: SubmitOpenAIContributionRequest
): Promise<Account> {
  const { data } = await apiClient.post<Account>(
    '/account-contributions/openai/exchange-code',
    payload
  )
  return data
}

export async function listMyContributions(
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<Account>> {
  const { data } = await apiClient.get<PaginatedResponse<Account>>('/account-contributions', {
    params: { page, page_size: pageSize }
  })
  return data
}

export async function revokeContribution(id: number): Promise<Account> {
  const { data } = await apiClient.delete<Account>(`/account-contributions/${id}`)
  return data
}

export async function listContributionRewards(
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<ContributorRewardLog>> {
  const { data } = await apiClient.get<PaginatedResponse<ContributorRewardLog>>(
    '/account-contributions/rewards',
    { params: { page, page_size: pageSize } }
  )
  return data
}

export const accountContributionsAPI = {
  generateOpenAIAuthURL: generateOpenAIContributionAuthURL,
  submitOpenAI: submitOpenAIContribution,
  listMine: listMyContributions,
  revoke: revokeContribution,
  listRewards: listContributionRewards
}

export default accountContributionsAPI
